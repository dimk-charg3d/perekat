// Package input captures the local mouse and keyboard and replays the events
// received from another device.
package input

// Event is one mouse action: a movement, a wheel step or a button change.
type Event struct {
	DX int `json:"dx,omitempty"`
	DY int `json:"dy,omitempty"`
	// Wheel and HWheel count notches; positive is up and right.
	Wheel  int `json:"wheel,omitempty"`
	HWheel int `json:"hwheel,omitempty"`
	// Button is "left", "right", "middle", "back" or "forward".
	Button  string `json:"button,omitempty"`
	Pressed bool   `json:"pressed,omitempty"`
	// Code is a key going down or up, as a Linux key code: what a keyboard
	// shared between two computers sends.
	Code int `json:"code,omitempty"`
	// Key is a key to press and let go, by name: see Keys.
	Key string `json:"key,omitempty"`
	// Text is typed as it is, whatever the keyboard layout.
	Text string `json:"text,omitempty"`
}

// Keys are the keys that can be pressed by name, with their Linux key codes.
var Keys = map[string]uint16{
	"esc": 1, "backspace": 14, "tab": 15, "enter": 28, "space": 57,
	"home": 102, "up": 103, "pageup": 104, "left": 105, "right": 106, "end": 107, "down": 108, "pagedown": 109, "delete": 111,
	"mute": 113, "voldown": 114, "volup": 115, "next": 163, "playpause": 164, "prev": 165,
}

// IsMotion reports whether the event only moves the pointer or the wheel.
func (e Event) IsMotion() bool {
	return e.Button == "" && e.Key == "" && e.Text == "" && e.Code == 0
}

// Injector replays events on this machine.
type Injector interface {
	Inject(Event) error
	Close() error
}
