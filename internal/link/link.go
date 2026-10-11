// Package link establishes encrypted connections between devices.
//
// A link starts as plain TCP: the side that dialled sends its identity in the
// clear, then both switch to TLS with the roles inverted (the dialler is the
// TLS server) and exchange identities again over the encrypted channel.
package link

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/protocol"
)

const (
	handshakeTimeout = 10 * time.Second
	maxIdentitySize  = 8 * 1024
	readBufferSize   = 64 * 1024
	maxPacketSize    = 32 << 20
)

// Config holds what this device presents to its peers.
type Config struct {
	Self protocol.Identity
	Cert tls.Certificate
}

// Link is an established encrypted connection to one device.
type Link struct {
	Peer protocol.Identity
	Cert *x509.Certificate
	// Dialed is set when this side opened the connection.
	Dialed bool

	conn *tls.Conn
	r    *bufio.Reader
	wmu  sync.Mutex
}

// Listen opens the first free TCP port of the KDE Connect range.
func Listen(ctx context.Context) (net.Listener, int, error) {
	var lc net.ListenConfig
	var lastErr error
	for port := protocol.DefaultPort; port <= protocol.MaxPort; port++ {
		ln, err := lc.Listen(ctx, "tcp4", ":"+strconv.Itoa(port))
		if err == nil {
			return ln, port, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("no free port in %d-%d: %w", protocol.DefaultPort, protocol.MaxPort, lastErr)
}

// Dial connects to a device that announced itself and expects it to prove the
// same device id over the encrypted channel.
func (c Config) Dial(ctx context.Context, ip net.IP, announced protocol.Identity) (*Link, error) {
	d := net.Dialer{Timeout: handshakeTimeout}
	conn, err := d.DialContext(ctx, "tcp4", net.JoinHostPort(ip.String(), strconv.Itoa(announced.TCPPort)))
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := c.writeIdentity(conn); err != nil {
		conn.Close()
		return nil, err
	}
	tc := tls.Server(conn, &tls.Config{
		Certificates: []tls.Certificate{c.Cert},
		ClientAuth:   tls.RequireAnyClientCert,
		MinVersion:   tls.VersionTLS12,
	})
	l, err := c.secure(tc, announced.DeviceID)
	if err != nil {
		return nil, err
	}
	l.Dialed = true
	return l, nil
}

// Accept completes the handshake on a connection opened by another device.
func (c Config) Accept(conn net.Conn) (*Link, error) {
	conn.SetDeadline(time.Now().Add(handshakeTimeout))
	line, err := readLine(conn, maxIdentitySize)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read identity: %w", err)
	}
	plain, err := decodeIdentity(line)
	if err != nil {
		conn.Close()
		return nil, err
	}
	// Trust comes from pinning the certificate at pairing time, not from a CA.
	tc := tls.Client(conn, &tls.Config{
		Certificates:       []tls.Certificate{c.Cert},
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	})
	return c.secure(tc, plain.DeviceID)
}

func (c Config) secure(tc *tls.Conn, expectID string) (*Link, error) {
	fail := func(err error) (*Link, error) {
		tc.Close()
		return nil, err
	}
	if expectID == c.Self.DeviceID {
		return fail(errors.New("connection from ourselves"))
	}
	if err := tc.Handshake(); err != nil {
		return fail(fmt.Errorf("tls handshake: %w", err))
	}
	if err := c.writeIdentity(tc); err != nil {
		return fail(err)
	}
	r := bufio.NewReaderSize(tc, readBufferSize)
	line, err := r.ReadSlice('\n')
	if err != nil {
		return fail(fmt.Errorf("read secure identity: %w", err))
	}
	peer, err := decodeIdentity(line)
	if err != nil {
		return fail(err)
	}
	if peer.DeviceID != expectID {
		return fail(fmt.Errorf("device id changed during handshake: %s, then %s", expectID, peer.DeviceID))
	}
	certs := tc.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return fail(errors.New("peer sent no certificate"))
	}
	if certs[0].Subject.CommonName != peer.DeviceID {
		return fail(fmt.Errorf("certificate is for %q, not %s", certs[0].Subject.CommonName, peer.DeviceID))
	}
	tc.SetDeadline(time.Time{})
	return &Link{Peer: peer, Cert: certs[0], conn: tc, r: r}, nil
}

func (c Config) writeIdentity(conn net.Conn) error {
	p, err := protocol.New(protocol.TypeIdentity, c.Self)
	if err != nil {
		return err
	}
	wire, err := p.Encode()
	if err != nil {
		return err
	}
	_, err = conn.Write(wire)
	return err
}

func decodeIdentity(line []byte) (protocol.Identity, error) {
	p, err := protocol.Decode(line)
	if err != nil {
		return protocol.Identity{}, err
	}
	id, err := p.Identity()
	if err != nil {
		return protocol.Identity{}, err
	}
	if id.ProtocolVersion < protocol.Version {
		return protocol.Identity{}, fmt.Errorf("protocol version %d is too old", id.ProtocolVersion)
	}
	return id, nil
}

// readLine reads up to a newline one byte at a time, so nothing past it is
// consumed before the connection is handed to TLS.
func readLine(conn net.Conn, limit int) ([]byte, error) {
	var line []byte
	b := make([]byte, 1)
	for len(line) < limit {
		if _, err := conn.Read(b); err != nil {
			return nil, err
		}
		if b[0] == '\n' {
			return line, nil
		}
		line = append(line, b[0])
	}
	return nil, errors.New("line too long")
}

// Send writes one packet. It is safe for concurrent use.
func (l *Link) Send(p protocol.Packet) error {
	wire, err := p.Encode()
	if err != nil {
		return err
	}
	l.wmu.Lock()
	defer l.wmu.Unlock()
	_, err = l.conn.Write(wire)
	return err
}

// ErrMalformed is returned by Receive for a packet that cannot be parsed. The
// link stays usable.
var ErrMalformed = errors.New("malformed packet")

// Receive blocks until the next packet arrives.
func (l *Link) Receive() (protocol.Packet, error) {
	var long []byte
	for {
		chunk, err := l.r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			if len(long)+len(chunk) > maxPacketSize {
				return protocol.Packet{}, errors.New("packet too large")
			}
			long = append(long, chunk...)
			continue
		}
		if err != nil {
			return protocol.Packet{}, err
		}
		if long != nil {
			chunk = append(long, chunk...)
		}
		p, err := protocol.Decode(chunk)
		if err != nil {
			return protocol.Packet{}, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		return p, nil
	}
}

// RemoteIP returns the address of the peer.
func (l *Link) RemoteIP() net.IP {
	return l.conn.RemoteAddr().(*net.TCPAddr).IP
}

func (l *Link) Close() error {
	return l.conn.Close()
}
