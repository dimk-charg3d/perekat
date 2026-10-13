package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/identity"
	"github.com/charg3d/perekat/internal/link"
)

// Settings are the user's preferences.
type Settings struct {
	// Name is how this device appears to others.
	Name string `json:"name"`
	// DownloadDir is where received files are saved.
	DownloadDir string `json:"downloadDir"`
	// Clipboard turns clipboard sync on and off.
	Clipboard bool `json:"clipboard"`
	// Language of the window: "ru" or "en".
	Language string `json:"language"`
	// KDEConnect allows links with devices that run KDE Connect rather than
	// perekat. It is off unless asked for: perekat talks to perekat.
	KDEConnect bool `json:"kdeConnect"`
	// FileAccess lets paired devices browse and change the files here.
	FileAccess bool `json:"fileAccess"`
	// Autostart starts perekat in the background when the user logs in.
	// CanAutostart is false where the system gives no say in it.
	Autostart    bool `json:"autostart"`
	CanAutostart bool `json:"canAutostart"`
	// Main is the main device: "self", the id of a paired device, or empty
	// when none is chosen. What is downloaded on the other devices is copied
	// to it, and its mouse and keyboard are the ones that can be shared.
	Main string `json:"main"`
	// Mirror copies what lands in this device's downloads to the main device.
	Mirror bool `json:"mirror"`
	// Colors maps a device id to the colour the window shows it in.
	Colors map[string]string `json:"colors"`
}

// storedSettings is what is kept on disk. The name lives with the identity,
// and the zero value must mean the defaults.
type storedSettings struct {
	DownloadDir   string            `json:"downloadDir,omitempty"`
	ClipboardOff  bool              `json:"clipboardOff,omitempty"`
	Language      string            `json:"language,omitempty"`
	KDEConnectOn  bool              `json:"kdeConnectOn,omitempty"`
	FileAccessOff bool              `json:"fileAccessOff,omitempty"`
	Colors        map[string]string `json:"colors,omitempty"`
	Shares        []Share           `json:"shares,omitempty"`
	// Main is the id of the main device and MainTime when it was chosen,
	// so that the latest choice wins on every device.
	Main      string `json:"main,omitempty"`
	MainTime  int64  `json:"mainTime,omitempty"`
	MirrorOff bool   `json:"mirrorOff,omitempty"`
	// Sync is what is shared with each paired device, by device id.
	Sync map[string]peerSync `json:"sync,omitempty"`
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (c *Core) settingsPath() string {
	return filepath.Join(c.dir, "settings.json")
}

func (c *Core) loadSettings() {
	if data, err := os.ReadFile(c.settingsPath()); err == nil {
		json.Unmarshal(data, &c.stored)
	}
}

// Settings returns the current preferences with defaults filled in.
func (c *Core) Settings() Settings {
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	s := Settings{
		Name:         c.links.Self.DeviceName,
		DownloadDir:  c.stored.DownloadDir,
		Clipboard:    !c.stored.ClipboardOff,
		Language:     c.stored.Language,
		KDEConnect:   c.stored.KDEConnectOn,
		FileAccess:   !c.stored.FileAccessOff,
		Main:         c.mainForUI(),
		Mirror:       !c.stored.MirrorOff,
		Autostart:    desktop.Autostart(),
		CanAutostart: desktop.CanAutostart,
		Colors:       map[string]string{},
	}
	for id, color := range c.stored.Colors {
		s.Colors[id] = color
	}
	if s.DownloadDir == "" {
		s.DownloadDir = desktop.DownloadsDir()
	}
	if s.Language == "" {
		s.Language = "ru"
	}
	return s
}

// SaveSettings applies new preferences. A new device name takes effect by
// restarting the daemon, since every connection has announced the old one.
func (c *Core) SaveSettings(s Settings) error {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" || len(s.Name) > 64 || strings.ContainsAny(s.Name, "\n\r\t") {
		return errors.New("the device name must be 1 to 64 characters on one line")
	}
	if info, err := os.Stat(s.DownloadDir); err != nil || !info.IsDir() {
		return errors.New("the folder for received files does not exist")
	}
	if s.Language != "ru" && s.Language != "en" {
		return errors.New("unknown language")
	}
	for _, color := range s.Colors {
		if !colorPattern.MatchString(color) {
			return errors.New("a colour must look like #1a2b3c")
		}
	}

	c.settingsMu.Lock()
	c.stored = storedSettings{
		DownloadDir:   s.DownloadDir,
		ClipboardOff:  !s.Clipboard,
		Language:      s.Language,
		KDEConnectOn:  s.KDEConnect,
		FileAccessOff: !s.FileAccess,
		Colors:        s.Colors,
		Shares:        c.stored.Shares,
		Sync:          c.stored.Sync,
		Main:          c.stored.Main,
		MainTime:      c.stored.MainTime,
		MirrorOff:     !s.Mirror,
	}
	if s.DownloadDir == desktop.DownloadsDir() {
		c.stored.DownloadDir = ""
	}
	data, _ := json.MarshalIndent(c.stored, "", "  ")
	renamed := s.Name != c.links.Self.DeviceName
	c.settingsMu.Unlock()
	if err := os.WriteFile(c.settingsPath(), data, 0o600); err != nil {
		return err
	}
	c.chooseMain(s.Main)
	if desktop.CanAutostart && s.Autostart != desktop.Autostart() {
		if err := desktop.SetAutostart(s.Autostart); err != nil {
			return err
		}
	}
	if !s.KDEConnect {
		c.dropForeignLinks()
	} else {
		c.Refresh()
	}
	if !renamed {
		return nil
	}

	self, err := identity.LoadOrCreate(c.dir)
	if err != nil {
		return err
	}
	self.Name = s.Name
	if err := identity.Save(c.dir, self); err != nil {
		return err
	}
	if c.OnRestart != nil {
		// Let the answer to this request get out first.
		time.AfterFunc(200*time.Millisecond, c.OnRestart)
	}
	return nil
}

