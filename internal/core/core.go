// Package core tracks devices, their links and their pairing state.
package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/discovery"
	"github.com/charg3d/perekat/internal/input"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
	"github.com/charg3d/perekat/internal/trust"
)

const (
	pairTimeout = 30 * time.Second
	// injectorWarmup is how long a new virtual mouse takes to be picked up by
	// the system. Events are dropped meanwhile: replayed late, they would
	// arrive as one fast jerk.
	injectorWarmup = 300 * time.Millisecond
	// maxClockSkew is how far a pair request's timestamp may be from our clock.
	maxClockSkew = 1800
)

type pairState int

const (
	pairNone pairState = iota
	pairRequested
	pairIncoming
)

type device struct {
	identity protocol.Identity
	addr     net.IP
	link     *link.Link

	state pairState
	ts    int64
	gen   int // bumped on every pairing state change, to void stale timeouts

	commands []Command // what the device offers to run
	battery  *Battery
	shares   []Share // the network folders the device offers
	office   bool    // the device can convert documents for previews
}

// DeviceInfo is the externally visible state of one device.
type DeviceInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Addr      string `json:"addr,omitempty"`
	Connected bool   `json:"connected"`
	Paired    bool   `json:"paired"`
	// Pairing is "requested" while we wait for the peer, "incoming" while the
	// peer waits for us, and empty otherwise.
	Pairing string `json:"pairing,omitempty"`
	// Key is the code both devices show during pairing; it must match.
	Key string `json:"key,omitempty"`
	// Remote is set while this machine's mouse controls the device.
	Remote bool `json:"remote,omitempty"`
	// Relayed is set when the device is reached through the relay server
	// rather than directly.
	Relayed bool `json:"relayed,omitempty"`
	// Foreign is set when the device runs KDE Connect rather than perekat.
	Foreign bool `json:"foreign,omitempty"`
	// CanRemote is set when the device can be put under remote control.
	CanRemote bool `json:"canRemote,omitempty"`
	// CanBrowse is set when the device's files can be browsed.
	CanBrowse bool `json:"canBrowse,omitempty"`
	// CanRing is set when the device can be asked to play a sound.
	CanRing  bool      `json:"canRing,omitempty"`
	Commands []Command `json:"commands,omitempty"`
	Battery  *Battery  `json:"battery,omitempty"`
	// Shares are the network folders the device offers.
	Shares []Share `json:"shares,omitempty"`
	// CanConvert is set when the device can convert documents for previews.
	CanConvert bool `json:"canConvert,omitempty"`
}

// remote is an active remote-control session: the local mouse is captured and
// its events go to one device.
type remote struct {
	target string
	events chan input.Event
	stop   func()
	quit   chan struct{}
}

// Core is the device registry of a running daemon.
type Core struct {
	log       *slog.Logger
	links     link.Config
	trust     *trust.Store
	localSPKI []byte

	mu      sync.Mutex
	devices map[string]*device
	dialing map[string]bool
	remote  *remote

	injectMu      sync.Mutex
	injector      input.Injector
	injectorReady time.Time

	// dir holds the user's configuration, including commands.json.
	dir string

	// OnRefresh, if set, is called to look for devices again.
	OnRefresh func()
	// OnRestart, if set, is called to restart the daemon.
	OnRestart func()

	settingsMu sync.Mutex
	stored     storedSettings

	fsMu      sync.Mutex
	fsPending map[string]chan FSResult

	relayMu  sync.Mutex
	relayCtx context.Context
	relay    *relaySession

	clipMu   sync.Mutex
	clip     string
	clipTime int64 // when clip was set, in milliseconds; zero if unknown

	transferMu sync.Mutex
	transfers  []*transfer
	transferID int
}

func New(log *slog.Logger, links link.Config, store *trust.Store, dir string) (*Core, error) {
	leaf, err := x509.ParseCertificate(links.Cert.Certificate[0])
	if err != nil {
		return nil, err
	}
	c := &Core{
		log:       log,
		links:     links,
		trust:     store,
		localSPKI: leaf.RawSubjectPublicKeyInfo,
		dir:       dir,
		devices:   map[string]*device{},
		dialing:   map[string]bool{},
	}
	c.loadSettings()
	return c, nil
}

// Serve accepts incoming links until the listener is closed.
func (c *Core) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if addr, ok := conn.RemoteAddr().(*net.TCPAddr); ok && discovery.SameHost(addr.IP) {
			conn.Close()
			continue
		}
		go func() {
			l, err := c.links.Accept(conn)
			if err != nil {
				c.log.Debug("incoming link failed", "from", conn.RemoteAddr(), "err", err)
				return
			}
			c.attach(l)
		}()
	}
}

