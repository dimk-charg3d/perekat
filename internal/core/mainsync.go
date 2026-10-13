package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
)

// One of the user's devices is the main one. The choice is made on any
// device and travels to the others; the latest choice wins. What is
// downloaded on the other devices is copied to the main one's downloads,
// and it is the main one's mouse and keyboard that can be shared.

type mainPacket struct {
	ID   string `json:"id"`
	Time int64  `json:"time"`
}

func (c *Core) mainID() string {
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	return c.stored.Main
}

// isMain reports whether the device with this id is the main one.
func (c *Core) isMain(id string) bool {
	return id != "" && c.mainID() == id
}

// mainForUI names the main device the way the window does: "self" for this one.
func (c *Core) mainForUI() string {
	if c.stored.Main == c.links.Self.DeviceID {
		return SelfDevice
	}
	return c.stored.Main
}

// chooseMain records the user's choice of the main device and tells the
// others. Choosing what is already chosen changes nothing.
func (c *Core) chooseMain(choice string) {
	id := choice
	if choice == SelfDevice {
		id = c.links.Self.DeviceID
	}
	if id == c.mainID() {
		return
	}
	c.setMain(mainPacket{ID: id, Time: time.Now().UnixMilli()})
	for _, l := range c.pairedLinks() {
		c.greetMain(l)
	}
}

// setMain adopts a choice of the main device if it is newer than the one
// known, and brings what depends on it in line.
func (c *Core) setMain(m mainPacket) bool {
	c.settingsMu.Lock()
	if m.Time <= c.stored.MainTime {
		c.settingsMu.Unlock()
		return false
	}
	c.stored.Main, c.stored.MainTime = m.ID, m.Time
	self := c.links.Self.DeviceID
	var dropped []string
	for peer, s := range c.stored.Sync {
		if !s.InputOn {
			continue
		}
		// A mouse is shared with the main device; a pair without it has none.
		if m.ID == self || m.ID == peer {
			s.Main = m.ID
		} else {
			s.InputOn = false
			dropped = append(dropped, peer)
		}
		c.stored.Sync[peer] = s
	}
	data, _ := json.MarshalIndent(c.stored, "", "  ")
	c.settingsMu.Unlock()
	os.WriteFile(c.settingsPath(), data, 0o600)
	for _, peer := range dropped {
		c.leaveRemote(peer)
		c.endGuest(peer)
	}
	c.log.Info("main device chosen", "id", m.ID)
	return true
}

func (c *Core) greetMain(l *link.Link) {
	c.settingsMu.Lock()
	m := mainPacket{ID: c.stored.Main, Time: c.stored.MainTime}
	c.settingsMu.Unlock()
	if m.Time != 0 && accepts(l.Peer, protocol.TypeMain) {
		send(l, protocol.TypeMain, m)
	}
}

func (c *Core) handleMain(l *link.Link, p protocol.Packet) {
	var m mainPacket
	if err := json.Unmarshal(p.Body, &m); err != nil || m.Time > time.Now().Add(time.Hour).UnixMilli() {
		return
	}
	if c.setMain(m) {
		// Pass the news on to the devices that have not heard it.
		for _, other := range c.pairedLinks() {
			if other != l {
				c.greetMain(other)
			}
		}
	}
}

// Mirroring of downloads.

// mirrorState is what a device remembers about its downloads folder: which
// files it has dealt with, so that each is copied to the main device once.
type mirrorState struct {
	sync.Mutex
	loaded bool
	// Dir is the folder the list belongs to; another folder starts afresh.
	Dir  string          `json:"dir"`
	Seen map[string]bool `json:"seen"`
	// sizes holds what a new file measured last time: a file still being
	// written keeps growing and is left alone until it stops.
	sizes map[string]int64
}

func (c *Core) mirrorPath() string {
	return filepath.Join(c.dir, "mirror.json")
}

