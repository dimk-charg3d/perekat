package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/clipboard"
	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/input"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/phonehost"
	"github.com/charg3d/perekat/internal/protocol"
)

// Clipboard: every local change goes to all paired devices, and their changes
// are applied here.

const maxClipboard = 1 << 20

type clipboardBody struct {
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// RunClipboard watches the local clipboard until ctx is cancelled.
func (c *Core) RunClipboard(ctx context.Context) {
	start := time.Now()
	clipboard.OnImage = func(png []byte) {
		c.localImage(png, time.Since(start) < 500*time.Millisecond)
	}
	err := clipboard.Watch(ctx, func(text string) {
		// What is already in the clipboard at startup is not a fresh copy.
		c.localClipboard(text, time.Since(start) < 500*time.Millisecond)
	})
	if err != nil && ctx.Err() == nil {
		c.log.Warn("clipboard sync is off", "err", err)
	}
}

func (c *Core) localClipboard(text string, initial bool) {
	if text == "" || len(text) > maxClipboard || !c.clipboardEnabled() {
		return
	}
	c.clipMu.Lock()
	if text == c.clip {
		c.clipMu.Unlock()
		return
	}
	c.clip, c.clipTime = text, 0
	if !initial {
		c.clipTime = time.Now().UnixMilli()
	}
	c.clipMu.Unlock()
	if initial {
		return
	}
	c.rememberClip(text, c.links.Self.DeviceName)
	for _, l := range c.pairedLinks() {
		if accepts(l.Peer, protocol.TypeClipboard) && !c.peerSync(l.Peer.DeviceID).ClipboardOff {
			send(l, protocol.TypeClipboard, clipboardBody{Content: text})
		}
	}
}

// clipImage remembers the last picture that went through the clipboard, so
// that one set from another device is not sent straight back.
var clipImage struct {
	sync.Mutex
	sum [32]byte
}

func seenImage(png []byte) bool {
	sum := sha256.Sum256(png)
	clipImage.Lock()
	defer clipImage.Unlock()
	if sum == clipImage.sum {
		return true
	}
	clipImage.sum = sum
	return false
}

func (c *Core) localImage(png []byte, initial bool) {
	if !c.clipboardEnabled() || seenImage(png) || initial {
		return
	}
	for _, l := range c.pairedLinks() {
		if accepts(l.Peer, protocol.TypeClipImage) && !c.peerSync(l.Peer.DeviceID).ClipboardOff {
			send(l, protocol.TypeClipImage, map[string]any{"png": png})
		}
	}
}

func (c *Core) handleClipImage(l *link.Link, p protocol.Packet) {
	var body struct {
		PNG []byte `json:"png"`
	}
	if err := json.Unmarshal(p.Body, &body); err != nil || len(body.PNG) == 0 || !c.clipboardEnabled() || c.peerSync(l.Peer.DeviceID).ClipboardOff || seenImage(body.PNG) {
		return
	}
	if err := clipboard.SetImage(body.PNG); err == nil {
		return
	}
	// A device that cannot hold a picture in its clipboard keeps it as a file.
	dir := c.downloadDir()
	os.MkdirAll(dir, 0o755)
	path := uniquePath(filepath.Join(dir, "Буфер "+time.Now().Format("2006-01-02 15.04.05")+".png"))
	if os.WriteFile(path, body.PNG, 0o644) == nil {
		desktop.Notify("Картинка от "+l.Peer.DeviceName, path)
	}
}

func (c *Core) handleClipboard(l *link.Link, p protocol.Packet) {
	var body clipboardBody
	if err := json.Unmarshal(p.Body, &body); err != nil || !c.clipboardEnabled() || c.peerSync(l.Peer.DeviceID).ClipboardOff {
		return
	}
	// On connect each side offers what it has; the more recent copy wins.
	if p.Type == protocol.TypeClipboardConnect {
		c.clipMu.Lock()
		stale := body.Timestamp == 0 || body.Timestamp <= c.clipTime
		c.clipMu.Unlock()
		if stale {
			return
		}
	}
	c.setClipboard(body.Content)
	c.rememberClip(body.Content, l.Peer.DeviceName)
}

// setClipboard applies text received from another device. Recording it first
// keeps the watcher from sending it straight back.
func (c *Core) setClipboard(text string) {
	if text == "" || len(text) > maxClipboard {
		return
	}
	c.clipMu.Lock()
	if text == c.clip {
		c.clipMu.Unlock()
		return
	}
	c.clip, c.clipTime = text, time.Now().UnixMilli()
	c.clipMu.Unlock()
	if err := clipboard.Set(text); err != nil {
		c.log.Warn("cannot set the clipboard", "err", err)
	}
}

func (c *Core) greetClipboard(l *link.Link) {
	c.clipMu.Lock()
	body := clipboardBody{Content: c.clip, Timestamp: c.clipTime}
	c.clipMu.Unlock()
	// Without a timestamp the other side cannot tell which copy is newer.
	if body.Content != "" && body.Timestamp != 0 && accepts(l.Peer, protocol.TypeClipboardConnect) {
		send(l, protocol.TypeClipboardConnect, body)
	}
}

// Commands: each device publishes a list of named commands that paired
// devices may trigger. Only the listed commands can be run, never arbitrary
// ones, and they take no arguments.

// Command is one entry of a device's command list.
type Command struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

// CommandsPath returns the file that defines this device's commands: a JSON
// object mapping a short name to a shell command line.
func (c *Core) CommandsPath() string {
	return filepath.Join(c.dir, "commands.json")
}

// localCommands reads the command file on every use, so edits apply at once.
func (c *Core) localCommands() map[string]string {
	data, err := os.ReadFile(c.CommandsPath())
	if err != nil {
		return nil
	}
	var commands map[string]string
	if err := json.Unmarshal(data, &commands); err != nil {
		c.log.Warn("cannot read commands", "file", c.CommandsPath(), "err", err)
		return nil
	}
	return commands
}

func (c *Core) sendCommandList(l *link.Link) {
	if !accepts(l.Peer, protocol.TypeRunCommand) {
		return
	}
	list := map[string]map[string]string{}
	for name, command := range c.localCommands() {
		list[name] = map[string]string{"name": name, "command": command}
	}
	// The protocol carries the list as a JSON document inside a string.
	encoded, _ := json.Marshal(list)
	send(l, protocol.TypeRunCommand, map[string]any{"commandList": string(encoded), "canAddCommand": false})
}

func (c *Core) handleRunRequest(l *link.Link, p protocol.Packet) {
	var body struct {
		RequestCommandList bool   `json:"requestCommandList"`
		Key                string `json:"key"`
	}
	if err := json.Unmarshal(p.Body, &body); err != nil {
		return
	}
	if body.RequestCommandList {
		c.sendCommandList(l)
	}
	if body.Key == "" {
		return
	}
	command, ok := c.localCommands()[body.Key]
	if !ok {
		c.log.Warn("asked to run an unknown command", "device", l.Peer.DeviceName, "command", body.Key)
		return
	}
	c.log.Info("running command", "device", l.Peer.DeviceName, "command", body.Key)
	cmd := desktop.Shell(command)
	if err := cmd.Start(); err != nil {
		c.log.Warn("command failed to start", "command", body.Key, "err", err)
		return
	}
	go cmd.Wait()
}

func (c *Core) handleCommandList(l *link.Link, p protocol.Packet) {
	var body struct {
		CommandList string `json:"commandList"`
	}
	if err := json.Unmarshal(p.Body, &body); err != nil || body.CommandList == "" {
		return
	}
	var list map[string]struct{ Name, Command string }
	if err := json.Unmarshal([]byte(body.CommandList), &list); err != nil {
		return
	}
	commands := make([]Command, 0, len(list))
	for key, entry := range list {
		commands = append(commands, Command{Key: key, Name: entry.Name, Command: entry.Command})
	}
	sort.Slice(commands, func(i, j int) bool { return commands[i].Name < commands[j].Name })

	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.devices[l.Peer.DeviceID]; d != nil && d.link == l {
		d.commands = commands
	}
}

// Run triggers one of the commands a device offers, chosen by key or name.
func (c *Core) Run(query, command string) error {
	l, err := c.pairedLink(query)
	if err != nil {
		return err
	}
	c.mu.Lock()
	offered := c.devices[l.Peer.DeviceID].commands
	c.mu.Unlock()
	for _, cmd := range offered {
		if cmd.Key == command || strings.EqualFold(cmd.Name, command) {
			return send(l, protocol.TypeRunRequest, map[string]string{"key": cmd.Key})
		}
	}
	return fmt.Errorf("device has no command %q", command)
}

// Ping, ring and battery.

func (c *Core) handlePing(l *link.Link, p protocol.Packet) {
	var body struct {
		Message string `json:"message"`
	}
	json.Unmarshal(p.Body, &body)
	if body.Message == "" {
		body.Message = "Пинг!"
	}
	c.log.Info("ping", "device", l.Peer.DeviceName)
	desktop.Notify(l.Peer.DeviceName, body.Message)
}

// Ping shows a notification on the device.
func (c *Core) Ping(query string) error {
	l, err := c.pairedLink(query)
	if err != nil {
		return err
	}
	return send(l, protocol.TypePing, struct{}{})
}

// Ring makes the device play a sound so it can be found.
func (c *Core) Ring(query string) error {
	l, err := c.pairedLink(query)
	if err != nil {
		return err
	}
	if !accepts(l.Peer, protocol.TypeRing) {
		return errors.New("device cannot ring")
	}
	return send(l, protocol.TypeRing, struct{}{})
}

// Battery is the charge a device reports.
type Battery struct {
	Charge   int  `json:"currentCharge"`
	Charging bool `json:"isCharging"`
}

func (c *Core) handleBattery(l *link.Link, p protocol.Packet) {
	var b Battery
	if err := json.Unmarshal(p.Body, &b); err != nil || b.Charge < 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.devices[l.Peer.DeviceID]; d != nil && d.link == l {
		d.battery = &b
	}
}

func (c *Core) sendBattery(l *link.Link) {
	charge, charging, ok := desktop.Battery()
	if ok && accepts(l.Peer, protocol.TypeBattery) {
		send(l, protocol.TypeBattery, Battery{Charge: charge, Charging: charging})
	}
}

// RunBattery reports this machine's battery to paired devices as it changes.
func (c *Core) RunBattery(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	var last Battery
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		charge, charging, ok := desktop.Battery()
		if now := (Battery{charge, charging}); ok && now != last {
			last = now
			for _, l := range c.pairedLinks() {
				c.sendBattery(l)
			}
		}
	}
}

