package relay

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/protocol"
)

const (
	helloTimeout   = 10 * time.Second
	sessionTimeout = 15 * time.Second
	// controlTimeout is how long a device may stay silent; devices ping more often.
	controlTimeout = 90 * time.Second
	pingInterval   = 25 * time.Second
)

// Server is a relay server.
type Server struct {
	Key  string
	Cert tls.Certificate
	Log  *slog.Logger

	mu       sync.Mutex
	controls map[string]*control
	sessions map[string]chan net.Conn
}

type control struct {
	identity protocol.Identity
	conn     net.Conn
	wmu      sync.Mutex
}

func (c *control) send(m message) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(helloTimeout))
	return writeMessage(c.conn, m)
}

// Serve handles connections until the listener is closed.
func (s *Server) Serve(ln net.Listener) error {
	s.controls = map[string]*control{}
	s.sessions = map[string]chan net.Conn{}
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(raw net.Conn) {
	conn := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{s.Cert}, MinVersion: tls.VersionTLS12})
	conn.SetDeadline(time.Now().Add(helloTimeout))
	m, err := readMessage(conn)
	if err != nil || subtle.ConstantTimeCompare([]byte(m.Key), []byte(s.Key)) != 1 {
		conn.Close()
		return
	}
	conn.SetDeadline(time.Time{})
	switch m.Op {
	case "hello":
		s.control(conn, m)
	case "connect":
		s.connect(conn, m)
	case "accept":
		s.accept(conn, m)
	case "probe":
		writeMessage(conn, message{Op: "ok"})
		conn.Close()
	default:
		conn.Close()
	}
}

// control serves a device's control connection for as long as it lasts.
func (s *Server) control(conn net.Conn, m message) {
	defer conn.Close()
	if m.Identity == nil || !protocol.ValidDeviceID(m.Identity.DeviceID) {
		return
	}
	c := &control{identity: *m.Identity, conn: conn}
	id := c.identity.DeviceID

	s.mu.Lock()
	if old := s.controls[id]; old != nil {
		old.conn.Close()
	}
	s.controls[id] = c
	var peers []protocol.Identity
	var others []*control
	for otherID, other := range s.controls {
		if otherID != id {
			peers = append(peers, other.identity)
			others = append(others, other)
		}
	}
	s.mu.Unlock()
	s.Log.Info("device online", "name", c.identity.DeviceName, "from", conn.RemoteAddr())

	if c.send(message{Op: "peers", Peers: peers}) != nil {
		return
	}
	for _, other := range others {
		other.send(message{Op: "online", Identity: &c.identity})
	}

	for {
		conn.SetReadDeadline(time.Now().Add(controlTimeout))
		if _, err := readMessage(conn); err != nil {
			break
		}
	}

	s.mu.Lock()
	current := s.controls[id] == c
	if current {
		delete(s.controls, id)
	}
	others = others[:0]
	for _, other := range s.controls {
		others = append(others, other)
	}
	s.mu.Unlock()
	if current {
		s.Log.Info("device offline", "name", c.identity.DeviceName)
		for _, other := range others {
			other.send(message{Op: "offline", ID: id})
		}
	}
}

// connect asks the target device to open its end of a stream and then joins
// the two connections.
func (s *Server) connect(conn net.Conn, m message) {
	s.mu.Lock()
	target := s.controls[m.To]
	s.mu.Unlock()
	if target == nil {
		writeMessage(conn, message{Op: "error", Error: "device is not online"})
		conn.Close()
		return
	}

	var raw [16]byte
	rand.Read(raw[:])
	session := hex.EncodeToString(raw[:])
	arrived := make(chan net.Conn, 1)
	s.mu.Lock()
	s.sessions[session] = arrived
	s.mu.Unlock()

	var peer net.Conn
	if target.send(message{Op: "incoming", From: m.From, Label: m.Label, Session: session}) == nil {
		select {
		case peer = <-arrived:
		case <-time.After(sessionTimeout):
		}
	}
	s.mu.Lock()
	delete(s.sessions, session)
	s.mu.Unlock()
	if peer == nil {
		// The device may have arrived just as the wait ended.
		select {
		case late := <-arrived:
			late.Close()
		default:
		}
		writeMessage(conn, message{Op: "error", Error: "device did not answer"})
		conn.Close()
		return
	}

	if writeMessage(conn, message{Op: "ok"}) != nil || writeMessage(peer, message{Op: "ok"}) != nil {
		conn.Close()
		peer.Close()
		return
	}
	splice(conn, peer)
}

// accept hands the connection to the connect call that is waiting for it.
func (s *Server) accept(conn net.Conn, m message) {
	s.mu.Lock()
	arrived := s.sessions[m.Session]
	delete(s.sessions, m.Session)
	s.mu.Unlock()
	if arrived == nil {
		writeMessage(conn, message{Op: "error", Error: "no such session"})
		conn.Close()
		return
	}
	arrived <- conn
}

// splice copies bytes both ways until either side stops.
func splice(a, b net.Conn) {
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		io.Copy(dst, src)
		done <- struct{}{}
	}
	go pipe(a, b)
	go pipe(b, a)
	<-done
	a.Close()
	b.Close()
	<-done
}
