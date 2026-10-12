package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/clipboard"
	"github.com/charg3d/perekat/internal/protocol"
)

// The clipboard history: what was copied lately, on this device or on any
// other, kept on disk so that it is still there after a restart. A password
// manager's entries never get here; they are not synced in the first place.

const (
	clipKept = 100
	// clipTooLong is where a copied text is too large to be worth keeping.
	clipTooLong = 256 * 1024
)

// ClipEntry is one copied text.
type ClipEntry struct {
	ID   int64  `json:"id"`
	Text string `json:"text"`
	// From is the name of the device it was copied on.
	From   string `json:"from"`
	Time   int64  `json:"time"`
	Pinned bool   `json:"pinned,omitempty"`
}

type clipHistory struct {
	sync.Mutex
	loaded  bool
	entries []ClipEntry // newest first
}

func (c *Core) clipPath() string {
	return filepath.Join(c.dir, "clipboard.json")
}

// withClips runs fn on the history and saves it when fn says it changed.
func (c *Core) withClips(fn func(h *clipHistory) bool) {
	h := &c.clips
	h.Lock()
	defer h.Unlock()
	if !h.loaded {
		h.loaded = true
		if data, err := os.ReadFile(c.clipPath()); err == nil {
			json.Unmarshal(data, &h.entries)
		}
	}
	if fn(h) {
		data, _ := json.Marshal(h.entries)
		os.WriteFile(c.clipPath(), data, 0o600)
	}
}

// rememberClip puts a copied text at the top of the history.
func (c *Core) rememberClip(text, from string) {
	if text == "" || len(text) > clipTooLong {
		return
	}
	c.withClips(func(h *clipHistory) bool {
		entry := ClipEntry{ID: time.Now().UnixMicro(), Text: text, From: from, Time: time.Now().UnixMilli()}
		kept := h.entries[:0:0]
		for _, e := range h.entries {
			if e.Text == text {
				// Copied again: it moves up and keeps its pin.
				entry.Pinned = e.Pinned
				continue
			}
			kept = append(kept, e)
		}
		h.entries = append([]ClipEntry{entry}, kept...)
		// The oldest go first, the pinned ones stay.
		for i := len(h.entries) - 1; i >= 0 && len(h.entries) > clipKept; i-- {
			if !h.entries[i].Pinned {
				h.entries = append(h.entries[:i], h.entries[i+1:]...)
			}
		}
		return true
	})
}

// Clips returns the clipboard history, newest first.
func (c *Core) Clips() []ClipEntry {
	out := []ClipEntry{}
	c.withClips(func(h *clipHistory) bool {
		out = append(out, h.entries...)
		return false
	})
	return out
}

// UseClip copies an entry of the history again, here and on the paired devices.
func (c *Core) UseClip(id int64) error {
	text := ""
	c.withClips(func(h *clipHistory) bool {
		for _, e := range h.entries {
			if e.ID == id {
				text = e.Text
			}
		}
		return false
	})
	if text == "" {
		return errors.New("no such entry")
	}
	c.clipMu.Lock()
	c.clip, c.clipTime = text, time.Now().UnixMilli()
	c.clipMu.Unlock()
	if err := clipboard.Set(text); err != nil {
		return err
	}
	c.rememberClip(text, c.links.Self.DeviceName)
	for _, l := range c.pairedLinks() {
		if accepts(l.Peer, protocol.TypeClipboard) && !c.peerSync(l.Peer.DeviceID).ClipboardOff {
			send(l, protocol.TypeClipboard, clipboardBody{Content: text})
		}
	}
	return nil
}

// PinClip keeps an entry from being pushed out, or lets it go again.
func (c *Core) PinClip(id int64) {
	c.withClips(func(h *clipHistory) bool {
		for i := range h.entries {
			if h.entries[i].ID == id {
				h.entries[i].Pinned = !h.entries[i].Pinned
				return true
			}
		}
		return false
	})
}

// DropClips removes one entry, or with id 0 everything that is not pinned.
func (c *Core) DropClips(id int64) {
	c.withClips(func(h *clipHistory) bool {
		kept := h.entries[:0:0]
		for _, e := range h.entries {
			if (id == 0 && e.Pinned) || (id != 0 && e.ID != id) {
				kept = append(kept, e)
			}
		}
		h.entries = kept
		return true
	})
}
