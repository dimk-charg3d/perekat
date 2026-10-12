package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
)

// keptTransfers is how many finished transfers stay visible.
const keptTransfers = 20

type transfer struct {
	TransferInfo
	done atomic.Int64
}

// TransferInfo describes one file being sent or received.
type TransferInfo struct {
	ID       int    `json:"id"`
	DeviceID string `json:"deviceId"`
	Device   string `json:"device"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Done     int64  `json:"done"`
	Incoming bool   `json:"incoming"`
	// State is "active", "done" or "failed".
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// Path is where a received file was saved.
	Path string `json:"path,omitempty"`
}

func (c *Core) startTransfer(l *link.Link, name string, size int64, incoming bool) *transfer {
	return c.startTransferFor(l.Peer.DeviceID, l.Peer.DeviceName, name, size, incoming)
}

func (c *Core) startTransferFor(deviceID, device, name string, size int64, incoming bool) *transfer {
	c.transferMu.Lock()
	defer c.transferMu.Unlock()
	c.transferID++
	t := &transfer{TransferInfo: TransferInfo{
		ID: c.transferID, DeviceID: deviceID, Device: device, Name: name, Size: size, Incoming: incoming, State: "active",
	}}
	c.transfers = append(c.transfers, t)
	if extra := len(c.transfers) - keptTransfers; extra > 0 {
		c.transfers = c.transfers[extra:]
	}
	return t
}

func (c *Core) finishTransfer(t *transfer, path string, err error) {
	c.transferMu.Lock()
	defer c.transferMu.Unlock()
	t.State, t.Path = "done", path
	if err != nil {
		t.State, t.Error = "failed", err.Error()
	}
}

// Transfers lists recent file transfers, newest first.
func (c *Core) Transfers() []TransferInfo {
	c.transferMu.Lock()
	defer c.transferMu.Unlock()
	out := make([]TransferInfo, 0, len(c.transfers))
	for i := len(c.transfers) - 1; i >= 0; i-- {
		info := c.transfers[i].TransferInfo
		info.Done = c.transfers[i].done.Load()
		out = append(out, info)
	}
	return out
}

// counter adds the bytes passing through it to a transfer's progress.
type counter struct {
	t *transfer
	r io.Reader
	w io.Writer
}

func (c counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.t.done.Add(int64(n))
	return n, err
}

func (c counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.t.done.Add(int64(n))
	return n, err
}

func (c *Core) handleShare(l *link.Link, p protocol.Packet) {
	var body struct {
		Filename string `json:"filename"`
		Text     string `json:"text"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal(p.Body, &body); err != nil {
		return
	}
	switch {
	case body.Filename != "" && p.PayloadTransferInfo != nil:
		go c.receiveFile(l, body.Filename, p.PayloadSize, p.PayloadTransferInfo.Port)
	case body.Text != "":
		c.setClipboard(body.Text)
		desktop.Notify(l.Peer.DeviceName, "Текст скопирован в буфер обмена")
	case body.URL != "":
		// Only web links are opened; anything else could launch a local program.
		if u, err := url.Parse(body.URL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
			desktop.Open(body.URL)
		}
	}
}

func (c *Core) receiveFile(l *link.Link, name string, size int64, port int) {
	t := c.startTransfer(l, safeName(name), size, true)
	path, err := c.download(l, t, port)
	c.finishTransfer(t, path, err)
	if err != nil {
		c.log.Warn("file was not received", "device", l.Peer.DeviceName, "file", t.Name, "err", err)
		return
	}
	c.log.Info("file received", "device", l.Peer.DeviceName, "path", path)
	desktop.Notify("Файл от "+l.Peer.DeviceName, path)
}

func (c *Core) download(l *link.Link, t *transfer, port int) (string, error) {
	dir := c.downloadDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Download next to the destination under a temporary name, so a broken
	// transfer never leaves a file that looks complete.
	part, err := os.CreateTemp(dir, ".perekat-*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(part.Name())
	err = c.links.FetchPayload(context.Background(), l, port, counter{t: t, w: part}, t.Size)
	if cerr := part.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	return claim(part.Name(), dir, t.Name)
}

// safeName reduces a name supplied by another device to a plain file name
// that cannot point outside the downloads folder.
func safeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(filepath.Clean("/" + name))
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimLeft(name, ". ")
	if name == "" || name == "/" {
		name = "file"
	}
	return name
}

// claim moves a finished download to its name in dir, adding a number when a
// file with that name already exists.
func claim(part, dir, name string) (string, error) {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		candidate := filepath.Join(dir, name)
		if i > 0 {
			candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		}
		// Creating the name exclusively reserves it against concurrent transfers.
		f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		f.Close()
		if err := os.Rename(part, candidate); err != nil {
			os.Remove(candidate)
			return "", err
		}
		return candidate, nil
	}
	return "", errors.New("too many files with the same name")
}

// SendFiles sends files to a device one after another and returns when all
// are delivered or one fails.
func (c *Core) SendFiles(query string, paths []string) error {
	l, err := c.pairedLink(query)
	if err != nil {
		return err
	}
	if !accepts(l.Peer, protocol.TypeShare) {
		return errors.New("device does not accept files")
	}
	if len(paths) == 0 {
		return errors.New("no files to send")
	}
	for _, path := range paths {
		if err := c.sendFile(l, path); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

func (c *Core) sendFile(l *link.Link, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a regular file")
	}

	id, wait, cancel, err := l.Payload.Offer()
	if err != nil {
		return err
	}
	p, err := protocol.New(protocol.TypeShare, map[string]any{
		"filename":         info.Name(),
		"lastModified":     info.ModTime().UnixMilli(),
		"numberOfFiles":    1,
		"totalPayloadSize": info.Size(),
	})
	if err != nil {
		cancel()
		return err
	}
	p.PayloadSize = info.Size()
	p.PayloadTransferInfo = &protocol.PayloadTransfer{Port: id}
	if err := l.Send(p); err != nil {
		cancel()
		return err
	}

	t := c.startTransfer(l, info.Name(), info.Size(), false)
	started := time.Now()
	err = c.links.SendPayload(l, wait, counter{t: t, r: f})
	c.finishTransfer(t, "", err)
	if err == nil {
		c.log.Info("file sent", "device", l.Peer.DeviceName, "file", info.Name(), "took", time.Since(started).Round(time.Millisecond))
	}
	return err
}

// SendText hands a piece of text to a device: a web link opens there, any
// other text lands in its clipboard.
func (c *Core) SendText(query, text string) error {
	l, err := c.pairedLink(query)
	if err != nil {
		return err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("nothing to send")
	}
	if u, err := url.Parse(text); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && !strings.ContainsAny(text, " \n\t") {
		return send(l, protocol.TypeShare, map[string]string{"url": text})
	}
	return send(l, protocol.TypeShare, map[string]string{"text": text})
}
