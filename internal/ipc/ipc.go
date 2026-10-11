// Package ipc lets the command line and the GUI talk to the running daemon
// over HTTP on a local socket.
package ipc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charg3d/perekat/internal/core"
	"github.com/charg3d/perekat/internal/identity"
	"github.com/charg3d/perekat/internal/protocol"
)

// SocketPath returns where the daemon listens for local clients.
func SocketPath() (string, error) {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "perekat.sock"), nil
	}
	dir, err := identity.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perekat.sock"), nil
}

// Serve exposes c on the local socket until ctx is cancelled.
func Serve(ctx context.Context, c *core.Core) error {
	path, err := SocketPath()
	if err != nil {
		return err
	}
	if conn, err := net.DialTimeout("unix", path, time.Second); err == nil {
		conn.Close()
		return errors.New("perekat daemon is already running")
	}
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /self", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(c.Self())
	})
	mux.HandleFunc("GET /devices", func(w http.ResponseWriter, r *http.Request) {
		devices := c.Devices()
		if devices == nil {
			devices = []core.DeviceInfo{}
		}
		json.NewEncoder(w).Encode(devices)
	})
	mux.HandleFunc("POST /pair", func(w http.ResponseWriter, r *http.Request) {
		info, err := c.Pair(r.URL.Query().Get("device"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(info)
	})
	mux.HandleFunc("POST /unpair", func(w http.ResponseWriter, r *http.Request) {
		if err := c.Unpair(r.URL.Query().Get("device")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})

	mux.HandleFunc("POST /remote", func(w http.ResponseWriter, r *http.Request) {
		active, err := c.Remote(r.URL.Query().Get("device"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(active)
	})
	mux.HandleFunc("POST /remote/off", func(w http.ResponseWriter, r *http.Request) {
		c.RemoteOff()
	})

	// act wraps an action on one device that reports only success or failure.
	act := func(pattern string, do func(device string, r *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if err := do(r.URL.Query().Get("device"), r); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
			}
		})
	}
	act("POST /ping", func(device string, _ *http.Request) error { return c.Ping(device) })
	act("POST /ring", func(device string, _ *http.Request) error { return c.Ring(device) })
	act("POST /run", func(device string, r *http.Request) error {
		return c.Run(device, r.URL.Query().Get("command"))
	})
	act("POST /send", func(device string, r *http.Request) error {
		var paths []string
		if err := json.NewDecoder(r.Body).Decode(&paths); err != nil {
			return err
		}
		return c.SendFiles(device, paths)
	})
	mux.HandleFunc("POST /refresh", func(w http.ResponseWriter, r *http.Request) {
		c.Refresh()
	})
	mux.HandleFunc("GET /transfers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(c.Transfers())
	})
	mux.HandleFunc("GET /commands-path", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(c.CommandsPath())
	})

	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Client calls the running daemon.
type Client struct {
	http http.Client
}

func NewClient() (*Client, error) {
	path, err := SocketPath()
	if err != nil {
		return nil, err
	}
	return &Client{http: http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", path)
			},
		},
	}}, nil
}

func (c *Client) Self() (protocol.Identity, error) {
	var out protocol.Identity
	return out, c.call(http.MethodGet, "/self", &out)
}

func (c *Client) Devices() ([]core.DeviceInfo, error) {
	var out []core.DeviceInfo
	return out, c.call(http.MethodGet, "/devices", &out)
}

func (c *Client) Pair(device string) (core.DeviceInfo, error) {
	var out core.DeviceInfo
	return out, c.call(http.MethodPost, "/pair?device="+url.QueryEscape(device), &out)
}

func (c *Client) Unpair(device string) error {
	return c.call(http.MethodPost, "/unpair?device="+url.QueryEscape(device), nil)
}

// Remote toggles remote control of the device and reports whether it is now on.
func (c *Client) Remote(device string) (bool, error) {
	var active bool
	return active, c.call(http.MethodPost, "/remote?device="+url.QueryEscape(device), &active)
}

func (c *Client) RemoteOff() error {
	return c.call(http.MethodPost, "/remote/off", nil)
}

// Refresh makes the daemon announce itself again.
func (c *Client) Refresh() error {
	return c.call(http.MethodPost, "/refresh", nil)
}

func (c *Client) Ping(device string) error {
	return c.call(http.MethodPost, "/ping?device="+url.QueryEscape(device), nil)
}

func (c *Client) Ring(device string) error {
	return c.call(http.MethodPost, "/ring?device="+url.QueryEscape(device), nil)
}

// Run triggers one of the commands the device offers.
func (c *Client) Run(device, command string) error {
	return c.call(http.MethodPost, "/run?device="+url.QueryEscape(device)+"&command="+url.QueryEscape(command), nil)
}

// Send delivers files to the device and returns when the transfer is over.
func (c *Client) Send(device string, paths []string) error {
	return c.callBody(http.MethodPost, "/send?device="+url.QueryEscape(device), paths, nil)
}

func (c *Client) Transfers() ([]core.TransferInfo, error) {
	var out []core.TransferInfo
	return out, c.call(http.MethodGet, "/transfers", &out)
}

// CommandsPath returns the file that defines this device's commands.
func (c *Client) CommandsPath() (string, error) {
	var out string
	return out, c.call(http.MethodGet, "/commands-path", &out)
}

func (c *Client) call(method, path string, out any) error {
	return c.callBody(method, path, nil, out)
}

func (c *Client) callBody(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://perekat"+path, body)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("daemon is not running (start it with `perekat daemon`): %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return errors.New(strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
