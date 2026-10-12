// Package discovery finds other devices on the local network by exchanging
// identity packets over UDP broadcast, as KDE Connect does.
package discovery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/protocol"
)

// Peer is a device heard on the network.
type Peer struct {
	Identity protocol.Identity
	Addr     net.IP
	SeenAt   time.Time
}

// networkPoll is how often the set of local networks is checked for changes.
const networkPoll = 5 * time.Second

// Service announces this device and reports the peers it hears.
//
// It announces at startup, when the machine joins a network and on Refresh,
// rather than on a timer: other implementations reconnect on every
// announcement they hear, which would keep replacing working links.
type Service struct {
	Self protocol.Identity
	Log  *slog.Logger

	refresh chan struct{}
}

// New prepares a discovery service for the given identity.
func New(self protocol.Identity, log *slog.Logger) *Service {
	return &Service{Self: self, Log: log, refresh: make(chan struct{}, 1)}
}

// Refresh announces this device again, to reach devices that missed it.
func (s *Service) Refresh() {
	select {
	case s.refresh <- struct{}{}:
	default:
	}
}

// Run announces and listens until ctx is cancelled. Every identity packet from
// another device is sent to peers; the channel is closed when Run returns.
func (s *Service) Run(ctx context.Context, peers chan<- Peer) error {
	defer close(peers)

	lc := net.ListenConfig{Control: reuseAddr}
	pc, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf(":%d", protocol.DefaultPort))
	if err != nil {
		return fmt.Errorf("listen for discovery: %w", err)
	}
	defer pc.Close()
	go func() {
		<-ctx.Done()
		pc.Close()
	}()

	go s.announceLoop(ctx, pc)

	buf := make([]byte, 64*1024)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read discovery packet: %w", err)
		}
		peer, err := s.parse(buf[:n], from)
		if err != nil {
			s.Log.Debug("ignored discovery packet", "from", from, "err", err)
			continue
		}
		select {
		case peers <- peer:
		case <-ctx.Done():
			return nil
		}
	}
}

var errSelf = errors.New("own announcement")

// SameHost reports whether ip belongs to this machine. Another program that
// speaks the protocol here, such as KDE Connect, would otherwise show up as a
// second device with this machine's name, and linking to it is pointless.
//
// PEREKAT_SAME_HOST turns the check off, to try several instances on one machine.
func SameHost(ip net.IP) bool {
	if ip == nil || os.Getenv("PEREKAT_SAME_HOST") != "" {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, n := range localNets() {
		if n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

var (
	netsMu     sync.Mutex
	hostNets   []*net.IPNet
	hostNetsOK bool
)

// SetLocalNets tells discovery which addresses this device has. Android does
// not let a program list its own network interfaces, so the application
// around the daemon reports them instead.
func SetLocalNets(nets []*net.IPNet) {
	netsMu.Lock()
	defer netsMu.Unlock()
	hostNets, hostNetsOK = nets, true
}

// localNets returns the addresses of this device with their network masks.
func localNets() []*net.IPNet {
	netsMu.Lock()
	nets, ok := hostNets, hostNetsOK
	netsMu.Unlock()
	if ok {
		return nets
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				// A network that cannot broadcast is marked by having no mask.
				if iface.Flags&net.FlagBroadcast == 0 || iface.Flags&net.FlagLoopback != 0 {
					ipnet = &net.IPNet{IP: ipnet.IP}
				}
				nets = append(nets, ipnet)
			}
		}
	}
	return nets
}

func (s *Service) parse(data []byte, from net.Addr) (Peer, error) {
	p, err := protocol.Decode(data)
	if err != nil {
		return Peer{}, err
	}
	id, err := p.Identity()
	if err != nil {
		return Peer{}, err
	}
	if id.DeviceID == s.Self.DeviceID || SameHost(from.(*net.UDPAddr).IP) {
		return Peer{}, errSelf
	}
	if id.TCPPort == 0 {
		return Peer{}, errors.New("announcement without tcp port")
	}
	return Peer{Identity: id, Addr: from.(*net.UDPAddr).IP, SeenAt: time.Now()}, nil
}

func (s *Service) announceLoop(ctx context.Context, pc net.PacketConn) {
	t := time.NewTicker(networkPoll)
	defer t.Stop()
	networks := fmt.Sprint(broadcastAddrs())
	for {
		if err := s.announce(pc); err != nil {
			s.Log.Warn("announce failed", "err", err)
		}
		for changed := false; !changed; {
			select {
			case <-t.C:
				now := fmt.Sprint(broadcastAddrs())
				changed, networks = now != networks, now
			case <-s.refresh:
				changed = true
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *Service) announce(pc net.PacketConn) error {
	p, err := protocol.New(protocol.TypeIdentity, s.Self)
	if err != nil {
		return err
	}
	wire, err := p.Encode()
	if err != nil {
		return err
	}
	var errs []error
	for _, ip := range broadcastAddrs() {
		_, err := pc.WriteTo(wire, &net.UDPAddr{IP: ip, Port: protocol.DefaultPort})
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ip, err))
		}
	}
	return errors.Join(errs...)
}

// broadcastAddrs returns the directed broadcast address of every active IPv4
// network, falling back to the limited broadcast address when none is found.
func broadcastAddrs() []net.IP {
	var out []net.IP
	for _, ipnet := range localNets() {
		ip := ipnet.IP.To4()
		if ip == nil || len(ipnet.Mask) != net.IPv4len {
			continue
		}
		bcast := make(net.IP, net.IPv4len)
		for i := range ip {
			bcast[i] = ip[i] | ^ipnet.Mask[i]
		}
		out = append(out, bcast)
	}
	if len(out) == 0 {
		out = append(out, net.IPv4bcast)
	}
	return out
}
