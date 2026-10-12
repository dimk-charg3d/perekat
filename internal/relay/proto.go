// Package relay connects devices that cannot reach each other directly.
//
// A relay server runs on a machine with a public address. Every device keeps
// one control connection to it, through which the server says who is online.
// To talk to another device, a device asks the server for a stream to it; the
// server pairs that request with a connection opened by the other device and
// copies bytes between the two. The devices then run their usual encrypted
// handshake inside the stream, so the server only ever sees ciphertext.
package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strconv"

	"github.com/charg3d/perekat/internal/protocol"
)

// DefaultPort is the port a relay server listens on.
const DefaultPort = 17160

const maxMessage = 256 * 1024

// Config is what a device needs to use a relay server.
type Config struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	// Key admits a device to the server; everyone who has it is in one network.
	Key string `json:"key"`
	// Fingerprint is the SHA-256 of the server's certificate, which devices
	// check instead of trusting a certificate authority.
	Fingerprint string `json:"fingerprint"`
}

func (c Config) addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// Code packs the configuration into one line that can be pasted on another device.
func (c Config) Code() string {
	q := url.Values{"key": {c.Key}, "fp": {c.Fingerprint}}
	return (&url.URL{Scheme: "perekat", Host: c.addr(), RawQuery: q.Encode()}).String()
}

// ParseCode reads a line produced by Code.
func ParseCode(code string) (Config, error) {
	u, err := url.Parse(code)
	if err != nil || u.Scheme != "perekat" {
		return Config{}, errors.New("this is not a perekat link")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return Config{}, errors.New("the link has no port")
	}
	c := Config{Host: u.Hostname(), Port: port, Key: u.Query().Get("key"), Fingerprint: u.Query().Get("fp")}
	if c.Host == "" || c.Key == "" || len(c.Fingerprint) != sha256.Size*2 {
		return Config{}, errors.New("the link is incomplete")
	}
	return c, nil
}

// Fingerprint returns the form of a certificate that Config carries.
func Fingerprint(certDER []byte) string {
	sum := sha256.Sum256(certDER)
	return hex.EncodeToString(sum[:])
}

// message is one line of the protocol between a device and the server.
type message struct {
	// Op is hello, peers, online, offline, ping, connect, incoming, accept,
	// probe, ok or error.
	Op       string              `json:"op"`
	Key      string              `json:"key,omitempty"`
	Identity *protocol.Identity  `json:"identity,omitempty"`
	Peers    []protocol.Identity `json:"peers,omitempty"`
	ID       string              `json:"id,omitempty"`
	From     string              `json:"from,omitempty"`
	To       string              `json:"to,omitempty"`
	// Label says what a stream is for, so the receiving device can route it.
	Label   string `json:"label,omitempty"`
	Session string `json:"session,omitempty"`
	Error   string `json:"error,omitempty"`
}

func writeMessage(w io.Writer, m message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}

// readMessage reads one line a byte at a time, so nothing after it is
// consumed: what follows may be a stream that belongs to someone else.
func readMessage(r io.Reader) (message, error) {
	var line []byte
	b := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, b); err != nil {
			return message{}, err
		}
		if b[0] == '\n' {
			break
		}
		if line = append(line, b[0]); len(line) > maxMessage {
			return message{}, errors.New("message too long")
		}
	}
	var m message
	if err := json.Unmarshal(line, &m); err != nil {
		return message{}, fmt.Errorf("bad message: %w", err)
	}
	return m, nil
}