func mirrorKey(name string, info os.FileInfo) string {
	return fmt.Sprintf("%s|%d|%d", name, info.Size(), info.ModTime().Unix())
}

// unfinished tells the files a browser is still writing, and hidden ones.
func unfinished(name string) bool {
	lower := strings.ToLower(name)
	for _, ext := range []string{".part", ".crdownload", ".tmp", ".download", ".partial", ".opdownload"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return strings.HasPrefix(name, ".")
}

// mirrorSkip marks a file in the downloads folder as dealt with: it came
// through perekat itself and must not travel on.
func (c *Core) mirrorSkip(path string) {
	info, err := os.Stat(path)
	if err != nil || filepath.Dir(path) != c.downloadDir() {
		return
	}
	m := &c.mirror
	m.Lock()
	defer m.Unlock()
	c.loadMirror()
	m.Seen[mirrorKey(filepath.Base(path), info)] = true
	c.saveMirror()
}

// loadMirror and saveMirror are called with the state locked.
func (c *Core) loadMirror() {
	m := &c.mirror
	if m.loaded {
		return
	}
	m.loaded = true
	if data, err := os.ReadFile(c.mirrorPath()); err == nil {
		json.Unmarshal(data, m)
	}
	if m.Seen == nil {
		m.Seen = map[string]bool{}
	}
	m.sizes = map[string]int64{}
}

func (c *Core) saveMirror() {
	data, _ := json.Marshal(&c.mirror)
	os.WriteFile(c.mirrorPath(), data, 0o600)
}

// RunMirror copies what appears in this device's downloads folder to the
// main device, until stop is closed. Files that were there before are left
// alone; files that appear while the main device is away go once it is back.
func (c *Core) RunMirror(stop <-chan struct{}) {
	tick := time.NewTicker(4 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		main := c.mainID()
		c.settingsMu.Lock()
		off := c.stored.MirrorOff
		c.settingsMu.Unlock()
		if main == "" || main == c.links.Self.DeviceID || off {
			continue
		}
		c.mirrorOnce(main)
	}
}

func (c *Core) mirrorOnce(main string) {
	dir := c.downloadDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	m := &c.mirror
	m.Lock()
	c.loadMirror()
	fresh := m.Dir != dir
	if fresh {
		// A folder seen for the first time: what is in it stays where it is.
		m.Dir, m.Seen, m.sizes = dir, map[string]bool{}, map[string]int64{}
	}
	var ready []string
	keys := map[string]string{}
	current := map[string]bool{}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() || unfinished(e.Name()) {
			continue
		}
		key := mirrorKey(e.Name(), info)
		current[key] = true
		if fresh {
			m.Seen[key] = true
			continue
		}
		if m.Seen[key] {
			continue
		}
		// Sent only once it has stopped changing between two looks.
		if last, ok := m.sizes[e.Name()]; !ok || last != info.Size() || time.Since(info.ModTime()) < 3*time.Second {
			m.sizes[e.Name()] = info.Size()
			continue
		}
		path := filepath.Join(dir, e.Name())
		ready = append(ready, path)
		keys[path] = key
	}
	// Forget the files that are gone, so the list does not grow for ever.
	for key := range m.Seen {
		if !current[key] {
			delete(m.Seen, key)
		}
	}
	if fresh {
		c.saveMirror()
	}
	m.Unlock()
	if len(ready) == 0 {
		return
	}
	l, err := c.pairedLink(main)
	if err != nil || !accepts(l.Peer, protocol.TypeShare) {
		return
	}
	for _, path := range ready {
		if err := c.sendFileAs(l, path, true); err != nil {
			c.log.Warn("download was not copied to the main device", "file", filepath.Base(path), "err", err)
			return
		}
		m.Lock()
		m.Seen[keys[path]] = true
		delete(m.sizes, filepath.Base(path))
		c.saveMirror()
		m.Unlock()
		c.log.Info("download copied to the main device", "file", filepath.Base(path))
	}
}
