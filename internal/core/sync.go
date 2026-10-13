package core

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/input"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/pointer"
	"github.com/charg3d/perekat/internal/protocol"
)

// What two paired devices share is set for the pair: the clipboard, the
// notifications, the files, and a mouse and keyboard that serve both
// computers. With a shared mouse and keyboard one computer is the main one:
// its pointer leaves through an edge of the screen and comes out on the
// other computer, the keyboard follows it, and pushing against the same
// edge from the other side brings both back.

// peerSync is what is kept for one paired device. The zero value means
// everything is shared and no mouse is.
type peerSync struct {
	ClipboardOff  bool `json:"clipboardOff,omitempty"`
	NotifyOff     bool `json:"notifyOff,omitempty"`
	FilesOff      bool `json:"filesOff,omitempty"`
	FileAccessOff bool `json:"fileAccessOff,omitempty"`
	// Input is the shared mouse and keyboard. Main is the id of the device
	// whose mouse and keyboard they are; Side is where the other screen
	// stands, seen from the main one: left, right, top or bottom.
	InputOn bool   `json:"inputOn,omitempty"`
	Main    string `json:"main,omitempty"`
	Side    string `json:"side,omitempty"`
}

// SyncSettings is what the window shows and changes for a pair.
type SyncSettings struct {
	Clipboard     bool `json:"clipboard"`
	Notifications bool `json:"notifications"`
	Files         bool `json:"files"`
	FileAccess    bool `json:"fileAccess"`
	// Input is on when the mouse and keyboard are shared. MainSelf says
	// whether they are this device's.
	Input    bool   `json:"input"`
	MainSelf bool   `json:"mainSelf"`
	Side     string `json:"side"`
	// CanInput is false when one of the two cannot share a mouse, and
	// InputWhy says which and why.
	CanInput bool   `json:"canInput"`
	InputWhy string `json:"inputWhy,omitempty"`
}

var sides = map[string]string{"left": "right", "right": "left", "top": "bottom", "bottom": "top"}

func (c *Core) peerSync(id string) peerSync {
	c.settingsMu.Lock()
	defer c.settingsMu.Unlock()
	return c.stored.Sync[id]
}

func (c *Core) setPeerSync(id string, p peerSync) error {
	c.settingsMu.Lock()
	if c.stored.Sync == nil {
		c.stored.Sync = map[string]peerSync{}
	}
	c.stored.Sync[id] = p
	data, _ := json.MarshalIndent(c.stored, "", "  ")
	c.settingsMu.Unlock()
	return os.WriteFile(c.settingsPath(), data, 0o600)
}

// Sync returns what is shared with a device.
func (c *Core) Sync(query string) (SyncSettings, error) {
	c.mu.Lock()
	id, err := c.find(query)
	var d *device
	if err == nil {
		d = c.devices[id]
	}
	c.mu.Unlock()
	if err != nil {
		return SyncSettings{}, err
	}
	p := c.peerSync(id)
	s := SyncSettings{
		Clipboard: !p.ClipboardOff, Notifications: !p.NotifyOff, Files: !p.FilesOff, FileAccess: !p.FileAccessOff,
		Input: p.InputOn, MainSelf: c.isMain(c.links.Self.DeviceID), Side: p.Side,
	}
	main := c.mainID()
	if s.Side == "" {
		s.Side = "right"
	}
	switch {
	case main != id && main != c.links.Self.DeviceID:
		s.InputWhy = "a mouse and keyboard are shared with the main device: choose it in the settings"
	case d == nil || d.link == nil:
		s.InputWhy = "the device is not connected"
	case !accepts(d.identity, protocol.TypeKVM) || d.identity.DeviceType == "phone" || c.links.Self.DeviceType == "phone":
		s.InputWhy = "a mouse and keyboard are shared between two computers"
	case !input.Supported || !pointer.Supported():
		s.InputWhy = "this computer cannot tell where its pointer is (on Linux this needs Hyprland)"
	case !d.pointer:
		s.InputWhy = "the other computer cannot tell where its pointer is (on Linux this needs Hyprland)"
	default:
		s.CanInput = true
	}
	return s, nil
}

// SaveSync changes what is shared with a device and tells the device what
// concerns it.
func (c *Core) SaveSync(query string, s SyncSettings) error {
	c.mu.Lock()
	id, err := c.find(query)
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if _, ok := sides[s.Side]; !ok {
		return errors.New("the side must be left, right, top or bottom")
	}
	p := peerSync{ClipboardOff: !s.Clipboard, NotifyOff: !s.Notifications, FilesOff: !s.Files, FileAccessOff: !s.FileAccess, InputOn: s.Input, Side: s.Side}
	// The mouse and keyboard are the main device's; without one of the two
	// being it, there is nothing to share.
	p.Main = c.mainID()
	if p.Main != id && p.Main != c.links.Self.DeviceID {
		p.InputOn = false
	}
	if err := c.setPeerSync(id, p); err != nil {
		return err
	}
	if !p.InputOn {
		c.leaveRemote(id)
	}
	if l, err := c.pairedLink(id); err == nil && accepts(l.Peer, protocol.TypeKVM) {
		send(l, protocol.TypeKVM, kvmPacket{Op: "config", On: p.InputOn, Main: p.Main, Side: p.Side})
	}
	return nil
}