// SendInput replays mouse and keyboard actions on a device: what a phone
// used as a remote control sends.
func (c *Core) SendInput(query string, events []input.Event) error {
	l, err := c.pairedLink(query)
	if err != nil {
		return err
	}
	if !accepts(l.Peer, protocol.TypeInput) {
		return errors.New("device cannot be controlled")
	}
	for _, e := range events {
		if err := send(l, protocol.TypeInput, e); err != nil {
			return err
		}
	}
	return nil
}

// Lock asks a device to lock its screen.
func (c *Core) Lock(query string) error {
	l, err := c.pairedLink(query)
	if err != nil {
		return err
	}
	if !accepts(l.Peer, protocol.TypeLock) {
		return errors.New("device cannot be locked from here")
	}
	return send(l, protocol.TypeLock, struct{}{})
}

// Notification is a notification shown on a phone, passed on to the
// computers so that it is seen there too.
type Notification struct {
	// Key tells notifications apart on the phone that showed them.
	Key   string `json:"key,omitempty"`
	App   string `json:"app"`
	Title string `json:"title"`
	Text  string `json:"text"`
	// CanReply is set when the notification is a message that can be answered.
	CanReply bool `json:"canReply,omitempty"`
	// Call marks a phone call; End says it is over, or that a notification
	// was dismissed on the phone.
	Call bool `json:"call,omitempty"`
	End  bool `json:"end,omitempty"`

	// Filled in on the computer that received it.
	DeviceID string `json:"deviceId,omitempty"`
	Device   string `json:"device,omitempty"`
	Time     int64  `json:"time,omitempty"`
}

