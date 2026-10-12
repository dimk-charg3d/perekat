// Package ipc lets the command line and the GUI talk to the running daemon
// over HTTP on a local socket.
package ipc

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charg3d/perekat/internal/core"
	"github.com/charg3d/perekat/internal/deploy"
	"github.com/charg3d/perekat/internal/identity"
	"github.com/charg3d/perekat/internal/input"
	"github.com/charg3d/perekat/internal/protocol"
	"github.com/charg3d/perekat/internal/version"
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

	srv := &http.Server{Handler: Handler(c)}
	go func() {
		<-ctx.Done()
		srv.Close()
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Handler returns the daemon's local API.
func Handler(c *core.Core) http.Handler {
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
	act("POST /text", func(device string, r *http.Request) error {
		var text string
		if err := json.NewDecoder(r.Body).Decode(&text); err != nil {
			return err
		}
		return c.SendText(device, text)
	})
	act("POST /input", func(device string, r *http.Request) error {
		var events []input.Event
		if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
			return err
		}
		return c.SendInput(device, events)
	})
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
	mux.HandleFunc("POST /fs", func(w http.ResponseWriter, r *http.Request) {
		var req core.FSRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res, err := c.FS(r.URL.Query().Get("device"), req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(res)
	})
	mux.HandleFunc("POST /fs/paste", func(w http.ResponseWriter, r *http.Request) {
		var p Paste
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := c.FSPaste(p.Src, p.Paths, p.Dst, p.Dir, p.Move); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})
	mux.HandleFunc("POST /fs/preview", func(w http.ResponseWriter, r *http.Request) {
		p, err := c.FSPreview(r.URL.Query().Get("device"), r.URL.Query().Get("path"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(p)
	})
	mux.HandleFunc("POST /fs/open", func(w http.ResponseWriter, r *http.Request) {
		if err := c.FSOpen(r.URL.Query().Get("device"), r.URL.Query().Get("path")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})
	// raw serves the content of a file, for previews and players; it honours
	// range requests, so a video can be wound.
	mux.HandleFunc("GET /fs/raw", func(w http.ResponseWriter, r *http.Request) {
		file, entry, err := c.FSReader(r.URL.Query().Get("device"), r.URL.Query().Get("path"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.ServeContent(w, r, entry.Name, time.Unix(entry.Modified, 0), file)
	})
	mux.HandleFunc("GET /fs/thumb", func(w http.ResponseWriter, r *http.Request) {
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		res, err := c.FS(r.URL.Query().Get("device"), core.FSRequest{Op: "thumb", Path: r.URL.Query().Get("path"), Length: size})
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=600")
		w.Write(res.Data)
	})
	// upload puts files picked in the interface into a folder of a device.
	mux.HandleFunc("POST /fs/upload", func(w http.ResponseWriter, r *http.Request) {
		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				return
			}
			if err == nil && part.FileName() != "" {
				err = c.FSUpload(r.URL.Query().Get("device"), r.URL.Query().Get("dir"), part.FileName(), 0, part)
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
	})
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(version.Version)
	})
	mux.HandleFunc("GET /ui", func(w http.ResponseWriter, r *http.Request) {
		w.Write(c.UIState())
	})
	mux.HandleFunc("PUT /ui", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err == nil {
			err = c.SaveUIState(data)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})
	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(c.Settings())
	})
	act("PUT /settings", func(_ string, r *http.Request) error {
		var s core.Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			return err
		}
		return c.SaveSettings(s)
	})
	mux.HandleFunc("GET /commands", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(c.Commands())
	})
	act("PUT /commands", func(_ string, r *http.Request) error {
		var commands map[string]string
		if err := json.NewDecoder(r.Body).Decode(&commands); err != nil {
			return err
		}
		return c.SaveCommands(commands)
	})
	mux.HandleFunc("GET /relay", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(c.RelayStatus())
	})
	act("POST /relay/setup", func(_ string, r *http.Request) error {
		var target deploy.Target
		if err := json.NewDecoder(r.Body).Decode(&target); err != nil {
			return err
		}
		return c.RelaySetup(r.Context(), target)
	})
	act("POST /relay/join", func(_ string, r *http.Request) error {
		var code string
		if err := json.NewDecoder(r.Body).Decode(&code); err != nil {
			return err
		}
		return c.RelayJoin(strings.TrimSpace(code))
	})
	act("POST /relay/leave", func(string, *http.Request) error { return c.RelayLeave() })
	mux.HandleFunc("POST /refresh", func(w http.ResponseWriter, r *http.Request) {
		c.Refresh()
	})
	mux.HandleFunc("GET /transfers", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(c.Transfers())
	})
	mux.HandleFunc("GET /commands-path", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(c.CommandsPath())
	})

	return mux
}

