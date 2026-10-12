// Package app assembles the daemon: discovery, links and the local API.
package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"

	"github.com/charg3d/perekat/internal/core"
	"github.com/charg3d/perekat/internal/discovery"
	"github.com/charg3d/perekat/internal/identity"
	"github.com/charg3d/perekat/internal/input"
	"github.com/charg3d/perekat/internal/ipc"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/netfs"
	"github.com/charg3d/perekat/internal/protocol"
	"github.com/charg3d/perekat/internal/trust"
)

// Extra, if set, is started with every (re)start of the daemon and must
// return when its context is cancelled. The phone app uses it to serve the
// interface.
var Extra func(ctx context.Context, c *core.Core) error

// Run starts the daemon and blocks until ctx is cancelled or a part of it
// fails. The daemon restarts itself when a setting needs it.
func Run(ctx context.Context, log *slog.Logger) error {
	for {
		restart, err := run(ctx, log)
		if !restart || ctx.Err() != nil {
			return err
		}
		log.Info("restarting")
	}
}

func run(ctx context.Context, log *slog.Logger) (restart bool, err error) {
	fail := func(err error) (bool, error) { return false, err }
	dir, err := identity.Dir()
	if err != nil {
		return fail(err)
	}
	self, err := identity.LoadOrCreate(dir)
	if err != nil {
		return fail(err)
	}
	cert, err := identity.LoadOrCreateCert(dir, self.ID)
	if err != nil {
		return fail(err)
	}
	store, err := trust.Open(filepath.Join(dir, "paired.json"))
	if err != nil {
		return fail(err)
	}

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	ln, port, err := link.Listen(ctx)
	if err != nil {
		return fail(err)
	}
	capabilities := protocol.Capabilities
	if !input.Supported {
		capabilities = slices.DeleteFunc(slices.Clone(capabilities), func(t string) bool { return t == protocol.TypeInput })
	}
	me := protocol.Identity{
		DeviceID:             self.ID,
		DeviceName:           self.Name,
		DeviceType:           self.Type,
		ProtocolVersion:      protocol.Version,
		IncomingCapabilities: capabilities,
		OutgoingCapabilities: capabilities,
		TCPPort:              port,
	}
	c, err := core.New(log, link.Config{Self: me, Cert: cert}, store, dir)
	if err != nil {
		return fail(err)
	}
	log.Info("starting", "id", self.ID, "name", self.Name, "port", port)

	var restarting atomic.Bool
	c.OnRestart = func() {
		restarting.Store(true)
		stop()
	}
	defer c.Close()
	defer netfs.Mount(c, log)()

	go c.RunClipboard(ctx)
	go c.RunInputShare(ctx.Done())
	go c.RunMenus(ctx.Done())
	go c.RunBattery(ctx)
	go c.RunRelay(ctx)

	services := []func() error{
		func() error { return c.Serve(ctx, ln) },
		func() error { return ipc.Serve(ctx, c) },
	}
	// PEREKAT_NO_LAN turns local discovery off, leaving only the relay
	// server. It exists to exercise the relay on a single network.
	if os.Getenv("PEREKAT_NO_LAN") == "" {
		peers := make(chan discovery.Peer)
		svc := discovery.New(me, log)
		c.OnRefresh = svc.Refresh
		services = append(services, func() error { return svc.Run(ctx, peers) })
		go func() {
			for p := range peers {
				c.HandlePeer(ctx, p)
			}
		}()
	}
	if Extra != nil {
		services = append(services, func() error { return Extra(ctx, c) })
	}
	errc := make(chan error, len(services))
	for _, service := range services {
		go func() { errc <- service() }()
	}

	// The first service to stop takes the others down; wait for all of them,
	// so the ports and the socket are free again before a restart.
	err = <-errc
	stop()
	for range len(services) - 1 {
		<-errc
	}
	if restarting.Load() {
		return true, nil
	}
	return false, err
}
