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

// Payloads travel over their own connection: the sender listens on a port of
// this range and names it in the packet, the receiver connects and reads.
const (
	payloadPortMin = 1739
	payloadPortMax = 1764
	payloadWait    = 30 * time.Second
)

// ListenPayload opens a port for one payload download.
func ListenPayload() (net.Listener, int, error) {
	var lastErr error
	for port := payloadPortMin; port <= payloadPortMax; port++ {
		ln, err := net.Listen("tcp4", ":"+strconv.Itoa(port))
		if err == nil {
			return ln, port, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("no free payload port: %w", lastErr)
}

// SendPayload waits for the peer of l to connect to ln and streams src to it.
func (c Config) SendPayload(ln net.Listener, l *Link, src io.Reader) error {
	defer ln.Close()
	ln.(*net.TCPListener).SetDeadline(time.Now().Add(payloadWait))
	conn, err := ln.Accept()
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

// FetchPayload downloads size bytes that the peer of l offers on port.
func (c Config) FetchPayload(ctx context.Context, l *Link, port int, dst io.Writer, size int64) error {
	d := net.Dialer{Timeout: handshakeTimeout}
	conn, err := d.DialContext(ctx, "tcp4", net.JoinHostPort(l.RemoteIP().String(), strconv.Itoa(port)))
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