// ServeHTTP serves the API under /api/ and the given interface files on a
// local TCP listener, for a client that cannot use the socket. Every API call
// must carry the token: other programs on the machine can reach the port.
//
// extra, if given, handles the calls under /api/host/.
func ServeHTTP(ctx context.Context, ln net.Listener, c *core.Core, token string, assets fs.FS, extra http.Handler) error {
	daemon := Handler(c)
	api := http.StripPrefix("/api", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if extra != nil && strings.HasPrefix(r.URL.Path, "/host/") {
			extra.ServeHTTP(w, r)
			return
		}
		daemon.ServeHTTP(w, r)
	}))
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		given := r.Header.Get("X-Perekat-Token")
		// A picture or a video is asked for by the page itself, which cannot
		// add a header, so reading a file may carry the token in the address.
		if given == "" && r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/fs/") {
			given = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
			http.Error(w, "wrong token", http.StatusForbidden)
			return
		}
		api.ServeHTTP(w, r)
	})
	mux.HandleFunc("POST /api/upload", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Perekat-Token")), []byte(token)) != 1 {
			http.Error(w, "wrong token", http.StatusForbidden)
			return
		}
		if err := upload(c, r); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
	})
	mux.Handle("/", http.FileServerFS(assets))

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

// upload receives files picked in the interface and sends them to a device.
// A client that cannot name files by path uses it instead of /send.
func upload(c *core.Core, r *http.Request) error {
	reader, err := r.MultipartReader()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "perekat-upload-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	var paths []string
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.Base(part.FileName())
		if name == "." || name == string(filepath.Separator) {
			continue
		}
		// Each file gets a folder of its own, so equal names do not collide.
		sub, err := os.MkdirTemp(dir, "")
		if err != nil {
			return err
		}
		f, err := os.Create(filepath.Join(sub, name))
		if err != nil {
			return err
		}
		_, err = io.Copy(f, part)
		f.Close()
		if err != nil {
			return err
		}
		paths = append(paths, f.Name())
	}
	return c.SendFiles(r.URL.Query().Get("device"), paths)
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

// FS performs a file operation on another device.
func (c *Client) FS(device string, req core.FSRequest) (core.FSResult, error) {
	var out core.FSResult
	return out, c.callBody(http.MethodPost, "/fs?device="+url.QueryEscape(device), req, &out)
}

// Paste describes a copy or a move of files between two devices.
type Paste struct {
	Src   string   `json:"src"`
	Paths []string `json:"paths"`
	Dst   string   `json:"dst"`
	Dir   string   `json:"dir"`
	Move  bool     `json:"move"`
}

func (c *Client) FSPaste(p Paste) error {
	return c.callBody(http.MethodPost, "/fs/paste", p, nil)
}

// UIState returns what the window remembers between runs, as JSON.
func (c *Client) UIState() (string, error) {
	var out json.RawMessage
	err := c.call(http.MethodGet, "/ui", &out)
	return string(out), err
}

func (c *Client) SaveUIState(state string) error {
	return c.callBody(http.MethodPut, "/ui", json.RawMessage(state), nil)
}

func (c *Client) FSPreview(device, path string) (core.Preview, error) {
	var out core.Preview
	return out, c.call(http.MethodPost, "/fs/preview?device="+url.QueryEscape(device)+"&path="+url.QueryEscape(path), &out)
}

func (c *Client) FSOpen(device, path string) error {
	return c.call(http.MethodPost, "/fs/open?device="+url.QueryEscape(device)+"&path="+url.QueryEscape(path), nil)
}

// Files serves the daemon's calls that return file content, for a page that
// asks for them under /api/fs/.
func (c *Client) Files() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://perekat"+strings.TrimPrefix(r.URL.Path, "/api")+"?"+r.URL.RawQuery, nil)
		if err != nil || !strings.HasPrefix(r.URL.Path, "/api/fs/") || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		if rng := r.Header.Get("Range"); rng != "" {
			req.Header.Set("Range", rng)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for name, values := range resp.Header {
			w.Header()[name] = values
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	})
}

func (c *Client) Settings() (core.Settings, error) {
	var out core.Settings
	return out, c.call(http.MethodGet, "/settings", &out)
}

func (c *Client) SaveSettings(s core.Settings) error {
	return c.callBody(http.MethodPut, "/settings", s, nil)
}

// Commands returns the commands this device offers to others.
func (c *Client) Commands() (map[string]string, error) {
	var out map[string]string
	return out, c.call(http.MethodGet, "/commands", &out)
}

func (c *Client) SaveCommands(commands map[string]string) error {
	return c.callBody(http.MethodPut, "/commands", commands, nil)
}

func (c *Client) RelayStatus() (core.RelayStatus, error) {
	var out core.RelayStatus
	return out, c.call(http.MethodGet, "/relay", &out)
}

// RelaySetup installs a relay server on a remote machine and joins it.
func (c *Client) RelaySetup(target deploy.Target) error {
	return c.callBody(http.MethodPost, "/relay/setup", target, nil)
}

// RelayJoin joins a relay server by a code from another device.
func (c *Client) RelayJoin(code string) error {
	return c.callBody(http.MethodPost, "/relay/join", code, nil)
}

func (c *Client) RelayLeave() error {
	return c.call(http.MethodPost, "/relay/leave", nil)
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
