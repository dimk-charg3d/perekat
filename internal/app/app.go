// Package app assembles the daemon: discovery, links and the local API.
package app

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/charg3d/perekat/internal/core"
	"github.com/charg3d/perekat/internal/discovery"
	"github.com/charg3d/perekat/internal/identity"
	"github.com/charg3d/perekat/internal/ipc"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
	"github.com/charg3d/perekat/internal/trust"
)

// Run starts the daemon and blocks until ctx is cancelled or a part of it fails.
func Run(ctx context.Context, log *slog.Logger) error {
	dir, err := identity.Dir()
	if err != nil {
		return err
	}
	self, err := identity.LoadOrCreate(dir)
	if err != nil {
		return err
	}
	cert, err := identity.LoadOrCreateCert(dir, self.ID)
	if err != nil {
		return err
	}
	store, err := trust.Open(filepath.Join(dir, "paired.json"))
	if err != nil {
		return err
	}

	ctx, stop := context.WithCancel(ctx)
	defer stop()

	ln, port, err := link.Listen(ctx)
	if err != nil {
		return err
	}
	me := protocol.Identity{
		DeviceID:             self.ID,
		DeviceName:           self.Name,
		DeviceType:           self.Type,
		ProtocolVersion:      protocol.Version,
		IncomingCapabilities: protocol.Capabilities,
		OutgoingCapabilities: protocol.Capabilities,
		TCPPort:              port,
	}
	c, err := core.New(log, link.Config{Self: me, Cert: cert}, store, dir)
	if err != nil {
		return err
	}
	log.Info("starting", "id", self.ID, "name", self.Name, "port", port)

	go c.RunClipboard(ctx)
	go c.RunBattery(ctx)

	errc := make(chan error, 3)
	go func() { errc <- c.Serve(ctx, ln) }()
	go func() { errc <- ipc.Serve(ctx, c) }()

	peers := make(chan discovery.Peer)
	svc := discovery.New(me, log)
	c.OnRefresh = svc.Refresh
	go func() { errc <- svc.Run(ctx, peers) }()
	go func() {
		for p := range peers {
			c.HandlePeer(ctx, p)
		}
	}()

	// The first service to stop takes the daemon down; on shutdown that is a nil error.
	return <-errc
}
