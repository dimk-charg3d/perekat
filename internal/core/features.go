package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charg3d/perekat/internal/clipboard"
	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/link"
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
	err := clipboard.Watch(ctx, func(text string) {
		// What is already in the clipboard at startup is not a fresh copy.
		c.localClipboard(text, time.Since(start) < 500*time.Millisecond)
	})
	if err != nil && ctx.Err() == nil {
		c.log.Warn("clipboard sync is off", "err", err)
	}
}

func (c *Core) localClipboard(text string, initial bool) {
	if text == "" || len(text) > maxClipboard {
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
	for _, l := range c.pairedLinks() {
		if accepts(l.Peer, protocol.TypeClipboard) {
			send(l, protocol.TypeClipboard, clipboardBody{Content: text})
		}
	}
}

func (c *Core) handleClipboard(l *link.Link, p protocol.Packet) {
	var body clipboardBody
	if err := json.Unmarshal(p.Body, &body); err != nil {
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