// HandlePeer connects to a device heard on the network unless already linked.
func (c *Core) HandlePeer(ctx context.Context, p discovery.Peer) {
	id := p.Identity.DeviceID
	if !speaksPerekat(p.Identity) && !c.foreignAllowed() {
		return
	}
	c.mu.Lock()
	// A device reached through the relay is dialled anyway: a direct link is better.
	if d := c.devices[id]; (d != nil && d.link != nil && !d.link.Relayed) || c.dialing[id] {
		c.mu.Unlock()
		return
	}
	c.dialing[id] = true
	c.mu.Unlock()

	go func() {
		l, err := c.links.Dial(ctx, p.Addr, p.Identity)
		c.mu.Lock()
		delete(c.dialing, id)
		c.mu.Unlock()
		if err != nil {
			c.log.Debug("outgoing link failed", "device", p.Identity.DeviceName, "err", err)
			return
		}
		c.attach(l)
	}()
}

func (c *Core) attach(l *link.Link) {
	id := l.Peer.DeviceID
	if !speaksPerekat(l.Peer) && !c.foreignAllowed() {
		l.Close()
		return
	}
	if _, paired := c.trust.Get(id); paired && !c.trust.Matches(id, l.Cert.Raw) {
		c.log.Warn("rejected link: certificate differs from the paired one", "device", l.Peer.DeviceName, "addr", l.RemoteIP())
		l.Close()
		return
	}

	c.mu.Lock()
	d := c.devices[id]
	if d == nil {
		d = &device{}
		c.devices[id] = d
	}
	old := d.link
	fresh := old == nil
	// Two perekat devices that hear each other at once both dial, and each
	// would keep a different connection. Agree on the one dialled by the
	// device with the smaller id. Other implementations keep the newest.
	if old != nil && !old.Relayed && l.Relayed {
		// Never trade a direct link for one through the relay.
		c.mu.Unlock()
		l.Close()
		return
	}
	if old != nil && old.Relayed == l.Relayed && speaksPerekat(l.Peer) {
		wantDialed := c.links.Self.DeviceID < id
		if old.Dialed == wantDialed && l.Dialed != wantDialed {
			c.mu.Unlock()
			l.Close()
			return
		}
	}
	d.link, d.identity, d.addr = l, l.Peer, l.RemoteIP()
	// A pairing in progress survives the device reconnecting.
	if fresh {
		d.setState(pairNone, 0)
	}
	c.mu.Unlock()

	if old != nil {
		old.Close()
	}
	paired := c.trust.Matches(id, l.Cert.Raw)
	c.log.Info("connected", "device", l.Peer.DeviceName, "addr", l.RemoteIP(), "paired", paired, "relayed", l.Relayed)
	if paired {
		go c.greet(l)
	}
	go c.readLoop(l)
}

func (c *Core) readLoop(l *link.Link) {
	for {
		p, err := l.Receive()
		if errors.Is(err, link.ErrMalformed) {
			continue
		}
		if err != nil {
			break
		}
		if p.Type == protocol.TypePair {
			c.handlePair(l, p)
			continue
		}
		// Everything but pairing is only accepted from paired devices.
		if !c.trust.Matches(l.Peer.DeviceID, l.Cert.Raw) {
			c.log.Debug("ignored packet from an unpaired device", "device", l.Peer.DeviceName, "type", p.Type)
			continue
		}
		switch p.Type {
		case protocol.TypeInput:
			c.handleInput(l, p)
		case protocol.TypeClipboard, protocol.TypeClipboardConnect:
			c.handleClipboard(l, p)
		case protocol.TypeShare:
			c.handleShare(l, p)
		case protocol.TypeRunRequest:
			c.handleRunRequest(l, p)
		case protocol.TypeRunCommand:
			c.handleCommandList(l, p)
		case protocol.TypePing:
			c.handlePing(l, p)
		case protocol.TypeRing:
			c.log.Info("asked to ring", "device", l.Peer.DeviceName)
			go desktop.Ring()
		case protocol.TypeBattery:
			c.handleBattery(l, p)
		case protocol.TypeFSRequest:
			c.handleFSRequest(l, p)
		case protocol.TypeFSResponse:
			c.handleFSResponse(p)
		case protocol.TypeShares:
			c.handleShares(l, p)
		default:
			c.log.Debug("unhandled packet", "device", l.Peer.DeviceName, "type", p.Type)
		}
	}
	l.Close()
	c.mu.Lock()
	d := c.devices[l.Peer.DeviceID]
	current := d != nil && d.link == l
	if current {
		d.link = nil
		d.setState(pairNone, 0)
		if c.remote != nil && c.remote.target == l.Peer.DeviceID {
			c.stopRemote()
		}
	}
	c.mu.Unlock()
	if current {
		c.log.Info("disconnected", "device", l.Peer.DeviceName)
	}
}

