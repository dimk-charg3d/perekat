package input

import "time"

// Keys travel between machines as Linux key codes. For most keys that is
// also the scan code a PC keyboard sends; the ones below are sent with the
// "extended" prefix, and Windows wants to know that.
var extendedScan = map[uint16]uint16{
	96: 0x1C, 97: 0x1D, 98: 0x35, 99: 0x37, 100: 0x38,
	102: 0x47, 103: 0x48, 104: 0x49, 105: 0x4B, 106: 0x4D, 107: 0x4F, 108: 0x50, 109: 0x51, 110: 0x52, 111: 0x53,
	113: 0x20, 114: 0x2E, 115: 0x30, 125: 0x5B, 126: 0x5C, 127: 0x5D, 163: 0x19, 164: 0x22, 165: 0x10,
}

// lastPlainKey is the highest key code that is its own scan code.
const lastPlainKey = 88

const (
	keyLeftCtrl = 29
	// escapeGap is how close two presses of the left Ctrl must be to give
	// the keyboard and the mouse back.
	escapeGap = 400 * time.Millisecond
)

// escapeWatch notices the left Ctrl pressed twice in a row, quickly.
type escapeWatch struct {
	last time.Time
	down bool
}

// pressed is told of every key going down or up and reports the double press.
func (w *escapeWatch) key(code int, pressed bool) bool {
	if code != keyLeftCtrl {
		if pressed {
			w.last = time.Time{}
		}
		return false
	}
	if !pressed {
		w.down = false
		return false
	}
	if w.down {
		// The key is held: the keyboard repeats it.
		return false
	}
	w.down = true
	if !w.last.IsZero() && time.Since(w.last) < escapeGap {
		w.last = time.Time{}
		return true
	}
	w.last = time.Now()
	return false
}
