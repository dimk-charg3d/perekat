package relay

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
)

const dialTimeout = 10 * time.Second

// Client keeps a device connected to a relay server.
type Client struct {
	Config Config
	Self   protocol.Identity
	Log    *slog.Logger
	// OnOnline is called for every device reachable through the server:
	// those already there when we connect and those that arrive later.
	OnOnline func(protocol.Identity)
	// OnIncoming is called with a stream another device opened to us.
	OnIncoming func(from, label string, conn net.Conn)

	mu        sync.Mutex
	connected bool
	peers     map[string]protocol.Identity
	expected  map[string]chan net.Conn
}

// Run stays connected to the server, reconnecting as needed, until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := c.session(ctx)
		c.mu.Lock()
		c.connected, c.peers = false, nil
		c.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		c.Log.Warn("relay connection lost", "server", c.Config.Host, "err", err)
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, 30*time.Second)
	}
}

// dial opens an authenticated connection and sends the first message.
func (c *Client) dial(ctx context.Context, first message) (net.Conn, error) {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: dialTimeout},
		Config: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// The server's certificate is self-signed; it is recognised by
			// its fingerprint instead of by a certificate authority.
			InsecureSkipVerify: true,
			VerifyPeerCertificate: func(certs [][]byte, _ [][]*x509.Certificate) error {
				if len(certs) == 0 || Fingerprint(certs[0]) != c.Config.Fingerprint {
					return errors.New("this is not the relay server that was set up")
				}
				return nil
			},
		},
	}
	conn, err := d.DialContext(ctx, "tcp", c.Config.addr())
	if err != nil {
		return nil, err
	}
	first.Key = c.Config.Key
	conn.SetDeadline(time.Now().Add(dialTimeout))
	if err := writeMessage(conn, first); err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return conn, nil
}

func (c *Client) session(ctx context.Context) error {
	conn, err := c.dial(ctx, message{Op: "hello", Identity: &c.Self})
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	go func() {
		t := time.NewTicker(pingInterval)
		defer t.Stop()
		for range t.C {
			conn.SetWriteDeadline(time.Now().Add(dialTimeout))
			if writeMessage(conn, message{Op: "ping"}) != nil {
				conn.Close()
				return
			}
		}
	}()

	for {
		m, err := readMessage(conn)
		if err != nil {
			return err
		}
		switch m.Op {
		case "peers":
			c.mu.Lock()
			c.connected = true
			c.peers = map[string]protocol.Identity{}
			for _, p := range m.Peers {
				c.peers[p.DeviceID] = p
			}
			c.mu.Unlock()
			c.Log.Info("connected to relay", "server", c.Config.Host, "devices", len(m.Peers))
			for _, p := range m.Peers {
				go c.OnOnline(p)
			}
		case "online":
			if m.Identity != nil {
				c.mu.Lock()
				c.peers[m.Identity.DeviceID] = *m.Identity
				c.mu.Unlock()
				go c.OnOnline(*m.Identity)
			}
		case "offline":
			c.mu.Lock()
			delete(c.peers, m.ID)
			c.mu.Unlock()
		case "incoming":
			go c.accept(ctx, m)
		}
	}
}

// accept opens our end of a stream another device asked for.
func (c *Client) accept(ctx context.Context, m message) {
	conn, err := c.dial(ctx, message{Op: "accept", Session: m.Session})
	if err == nil {
		err = awaitOK(conn)
	}
	if err != nil {
		c.Log.Debug("relay stream failed", "from", m.From, "err", err)
		return
	}
	c.mu.Lock()
	waiting := c.expected[m.From+"\x00"+m.Label]
	delete(c.expected, m.From+"\x00"+m.Label)
	c.mu.Unlock()
	if waiting != nil {
		waiting <- conn
		return
	}
	c.OnIncoming(m.From, m.Label, conn)
}

func awaitOK(conn net.Conn) error {
	conn.SetDeadline(time.Now().Add(sessionTimeout + dialTimeout))
	reply, err := readMessage(conn)
	conn.SetDeadline(time.Time{})
	if err == nil && reply.Op != "ok" {
		err = errors.New(reply.Error)
	}
	if err != nil {
		conn.Close()
	}
	return err
}

// Probe checks that the server is reachable and accepts the key, without
// announcing this device.
func (c *Client) Probe(ctx context.Context) error {
	conn, err := c.dial(ctx, message{Op: "probe"})
	if err != nil {
		return err
	}
	defer conn.Close()
	return awaitOK(conn)
}

// Open asks the server for a stream to another device.
func (c *Client) Open(ctx context.Context, to, label string) (net.Conn, error) {
	conn, err := c.dial(ctx, message{Op: "connect", From: c.Self.DeviceID, To: to, Label: label})
	if err != nil {
		return nil, err
	}
	if err := awaitOK(conn); err != nil {
		return nil, fmt.Errorf("relay: %w", err)
	}
	return conn, nil
}

// expect arranges for the next stream with this label from a device to be
// returned by wait instead of going to OnIncoming.
func (c *Client) expect(from, label string) (wait func() (net.Conn, error), cancel func()) {
	key := from + "\x00" + label
	arrived := make(chan net.Conn, 1)
	c.mu.Lock()
	if c.expected == nil {
		c.expected = map[string]chan net.Conn{}
	}
	c.expected[key] = arrived
	c.mu.Unlock()

	cancel = func() {
		c.mu.Lock()
		if c.expected[key] == arrived {
			delete(c.expected, key)
		}
		c.mu.Unlock()
	}
	wait = func() (net.Conn, error) {
		defer cancel()
		select {
		case conn := <-arrived:
			return conn, nil
		case <-time.After(30 * time.Second):
			return nil, errors.New("timed out")
		}
	}
	return wait, cancel
}

// Connected reports whether the control connection is up.
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Peers lists the devices currently reachable through the server.
func (c *Client) Peers() []protocol.Identity {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]protocol.Identity, 0, len(c.peers))
	for _, p := range c.peers {
		out = append(out, p)
	}
	return out
}

// Payload returns the transport that carries a relayed link's payloads
// through the server.
func (c *Client) Payload(peer string) link.PayloadTransport {
	return relayPayload{c, peer}
}

type relayPayload struct {
	c    *Client
	peer string
}

func payloadLabel(id int) string { return "payload:" + strconv.Itoa(id) }

func (p relayPayload) Offer() (int, func() (net.Conn, error), func(), error) {
	var raw [4]byte
	rand.Read(raw[:])
	id := int(binary.BigEndian.Uint32(raw[:]) >> 1)
	wait, cancel := p.c.expect(p.peer, payloadLabel(id))
	return id, wait, cancel, nil
}

func (p relayPayload) Open(ctx context.Context, id int) (net.Conn, error) {
	return p.c.Open(ctx, p.peer, payloadLabel(id))
}