// notifyKept is how many notifications of the phones are remembered.
const notifyKept = 60

type notifications struct {
	sync.Mutex
	list []Notification // newest first
	// paused are the players stopped for a phone call, and resume the timer
	// that starts them again once the call is over.
	paused []string
	resume *time.Timer
}

// Notified sends a notification of this device to the paired computers.
func (c *Core) Notified(n Notification) {
	for _, l := range c.pairedLinks() {
		if accepts(l.Peer, protocol.TypeNotification) && l.Peer.DeviceType != "phone" {
			send(l, protocol.TypeNotification, n)
		}
	}
}

// Notifications lists the recent notifications of the phones, newest first.
func (c *Core) Notifications() []Notification {
	c.notes.Lock()
	defer c.notes.Unlock()
	return append([]Notification{}, c.notes.list...)
}

func (c *Core) handleNotification(l *link.Link, p protocol.Packet) {
	var n Notification
	if err := json.Unmarshal(p.Body, &n); err != nil || c.peerSync(l.Peer.DeviceID).NotifyOff {
		return
	}
	n.DeviceID, n.Device, n.Time = l.Peer.DeviceID, l.Peer.DeviceName, time.Now().UnixMilli()
	same := func(x Notification) bool { return x.DeviceID == n.DeviceID && x.Key == n.Key && n.Key != "" }

	c.notes.Lock()
	kept := c.notes.list[:0:0]
	for _, x := range c.notes.list {
		if !same(x) {
			kept = append(kept, x)
		}
	}
	if n.End {
		c.notes.list = kept
		if n.Call {
			// Music comes back a moment after the call, unless the phone
			// only swapped one call notification for another.
			paused := c.notes.paused
			c.notes.resume = time.AfterFunc(2*time.Second, func() {
				c.notes.Lock()
				c.notes.paused, c.notes.resume = nil, nil
				c.notes.Unlock()
				desktop.ResumeMedia(paused)
			})
		}
		c.notes.Unlock()
		return
	}
	if n.Title == "" && n.Text == "" {
		c.notes.Unlock()
		return
	}
	if !n.Call {
		c.notes.list = append([]Notification{n}, kept...)
		if len(c.notes.list) > notifyKept {
			c.notes.list = c.notes.list[:notifyKept]
		}
	}
	ringing := n.Call && c.notes.resume == nil && c.notes.paused == nil
	if n.Call && c.notes.resume != nil {
		c.notes.resume.Stop()
		c.notes.resume = nil
	}
	c.notes.Unlock()

	title := n.Title
	if n.App != "" && n.App != n.Title {
		title = n.App + ": " + n.Title
	}
	switch {
	case n.Call:
		if !ringing {
			return
		}
		paused := desktop.PauseMedia()
		c.notes.Lock()
		c.notes.paused = paused
		c.notes.Unlock()
		desktop.Notify("Звонок · "+n.Device, strings.TrimSpace(n.Title+"\n"+n.Text))
	case n.CanReply:
		go func() {
			if desktop.NotifyAction(title+" · "+n.Device, n.Text, "Ответить") && c.OnReply != nil {
				c.OnReply(n.DeviceID)
			}
		}()
	default:
		desktop.Notify(title+" · "+n.Device, n.Text)
	}
}

// Reply answers a message shown in a notification of a phone.
func (c *Core) Reply(query, key, text string) error {
	l, err := c.pairedLink(query)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("nothing to send")
	}
	if !accepts(l.Peer, protocol.TypeNotifyReply) {
		return errors.New("device cannot send replies")
	}
	return send(l, protocol.TypeNotifyReply, map[string]string{"key": key, "text": text})
}

// handleNotifyReply hands a reply typed on a computer to the application.
func (c *Core) handleNotifyReply(p protocol.Packet) {
	var body struct{ Key, Text string }
	if json.Unmarshal(p.Body, &body) == nil && body.Key != "" && body.Text != "" {
		phonehost.Push(phonehost.Event{Type: "reply", Key: body.Key, Text: body.Text})
	}
}
