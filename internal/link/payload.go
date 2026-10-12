package link

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// Payloads travel over their own connection. On a direct link the sender
// listens on a port of this range and names it in the packet, and the receiver
// connects to it.
const (
	payloadPortMin = 1739
	payloadPortMax = 1764
	payloadWait    = 30 * time.Second
)

// PayloadTransport opens the side connections that carry payloads for a link.
type PayloadTransport interface {
	// Offer reserves a transfer id for the packet. wait blocks until the
	// peer connects to it; cancel releases it if wait is never called.
	Offer() (id int, wait func() (net.Conn, error), cancel func(), err error)
	// Open connects to a transfer the peer offered.
	Open(ctx context.Context, id int) (net.Conn, error)
}

type directPayload struct{ l *Link }

func (d directPayload) Offer() (int, func() (net.Conn, error), func(), error) {
	var lastErr error
	for port := payloadPortMin; port <= payloadPortMax; port++ {
		ln, err := net.Listen("tcp4", ":"+strconv.Itoa(port))
		if err != nil {
			lastErr = err
			continue
		}
		wait := func() (net.Conn, error) {
			defer ln.Close()
			ln.(*net.TCPListener).SetDeadline(time.Now().Add(payloadWait))
			return ln.Accept()
		}
		return port, wait, func() { ln.Close() }, nil
	}
	return 0, nil, nil, fmt.Errorf("no free payload port: %w", lastErr)
}

func (d directPayload) Open(ctx context.Context, id int) (net.Conn, error) {
	dialer := net.Dialer{Timeout: handshakeTimeout}
	return dialer.DialContext(ctx, "tcp4", net.JoinHostPort(d.l.RemoteIP().String(), strconv.Itoa(id)))
}

// SendPayload waits for the peer of l to connect and streams src to it.
func (c Config) SendPayload(l *Link, wait func() (net.Conn, error), src io.Reader) error {
	conn, err := wait()
	if err != nil {
		return fmt.Errorf("the other device did not start the download: %w", err)
	}
	tc := tls.Server(conn, &tls.Config{
		Certificates: []tls.Certificate{c.Cert},
		ClientAuth:   tls.RequireAnyClientCert,
		MinVersion:   tls.VersionTLS12,
	})
	defer tc.Close()
	if err := handshakeWith(tc, l); err != nil {
		return err
	}
	_, err = io.Copy(tc, src)
	return err
}

// FetchPayload downloads size bytes of the transfer id that the peer of l offers.
func (c Config) FetchPayload(ctx context.Context, l *Link, id int, dst io.Writer, size int64) error {
	conn, err := l.Payload.Open(ctx, id)
	if err != nil {
		return err
	}
	tc := tls.Client(conn, &tls.Config{
		Certificates:       []tls.Certificate{c.Cert},
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	})
	defer tc.Close()
	if err := handshakeWith(tc, l); err != nil {
		return err
	}
	_, err = io.CopyN(dst, tc, size)
	return err
}

// handshakeWith completes TLS and checks that the other end holds the same
// certificate as the peer of l, so nobody else on the network can take or
// supply the payload.
func handshakeWith(tc *tls.Conn, l *Link) error {
	tc.SetDeadline(time.Now().Add(handshakeTimeout))
	if err := tc.Handshake(); err != nil {
		return fmt.Errorf("tls handshake: %w", err)
	}
	tc.SetDeadline(time.Time{})
	certs := tc.ConnectionState().PeerCertificates
	if len(certs) == 0 || !bytes.Equal(certs[0].Raw, l.Cert.Raw) {
		return errors.New("payload connection is not from the paired device")
	}
	return nil
}