// foreignAllowed reports whether devices that do not run perekat are welcome.
func (c *Core) foreignAllowed() bool {
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	return c.stored.KDEConnectOn
}

// dropForeignLinks disconnects devices that do not run perekat.
func (c *Core) dropForeignLinks() {
	c.mu.Lock()
	var foreign []*link.Link
	for _, d := range c.devices {
		if d.link != nil && !speaksPerekat(d.link.Peer) {
			foreign = append(foreign, d.link)
		}
	}
	c.mu.Unlock()
	for _, l := range foreign {
		l.Close()
	}
}

func (c *Core) fileAccess() bool {
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	return !c.stored.FileAccessOff
}

func (c *Core) clipboardEnabled() bool {
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	return !c.stored.ClipboardOff
}

func (c *Core) downloadDir() string {
	c.settingsMu.Lock()
	dir := c.stored.DownloadDir
	c.settingsMu.Unlock()
	if dir == "" {
		dir = desktop.DownloadsDir()
	}
	return dir
}

// Commands returns the commands this device offers: name to shell command line.
func (c *Core) Commands() map[string]string {
	commands := c.localCommands()
	if commands == nil {
		commands = map[string]string{}
	}
	return commands
}

// SaveCommands replaces the commands this device offers and tells paired devices.
func (c *Core) SaveCommands(commands map[string]string) error {
	clean := map[string]string{}
	for name, command := range commands {
		name, command = strings.TrimSpace(name), strings.TrimSpace(command)
		if name == "" || command == "" {
			return errors.New("every command needs a name and a command line")
		}
		clean[name] = command
	}
	data, _ := json.MarshalIndent(clean, "", "  ")
	if err := os.WriteFile(c.CommandsPath(), data, 0o600); err != nil {
		return err
	}
	for _, l := range c.pairedLinks() {
		c.sendCommandList(l)
	}
	return nil
}

// Close drops every connection and gives back what the daemon holds.
func (c *Core) Close() {
	c.mu.Lock()
	c.stopRemote()
	var links []interface{ Close() error }
	for _, d := range c.devices {
		if d.link != nil {
			links = append(links, d.link)
		}
	}
	c.mu.Unlock()
	for _, l := range links {
		l.Close()
	}
	c.injectMu.Lock()
	if c.injector != nil {
		c.injector.Close()
		c.injector = nil
	}
	c.injectMu.Unlock()
}