func (c *Core) handlePair(l *link.Link, p protocol.Packet) {
	var body protocol.Pair
	if err := json.Unmarshal(p.Body, &body); err != nil {
		c.log.Debug("bad pair packet", "device", l.Peer.DeviceName, "err", err)
		return
	}
	id, name := l.Peer.DeviceID, l.Peer.DeviceName

	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.devices[id]
	if d == nil || d.link != l {
		return
	}
	_, paired := c.trust.Get(id)

	if !body.Pair {
		switch {
		case paired:
			c.forget(id)
			c.log.Info("unpaired by the other device", "device", name)
		case d.state == pairRequested:
			c.log.Info("pair request was refused", "device", name)
		}
		d.setState(pairNone, 0)
		return
	}

	if d.state == pairRequested {
		d.setState(pairNone, 0)
		c.remember(l)
		c.log.Info("paired", "device", name)
		return
	}

	// A request and an acceptance look the same on the wire, so a request from
	// an already paired device cannot be auto-accepted without risking a loop.
	// Drop the old pairing and ask the user again.
	if paired {
		c.forget(id)
	}
	if skew := time.Now().Unix() - body.Timestamp; body.Timestamp == 0 || skew > maxClockSkew || skew < -maxClockSkew {
		c.log.Warn("ignored pair request: clocks differ too much", "device", name)
		return
	}
	c.expire(d, d.setState(pairIncoming, body.Timestamp))
	c.log.Info("pair request received; run `perekat pair` to accept", "device", name, "key", c.key(l, body.Timestamp))
}

// setState changes the pairing state and returns its generation.
func (d *device) setState(s pairState, ts int64) int {
	d.state, d.ts = s, ts
	d.gen++
	return d.gen
}

// expire cancels a pending pairing if it is still pending after pairTimeout.
func (c *Core) expire(d *device, gen int) {
	time.AfterFunc(pairTimeout, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if d.gen == gen {
			d.setState(pairNone, 0)
			c.log.Info("pairing timed out", "device", d.identity.DeviceName)
		}
	})
}

func (c *Core) remember(l *link.Link) {
	go c.greet(l)
	err := c.trust.Add(trust.Entry{ID: l.Peer.DeviceID, Name: l.Peer.DeviceName, Type: l.Peer.DeviceType, Cert: l.Cert.Raw})
	if err != nil {
		c.log.Error("cannot save pairing", "err", err)
	}
}

func (c *Core) forget(id string) {
	if err := c.trust.Remove(id); err != nil {
		c.log.Error("cannot remove pairing", "err", err)
	}
}

// key derives the code shown on both devices during pairing: a hash of the two
// public keys, larger first, and the timestamp of the request.
func (c *Core) key(l *link.Link, ts int64) string {
	a, b := c.localSPKI, l.Cert.RawSubjectPublicKeyInfo
	if bytes.Compare(a, b) < 0 {
		a, b = b, a
	}
	h := sha256.New()
	h.Write(a)
	h.Write(b)
	h.Write([]byte(strconv.FormatInt(ts, 10)))
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil))[:8])
}

// Refresh announces this device again, so devices that missed it connect.
func (c *Core) Refresh() {
	if c.OnRefresh != nil {
		c.OnRefresh()
	}
}

// Self returns the identity this device announces.
func (c *Core) Self() protocol.Identity {
	return c.links.Self
}

