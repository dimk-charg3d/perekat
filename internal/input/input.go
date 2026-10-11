// Package input captures the local mouse and replays mouse events received
// from another device.
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
}

// IsMotion reports whether the event only moves the pointer or the wheel.
func (e Event) IsMotion() bool {
	return e.Button == ""
}

// Injector replays events on this machine.
type Injector interface {
	Inject(Event) error
	Close() error
}
