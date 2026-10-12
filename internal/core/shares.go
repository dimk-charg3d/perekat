package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
)

// Network folders: ordinary folders on a computer that every paired device
// may read and change, whatever the file access setting says. Other
// computers show them inside their own file system, under NetDir.

// netDirName is the folder the network folders of other devices appear in.
const netDirName = "Сеть"

// Share is one network folder.
type Share struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// sharesRoot is where the network folders of this device are made.
func sharesRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Перекат")
}

// NetDir is where the network folders of other devices are mounted.
func NetDir() string {
	return filepath.Join(sharesRoot(), netDirName)
}

// Shares lists the network folders of this device.
func (c *Core) Shares() []Share {
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	return append([]Share{}, c.stored.Shares...)
}

func (c *Core) inShare(path string) bool {
	for _, share := range c.Shares() {
		if within(path, share.Path) {
			return true
		}
	}
	return false
}

// CreateShare makes a network folder on this device.
func (c *Core) CreateShare(name string) (Share, error) {
	if c.links.Self.DeviceType == "phone" {
		return Share{}, errors.New("network folders are kept on computers, not on phones")
	}
	if name == "" || name != safeName(name) || name == netDirName || len(name) > 64 {
		return Share{}, errors.New("not a valid name for a folder")
	}
	share := Share{Name: name, Path: filepath.Join(sharesRoot(), name)}
	for _, existing := range c.Shares() {
		if existing.Name == name {
			return Share{}, errors.New("a network folder with this name is already there")
		}
	}
	if err := os.MkdirAll(share.Path, 0o755); err != nil {
		return Share{}, plain(err)
	}
	c.settingsMu.Lock()
	c.stored.Shares = append(c.stored.Shares, share)
	c.settingsMu.Unlock()
	return share, c.sharesChanged()
}

// RemoveShare stops offering a network folder. The folder and what is in it
// stay on the disk.
func (c *Core) RemoveShare(path string) error {
	c.settingsMu.Lock()
	kept := c.stored.Shares[:0:0]
	for _, share := range c.stored.Shares {
		if share.Path != path {
			kept = append(kept, share)
		}
	}
	found := len(kept) != len(c.stored.Shares)
	c.stored.Shares = kept
	c.settingsMu.Unlock()
	if !found {
		return errors.New("no such network folder")
	}
	return c.sharesChanged()
}

func (c *Core) sharesChanged() error {
	c.settingsMu.Lock()
	data, _ := json.MarshalIndent(c.stored, "", "  ")
	c.settingsMu.Unlock()
	if err := os.WriteFile(c.settingsPath(), data, 0o600); err != nil {
		return err
	}
	for _, l := range c.pairedLinks() {
		c.sendShares(l)
	}
	return nil
}

func (c *Core) sendShares(l *link.Link) {
	if accepts(l.Peer, protocol.TypeShares) {
		send(l, protocol.TypeShares, map[string]any{"shares": c.Shares()})
	}
}

func (c *Core) handleShares(l *link.Link, p protocol.Packet) {
	var body struct {
		Shares []Share `json:"shares"`
	}
	if err := json.Unmarshal(p.Body, &body); err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.devices[l.Peer.DeviceID]; d != nil && d.link == l {
		d.shares = body.Shares
	}
}
