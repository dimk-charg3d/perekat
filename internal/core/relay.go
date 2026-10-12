package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/charg3d/perekat/internal/deploy"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
	"github.com/charg3d/perekat/internal/relay"
)

// Remote access: with a relay server configured, devices that are not on the
// same network link up through it. A direct link is always preferred.

const (
	relayLabelLink = "link"
	// relayRetry is how often missing links to online devices are retried.
	relayRetry = 20 * time.Second
	// relayProbe is how long a new server gets to answer before it is rejected.
	relayProbe = 10 * time.Second
)

// RelayStatus describes the remote-access setup.
type RelayStatus struct {
	Configured bool   `json:"configured"`
	Host       string `json:"host,omitempty"`
	Connected  bool   `json:"connected"`
	// Code lets another device join the same server.
	Code string `json:"code,omitempty"`
}

type relaySession struct {
	cfg    relay.Config
	client *relay.Client
	stop   context.CancelFunc
}

func (c *Core) relayPath() string {
	return filepath.Join(c.dir, "relay.json")
}

// RunRelay connects to the configured relay server, if any, and keeps links
// through it alive until ctx is cancelled.
func (c *Core) RunRelay(ctx context.Context) {
	c.relayMu.Lock()
	c.relayCtx = ctx
	c.relayMu.Unlock()

	var cfg relay.Config
	if data, err := os.ReadFile(c.relayPath()); err == nil && json.Unmarshal(data, &cfg) == nil {
		c.startRelay(cfg)
	}

	t := time.NewTicker(relayRetry)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s := c.currentRelay(); s != nil {
			for _, peer := range s.client.Peers() {
				c.relayDial(s, peer)
			}
		}
	}
}

func (c *Core) currentRelay() *relaySession {
	c.relayMu.Lock()
	defer c.relayMu.Unlock()
	return c.relay
}

func (c *Core) startRelay(cfg relay.Config) *relaySession {
	c.relayMu.Lock()
	defer c.relayMu.Unlock()
	if c.relay != nil {
		c.relay.stop()
		c.dropRelayedLinks()
	}
	ctx, stop := context.WithCancel(c.relayCtx)
	s := &relaySession{cfg: cfg, stop: stop}
	s.client = &relay.Client{
		Config:     cfg,
		Self:       c.links.Self,
		Log:        c.log,
		OnOnline:   func(peer protocol.Identity) { c.relayDial(s, peer) },
		OnIncoming: func(from, label string, conn net.Conn) { c.relayIncoming(s, from, label, conn) },
	}
	c.relay = s
	go s.client.Run(ctx)
	return s
}

// relayDial links to a device through the server unless a link already
// exists. Of two devices only the one with the smaller id dials, so they do
// not open two links at once.
func (c *Core) relayDial(s *relaySession, peer protocol.Identity) {
	id := peer.DeviceID
	if c.links.Self.DeviceID >= id {
		return
	}
	c.mu.Lock()
	if d := c.devices[id]; (d != nil && d.link != nil) || c.dialing[id] {
		c.mu.Unlock()
		return
	}
	c.dialing[id] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.dialing, id)
		c.mu.Unlock()
	}()

	conn, err := s.client.Open(c.relayCtx, id, relayLabelLink)
	if err != nil {
		c.log.Debug("relay link failed", "device", peer.DeviceName, "err", err)
		return
	}
	l, err := c.links.DialConn(conn, peer)
	if err != nil {
		c.log.Debug("relay link failed", "device", peer.DeviceName, "err", err)
		return
	}
	l.Relayed, l.Payload = true, s.client.Payload(id)
	c.attach(l)
}

func (c *Core) relayIncoming(s *relaySession, from, label string, conn net.Conn) {
	if label != relayLabelLink {
		conn.Close()
		return
	}
	l, err := c.links.Accept(conn)
	if err != nil {
		c.log.Debug("incoming relay link failed", "from", from, "err", err)
		return
	}
	l.Relayed, l.Payload = true, s.client.Payload(l.Peer.DeviceID)
	c.attach(l)
}

// RelayStatus reports the remote-access setup.
func (c *Core) RelayStatus() RelayStatus {
	s := c.currentRelay()
	if s == nil {
		return RelayStatus{}
	}
	return RelayStatus{Configured: true, Host: s.cfg.Host, Connected: s.client.Connected(), Code: s.cfg.Code()}
}

// RelaySetup installs a relay server on the target machine and joins it.
func (c *Core) RelaySetup(ctx context.Context, t deploy.Target) error {
	cfg, err := deploy.Install(ctx, t, func(step string) { c.log.Info("relay setup", "step", step) })
	if err != nil {
		return err
	}
	return c.useRelay(cfg, "the server is installed, but this device cannot reach it: "+
		"open TCP port "+portString(cfg.Port)+" in the hosting provider's firewall")
}

// RelayJoin joins a relay server described by a code from another device.
func (c *Core) RelayJoin(code string) error {
	cfg, err := relay.ParseCode(code)
	if err != nil {
		return err
	}
	return c.useRelay(cfg, "cannot reach the relay server")
}

// useRelay switches to a relay configuration after checking that it works.
func (c *Core) useRelay(cfg relay.Config, failure string) error {
	ctx, cancel := context.WithTimeout(c.relayCtx, relayProbe)
	defer cancel()
	probe := relay.Client{Config: cfg, Log: c.log}
	if err := probe.Probe(ctx); err != nil {
		return fmt.Errorf("%s (%w)", failure, err)
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(c.relayPath(), data, 0o600); err != nil {
		return err
	}
	c.startRelay(cfg)
	return nil
}

func (c *Core) stopRelay() {
	c.relayMu.Lock()
	defer c.relayMu.Unlock()
	if c.relay != nil {
		c.relay.stop()
		c.relay = nil
	}
	c.dropRelayedLinks()
}

// dropRelayedLinks closes the links that ran through a relay session that is
// going away; the next session opens new ones.
func (c *Core) dropRelayedLinks() {
	c.mu.Lock()
	var stale []*link.Link
	for _, d := range c.devices {
		if d.link != nil && d.link.Relayed {
			stale = append(stale, d.link)
		}
	}
	c.mu.Unlock()
	for _, l := range stale {
		l.Close()
	}
}

// RelayLeave stops using the relay server. The server itself is left running.
func (c *Core) RelayLeave() error {
	c.stopRelay()
	err := os.Remove(c.relayPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