// Devices lists connected devices and paired ones that are offline.
func (c *Core) Devices() []DeviceInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []DeviceInfo
	seen := map[string]bool{}
	for id, d := range c.devices {
		if d.link == nil {
			continue
		}
		seen[id] = true
		out = append(out, c.info(id, d))
	}
	for _, e := range c.trust.List() {
		if !seen[e.ID] {
			out = append(out, DeviceInfo{ID: e.ID, Name: e.Name, Type: e.Type, Paired: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name+out[i].ID < out[j].Name+out[j].ID })
	return out
}

func (c *Core) info(id string, d *device) DeviceInfo {
	_, paired := c.trust.Get(id)
	info := DeviceInfo{
		ID:         id,
		Name:       d.identity.DeviceName,
		Type:       d.identity.DeviceType,
		Addr:       d.addr.String(),
		Connected:  true,
		Paired:     paired,
		Relayed:    d.link.Relayed,
		Foreign:    !speaksPerekat(d.identity),
		Remote:     c.remote != nil && c.remote.target == id,
		CanRemote:  paired && accepts(d.identity, protocol.TypeInput),
		CanBrowse:  paired && accepts(d.identity, protocol.TypeFSRequest),
		CanRing:    paired && accepts(d.identity, protocol.TypeRing),
		Commands:   d.commands,
		Battery:    d.battery,
		Shares:     d.shares,
		CanConvert: d.office,
	}
	switch d.state {
	case pairRequested:
		info.Pairing, info.Key = "requested", c.key(d.link, d.ts)
	case pairIncoming:
		info.Pairing, info.Key = "incoming", c.key(d.link, d.ts)
	}
	return info
}

// Pair accepts a pending request from the device, or sends one if none is pending.
func (c *Core) Pair(query string) (DeviceInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, err := c.find(query)
	if err != nil {
		return DeviceInfo{}, err
	}
	d := c.devices[id]
	if d == nil || d.link == nil {
		return DeviceInfo{}, errors.New("device is not connected")
	}

	switch d.state {
	case pairIncoming:
		if err := send(d.link, protocol.TypePair, protocol.Pair{Pair: true}); err != nil {
			return DeviceInfo{}, err
		}
		d.setState(pairNone, 0)
		c.remember(d.link)
		c.log.Info("paired", "device", d.identity.DeviceName)
	case pairRequested:
		return DeviceInfo{}, errors.New("already waiting for the other device to accept")
	default:
		if _, paired := c.trust.Get(id); paired {
			return DeviceInfo{}, errors.New("already paired")
		}
		ts := time.Now().Unix()
		if err := send(d.link, protocol.TypePair, protocol.Pair{Pair: true, Timestamp: ts}); err != nil {
			return DeviceInfo{}, err
		}
		c.expire(d, d.setState(pairRequested, ts))
	}
	return c.info(id, d), nil
}

// Unpair forgets the device, refuses its pending request, or withdraws ours.
func (c *Core) Unpair(query string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, err := c.find(query)
	if err != nil {
		return err
	}
	d := c.devices[id]
	_, paired := c.trust.Get(id)
	if !paired && (d == nil || d.state == pairNone) {
		return errors.New("device is not paired")
	}
	if d != nil && d.link != nil {
		send(d.link, protocol.TypePair, protocol.Pair{Pair: false})
		d.setState(pairNone, 0)
	}
	if paired {
		c.forget(id)
	}
	return nil
}

// handleInput replays a mouse event sent by a paired device.
func (c *Core) handleInput(l *link.Link, p protocol.Packet) {
	var e input.Event
	if err := json.Unmarshal(p.Body, &e); err != nil {
		return
	}
	c.injectMu.Lock()
	defer c.injectMu.Unlock()
	if !c.ensureInjector() || (e.IsMotion() && time.Now().Before(c.injectorReady)) {
		return
	}
	if err := c.injector.Inject(e); err != nil {
		c.log.Warn("mouse event was not delivered", "err", err)
	}
}

// prepareInjector creates the virtual mouse ahead of time for a device that
// may take control, so its first movements are not lost to the warm-up.
func (c *Core) prepareInjector() {
	go func() {
		c.injectMu.Lock()
		defer c.injectMu.Unlock()
		c.ensureInjector()
	}()
}

// ensureInjector creates the virtual mouse on first use. It must be called
// with c.injectMu held.
func (c *Core) ensureInjector() bool {
	if c.injector != nil {
		return true
	}
	inj, err := input.NewInjector()
	if err != nil {
		c.log.Warn("cannot control the local mouse", "err", err)
		return false
	}
	c.injector = inj
	c.injectorReady = time.Now().Add(injectorWarmup)
	return true
}

// Remote toggles remote control of a device: while it is on, the local mouse
// moves that device's pointer instead of ours. Turning it on for another device
// moves the session there. It reports whether a session is now active.
func (c *Core) Remote(query string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, err := c.find(query)
	if err != nil {
		return false, err
	}
	if c.remote != nil {
		same := c.remote.target == id
		c.stopRemote()
		if same {
			return false, nil
		}
	}

	d := c.devices[id]
	if d == nil || d.link == nil {
		return false, errors.New("device is not connected")
	}
	if !c.trust.Matches(id, d.link.Cert.Raw) {
		return false, errors.New("device is not paired")
	}
	if !accepts(d.identity, protocol.TypeInput) {
		return false, errors.New("device cannot be controlled with a mouse")
	}

	r := &remote{target: id, events: make(chan input.Event, 1024), quit: make(chan struct{})}
	// Capture callbacks must not block: if the network stalls, drop events.
	r.stop, err = input.StartCapture(func(e input.Event) {
		select {
		case r.events <- e:
		default:
		}
	})
	if err != nil {
		return false, err
	}
	c.remote = r
	go c.pump(r, d.link)
	c.log.Info("remote control on", "device", d.identity.DeviceName)
	return true, nil
}

// RemoteOff ends the remote-control session, if any.
func (c *Core) RemoteOff() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopRemote()
}