// kvmPacket is what two computers tell each other about the shared mouse.
type kvmPacket struct {
	// Op is hello, config, enter, leave or exit.
	Op string `json:"op"`
	// Pointer says, in a hello, that the sender can follow its pointer.
	Pointer bool   `json:"pointer,omitempty"`
	On      bool   `json:"on,omitempty"`
	Main    string `json:"main,omitempty"`
	Side    string `json:"side,omitempty"`
	// At is how far along the edge the pointer crossed, from 0 to 1.
	At float64 `json:"at,omitempty"`
}

// kvm is the state of the shared mouse on this machine.
type kvm struct {
	sync.Mutex
	// returned is when the pointer last came back, to keep it from
	// bouncing straight out again.
	returned time.Time
	// guest is set while another computer's mouse drives this one; closing
	// stop ends the watch for the pointer leaving.
	guest string
	stop  chan struct{}
	// held are the keys the other computer has down here.
	held map[int]bool
}

func (c *Core) greetKVM(l *link.Link) {
	if accepts(l.Peer, protocol.TypeKVM) {
		send(l, protocol.TypeKVM, kvmPacket{Op: "hello", Pointer: input.Supported && pointer.Supported()})
	}
}

func (c *Core) handleKVM(l *link.Link, p protocol.Packet) {
	var k kvmPacket
	if err := json.Unmarshal(p.Body, &k); err != nil {
		return
	}
	id := l.Peer.DeviceID
	switch k.Op {
	case "hello":
		c.mu.Lock()
		if d := c.devices[id]; d != nil && d.link == l {
			d.pointer = k.Pointer
		}
		c.mu.Unlock()
	case "config":
		if _, ok := sides[k.Side]; !ok || (k.Main != id && k.Main != c.links.Self.DeviceID) {
			return
		}
		s := c.peerSync(id)
		s.InputOn, s.Main, s.Side = k.On, k.Main, k.Side
		c.setPeerSync(id, s)
		if !k.On {
			c.leaveRemote(id)
			c.endGuest(id)
		}
	case "enter":
		s := c.peerSync(id)
		if s.InputOn && s.Main == id {
			c.beGuest(l, sides[s.Side], k.At)
		}
	case "leave":
		// The pointer came back over the edge: take the mouse back and put
		// the pointer where it came out.
		s := c.peerSync(id)
		c.mu.Lock()
		ours := c.remote != nil && c.remote.target == id
		if ours {
			c.stopRemote()
		}
		c.mu.Unlock()
		if ours {
			c.kvm.Lock()
			c.kvm.returned = time.Now()
			c.kvm.Unlock()
			warpToEdge(s.Side, k.At, 6)
		}
	case "exit":
		c.endGuest(id)
	}
}

// warpToEdge puts the pointer on an edge of the screen, inset from it.
func warpToEdge(side string, at float64, inset int) {
	minX, minY, maxX, maxY, ok := pointer.Bounds()
	if !ok {
		return
	}
	at = min(1, max(0, at))
	x := minX + int(at*float64(maxX-minX-1))
	y := minY + int(at*float64(maxY-minY-1))
	switch side {
	case "left":
		x = minX + inset
	case "right":
		x = maxX - 1 - inset
	case "top":
		y = minY + inset
	case "bottom":
		y = maxY - 1 - inset
	}
	pointer.Warp(x, y)
}

// atEdge reports whether the pointer is on an edge and how far along it.
func atEdge(side string) (on bool, at float64, ok bool) {
	x, y, ok := pointer.Position()
	minX, minY, maxX, maxY, ok2 := pointer.Bounds()
	if !ok || !ok2 || maxX-minX < 2 || maxY-minY < 2 {
		return false, 0, false
	}
	alongX := float64(x-minX) / float64(maxX-minX-1)
	alongY := float64(y-minY) / float64(maxY-minY-1)
	switch side {
	case "left":
		return x <= minX, alongY, true
	case "right":
		return x >= maxX-1, alongY, true
	case "top":
		return y <= minY, alongX, true
	default:
		return y >= maxY-1, alongX, true
	}
}

// RunInputShare watches the pointer of the main computer and hands the mouse
// and keyboard over when it pushes against the edge the other computer
// stands at. It returns when ctx ends.
func (c *Core) RunInputShare(stop <-chan struct{}) {
	// PEREKAT_NO_INPUT_SHARE keeps a test instance from taking the mouse.
	if !input.Supported || os.Getenv("PEREKAT_NO_INPUT_SHARE") != "" {
		return
	}
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	pushes := 0
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		id, side := c.inputPeer()
		c.mu.Lock()
		busy := c.remote != nil
		c.mu.Unlock()
		c.kvm.Lock()
		cooling := time.Since(c.kvm.returned) < 500*time.Millisecond
		c.kvm.Unlock()
		if id == "" || busy || cooling {
			pushes = 0
			continue
		}
		on, at, ok := atEdge(side)
		if !ok || !on {
			pushes = 0
			continue
		}
		// A moment against the edge, so that a pointer flung into a corner
		// does not jump away.
		if pushes++; pushes < 4 {
			continue
		}
		pushes = 0
		l, err := c.pairedLink(id)
		if err != nil {
			continue
		}
		if on, err := c.Remote(id); err != nil || !on {
			continue
		}
		send(l, protocol.TypeKVM, kvmPacket{Op: "enter", At: at})
	}
}

// inputPeer returns the connected computer this one shares its mouse with,
// as the main one, and the side it stands at.
func (c *Core) inputPeer() (id, side string) {
	c.settingsMu.Lock()
	candidates := map[string]string{}
	for peer, s := range c.stored.Sync {
		if s.InputOn && s.Main == c.links.Self.DeviceID {
			candidates[peer] = s.Side
		}
	}
	c.settingsMu.Unlock()
	if len(candidates) == 0 {
		return "", ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for peer, side := range candidates {
		if d := c.devices[peer]; d != nil && d.link != nil && d.pointer && c.trust.Matches(peer, d.link.Cert.Raw) {
			return peer, side
		}
	}
	return "", ""
}

// leaveRemote gives the mouse and keyboard back to this machine if a device
// has them, and tells the device.
func (c *Core) leaveRemote(id string) {
	c.mu.Lock()
	ours := c.remote != nil && c.remote.target == id
	if ours {
		c.stopRemote()
	}
	c.mu.Unlock()
	if !ours {
		return
	}
	if l, err := c.pairedLink(id); err == nil && accepts(l.Peer, protocol.TypeKVM) {
		send(l, protocol.TypeKVM, kvmPacket{Op: "exit"})
	}
}

// beGuest receives the main computer's pointer on an edge of this screen and
// watches for it going back.
func (c *Core) beGuest(l *link.Link, side string, at float64) {
	id := l.Peer.DeviceID
	c.endGuest(id)
	warpToEdge(side, at, 6)
	stop := make(chan struct{})
	c.kvm.Lock()
	c.kvm.guest, c.kvm.stop = id, stop
	c.kvm.Unlock()
	go func() {
		tick := time.NewTicker(15 * time.Millisecond)
		defer tick.Stop()
		pushes := 0
		for {
			select {
			case <-stop:
				return
			case <-l.Done():
				c.endGuest(id)
				return
			case <-tick.C:
			}
			on, along, ok := atEdge(side)
			if !ok || !on {
				pushes = 0
				continue
			}
			if pushes++; pushes < 3 {
				continue
			}
			c.endGuest(id)
			send(l, protocol.TypeKVM, kvmPacket{Op: "leave", At: along})
			return
		}
	}()
}

// endGuest stops watching for the pointer of a main computer and lets go of
// every key it left down here.
func (c *Core) endGuest(id string) {
	c.kvm.Lock()
	if c.kvm.guest != id {
		c.kvm.Unlock()
		return
	}
	close(c.kvm.stop)
	c.kvm.guest, c.kvm.stop = "", nil
	held := c.kvm.held
	c.kvm.held = nil
	c.kvm.Unlock()
	c.injectMu.Lock()
	defer c.injectMu.Unlock()
	if c.injector == nil {
		return
	}
	for code := range held {
		c.injector.Inject(input.Event{Code: code, Pressed: false})
	}
}

// noteKey remembers which keys another computer holds down here.
func (c *Core) noteKey(e input.Event) {
	if e.Code == 0 {
		return
	}
	c.kvm.Lock()
	defer c.kvm.Unlock()
	if c.kvm.held == nil {
		c.kvm.held = map[int]bool{}
	}
	if e.Pressed {
		c.kvm.held[e.Code] = true
	} else {
		delete(c.kvm.held, e.Code)
	}
}

// RunMenus keeps the file manager's "Send to ... via Perekat" entries in step
// with the paired devices, until stop is closed.
func (c *Core) RunMenus(stop <-chan struct{}) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		var targets []desktop.SendTarget
		for _, e := range c.trust.List() {
			targets = append(targets, desktop.SendTarget{ID: e.ID, Name: e.Name})
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].Name+targets[i].ID < targets[j].Name+targets[j].ID })
		desktop.SetSendMenu(targets)
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}