// stopRemote must be called with c.mu held.
func (c *Core) stopRemote() {
	r := c.remote
	if r == nil {
		return
	}
	c.remote = nil
	r.stop()
	close(r.quit)
	c.log.Info("remote control off")
}

// pump forwards captured events, merging movements that pile up while a
// packet is being written.
func (c *Core) pump(r *remote, l *link.Link) {
	for {
		var batch []input.Event
		select {
		case <-r.quit:
			return
		case e := <-r.events:
			batch = append(batch, e)
		}
		for more := true; more; {
			select {
			case e := <-r.events:
				last := &batch[len(batch)-1]
				if e.IsMotion() && last.IsMotion() {
					last.DX += e.DX
					last.DY += e.DY
					last.Wheel += e.Wheel
					last.HWheel += e.HWheel
				} else {
					batch = append(batch, e)
				}
			default:
				more = false
			}
		}
		for _, e := range batch {
			if err := send(l, protocol.TypeInput, e); err != nil {
				c.mu.Lock()
				if c.remote == r {
					c.stopRemote()
				}
				c.mu.Unlock()
				return
			}
		}
	}
}

// find resolves a device id or a case-insensitive device name.
func (c *Core) find(query string) (string, error) {
	names := map[string]string{}
	for id, d := range c.devices {
		if d.link != nil {
			names[id] = d.identity.DeviceName
		}
	}
	for _, e := range c.trust.List() {
		if _, ok := names[e.ID]; !ok {
			names[e.ID] = e.Name
		}
	}
	if _, ok := names[query]; ok {
		return query, nil
	}
	var matches []string
	for id, name := range names {
		if strings.EqualFold(name, query) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no device named %q", query)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("several devices are named %q; use the id", query)
	}
}

// accepts reports whether the device handles packets of the given type.
func accepts(id protocol.Identity, typ string) bool {
	return slices.Contains(id.IncomingCapabilities, typ)
}

// pairedLink returns the live link to a paired device named by query.
func (c *Core) pairedLink(query string) (*link.Link, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id, err := c.find(query)
	if err != nil {
		return nil, err
	}
	d := c.devices[id]
	if d == nil || d.link == nil {
		return nil, errors.New("device is not connected")
	}
	if !c.trust.Matches(id, d.link.Cert.Raw) {
		return nil, errors.New("device is not paired")
	}
	return d.link, nil
}

// pairedLinks returns the live links to all paired devices.
func (c *Core) pairedLinks() []*link.Link {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*link.Link
	for id, d := range c.devices {
		if d.link != nil && c.trust.Matches(id, d.link.Cert.Raw) {
			out = append(out, d.link)
		}
	}
	return out
}

// greet brings a paired device up to date right after it connects or pairs.
func (c *Core) greet(l *link.Link) {
	if input.Supported && accepts(l.Peer, protocol.TypeInput) {
		c.prepareInjector()
	}
	c.greetClipboard(l)
	c.sendCommandList(l)
	if accepts(l.Peer, protocol.TypeRunRequest) {
		send(l, protocol.TypeRunRequest, map[string]bool{"requestCommandList": true})
	}
	c.sendBattery(l)
	c.sendShares(l)
}

// speaksPerekat reports whether the device understands perekat's own packets.
func speaksPerekat(id protocol.Identity) bool {
	return accepts(id, protocol.TypePeer)
}

func portString(port int) string {
	return strconv.Itoa(port)
}

func send(l *link.Link, typ string, body any) error {
	p, err := protocol.New(typ, body)
	if err != nil {
		return err
	}
	return l.Send(p)
}
