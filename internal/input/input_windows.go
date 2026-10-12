//go:build windows

package input

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procSendInput           = user32.NewProc("SendInput")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procPostThreadMessage   = user32.NewProc("PostThreadMessageW")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetCursorPos        = user32.NewProc("SetCursorPos")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procGetCurrentThreadID  = kernel32.NewProc("GetCurrentThreadId")
)

const (
	inputMouse = 0

	mouseeventfMove       = 0x0001
	mouseeventfLeftDown   = 0x0002
	mouseeventfLeftUp     = 0x0004
	mouseeventfRightDown  = 0x0008
	mouseeventfRightUp    = 0x0010
	mouseeventfMiddleDown = 0x0020
	mouseeventfMiddleUp   = 0x0040
	mouseeventfXDown      = 0x0080
	mouseeventfXUp        = 0x0100
	mouseeventfWheel      = 0x0800
	mouseeventfHWheel     = 0x1000

	xbutton1   = 1
	xbutton2   = 2
	wheelDelta = 120

	whMouseLL      = 14
	llmhfInjected  = 0x01
	wmQuit         = 0x0012
	wmMouseMove    = 0x0200
	wmLButtonDown  = 0x0201
	wmLButtonUp    = 0x0202
	wmRButtonDown  = 0x0204
	wmRButtonUp    = 0x0205
	wmMButtonDown  = 0x0207
	wmMButtonUp    = 0x0208
	wmMouseWheel   = 0x020A
	wmXButtonDown  = 0x020B
	wmXButtonUp    = 0x020C
	wmMouseHWheel  = 0x020E
	smCXScreen     = 0
	smCYScreen     = 1
	blockEvent     = 1
	hookActionCode = 0
)

type point struct{ X, Y int32 }

type mouseInput struct {
	DX, DY    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type winInput struct {
	Type uint32
	Mi   mouseInput
}

type sendInputInjector struct{}

// NewInjector returns an injector that synthesizes mouse input.
func NewInjector() (Injector, error) {
	return sendInputInjector{}, nil
}

func (sendInputInjector) Inject(e Event) error {
	var inputs []winInput
	add := func(mi mouseInput) { inputs = append(inputs, winInput{Type: inputMouse, Mi: mi}) }

	if e.DX != 0 || e.DY != 0 {
		add(mouseInput{DX: int32(e.DX), DY: int32(e.DY), Flags: mouseeventfMove})
	}
	if e.Wheel != 0 {
		add(mouseInput{MouseData: uint32(int32(e.Wheel * wheelDelta)), Flags: mouseeventfWheel})
	}
	if e.HWheel != 0 {
		add(mouseInput{MouseData: uint32(int32(e.HWheel * wheelDelta)), Flags: mouseeventfHWheel})
	}
	down := func(pressed bool, d, u uint32) uint32 {
		if pressed {
			return d
		}
		return u
	}
	switch e.Button {
	case "left":
		add(mouseInput{Flags: down(e.Pressed, mouseeventfLeftDown, mouseeventfLeftUp)})
	case "right":
		add(mouseInput{Flags: down(e.Pressed, mouseeventfRightDown, mouseeventfRightUp)})
	case "middle":
		add(mouseInput{Flags: down(e.Pressed, mouseeventfMiddleDown, mouseeventfMiddleUp)})
	case "back":
		add(mouseInput{MouseData: xbutton1, Flags: down(e.Pressed, mouseeventfXDown, mouseeventfXUp)})
	case "forward":
		add(mouseInput{MouseData: xbutton2, Flags: down(e.Pressed, mouseeventfXDown, mouseeventfXUp)})
	}
	if e.Key != "" || e.Text != "" {
		typeKeys(e)
	}
	if e.Code > 0 {
		pressKey(e.Code, e.Pressed)
	}
	if len(inputs) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(inputs[0]))
	if int(n) != len(inputs) {
		return err
	}
	return nil
}

func (sendInputInjector) Close() error { return nil }

// keyInput is the keyboard form of the INPUT structure.
type keyInput struct {
	Type  uint32
	_     uint32
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	_     uint32
	Extra uintptr
	_     [8]byte
}

const (
	inputKeyboard    = 1
	keyeventfKeyUp   = 0x0002
	keyeventfUnicode = 0x0004
)

// virtualKeys are the Windows codes of the keys that can be pressed by name.
var virtualKeys = map[string]uint16{
	"esc": 0x1B, "backspace": 0x08, "tab": 0x09, "enter": 0x0D, "space": 0x20,
	"home": 0x24, "up": 0x26, "pageup": 0x21, "left": 0x25, "right": 0x27, "end": 0x23, "down": 0x28, "pagedown": 0x22, "delete": 0x2E,
	"mute": 0xAD, "voldown": 0xAE, "volup": 0xAF, "next": 0xB0, "playpause": 0xB3, "prev": 0xB1,
}

const (
	keyeventfExtended = 0x0001
	keyeventfScancode = 0x0008
)

// pressKey puts a key down or lets it go, by its Linux key code.
func pressKey(code int, pressed bool) {
	key := keyInput{Type: inputKeyboard, Flags: keyeventfScancode}
	if scan, ok := extendedScan[uint16(code)]; ok {
		key.Scan, key.Flags = scan, key.Flags|keyeventfExtended
	} else if code <= lastPlainKey {
		key.Scan = uint16(code)
	} else {
		return
	}
	if !pressed {
		key.Flags |= keyeventfKeyUp
	}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&key)), unsafe.Sizeof(key))
}

// typeKeys presses a named key or types text, character by character.
func typeKeys(e Event) {
	var keys []keyInput
	if vk, ok := virtualKeys[e.Key]; ok {
		keys = append(keys, keyInput{Type: inputKeyboard, Vk: vk}, keyInput{Type: inputKeyboard, Vk: vk, Flags: keyeventfKeyUp})
	}
	for _, unit := range syscall.StringToUTF16(e.Text) {
		if unit == 0 {
			break
		}
		keys = append(keys, keyInput{Type: inputKeyboard, Scan: unit, Flags: keyeventfUnicode}, keyInput{Type: inputKeyboard, Scan: unit, Flags: keyeventfUnicode | keyeventfKeyUp})
	}
	if len(keys) > 0 {
		procSendInput.Call(uintptr(len(keys)), uintptr(unsafe.Pointer(&keys[0])), unsafe.Sizeof(keys[0]))
	}
}

type msllHook struct {
	Pt        point
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// The hook callback is created once: Windows callbacks made by the Go runtime
// are never freed. It forwards to whichever capture is active.
var (
	hookEmit     atomic.Pointer[func(Event)]
	hookCallback uintptr
	hookOnce     sync.Once
)

func hookProc(code, wParam, lParam uintptr) uintptr {
	emit := hookEmit.Load()
	info := (*msllHook)(unsafe.Pointer(lParam))
	// Injected events are what the other device sends us; let them through.
	if int32(code) != hookActionCode || emit == nil || info.Flags&llmhfInjected != 0 {
		r, _, _ := procCallNextHookEx.Call(0, code, wParam, lParam)
		return r
	}

	hiword := int(int16(info.MouseData >> 16))
	button := func(name string, pressed bool) { (*emit)(Event{Button: name, Pressed: pressed}) }
	xbutton := func(pressed bool) {
		if hiword == xbutton2 {
			button("forward", pressed)
		} else {
			button("back", pressed)
		}
	}
	switch wParam {
	case wmMouseMove:
		// The event is swallowed, so the pointer stays put and the distance
		// to the reported position is the movement since the last event.
		var cur point
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&cur)))
		if dx, dy := int(info.Pt.X-cur.X), int(info.Pt.Y-cur.Y); dx != 0 || dy != 0 {
			(*emit)(Event{DX: dx, DY: dy})
		}
	case wmLButtonDown:
		button("left", true)
	case wmLButtonUp:
		button("left", false)
	case wmRButtonDown:
		button("right", true)
	case wmRButtonUp:
		button("right", false)
	case wmMButtonDown:
		button("middle", true)
	case wmMButtonUp:
		button("middle", false)
	case wmXButtonDown:
		xbutton(true)
	case wmXButtonUp:
		xbutton(false)
	case wmMouseWheel:
		(*emit)(Event{Wheel: hiword / wheelDelta})
	case wmMouseHWheel:
		(*emit)(Event{HWheel: hiword / wheelDelta})
	}
	return blockEvent
}

type kbdllHook struct {
	Vk, Scan, Flags, Time uint32
	Extra                 uintptr
}

const (
	whKeyboardLL  = 13
	llkhfExtended = 0x01
	llkhfInjected = 0x10
	llkhfUp       = 0x80
)

var (
	keyCallback uintptr
	hookEscape  atomic.Pointer[func()]
	keyWatch    escapeWatch
	// scanToCode turns an extended scan code back into a Linux key code.
	scanToCode = map[uint16]int{}
)

func keyProc(code, wParam, lParam uintptr) uintptr {
	emit := hookEmit.Load()
	info := (*kbdllHook)(unsafe.Pointer(lParam))
	if int32(code) != hookActionCode || emit == nil || info.Flags&llkhfInjected != 0 {
		r, _, _ := procCallNextHookEx.Call(0, code, wParam, lParam)
		return r
	}
	key := int(info.Scan)
	if info.Flags&llkhfExtended != 0 {
		key = scanToCode[uint16(info.Scan)]
	} else if key > lastPlainKey {
		key = 0
	}
	if key == 0 {
		return blockEvent
	}
	pressed := info.Flags&llkhfUp == 0
	if keyWatch.key(key, pressed) {
		(*emit)(Event{Code: key, Pressed: false})
		if escape := hookEscape.Load(); escape != nil {
			// The hook must return at once; leaving takes a moment.
			go (*escape)()
		}
		return blockEvent
	}
	(*emit)(Event{Code: key, Pressed: pressed})
	return blockEvent
}

// StartCapture swallows the local mouse and keyboard and reports what they do
// to emit. The returned function gives them back. emit runs inside the system
// input hook and must return quickly. escape is called when the left Ctrl is
// pressed twice quickly.
func StartCapture(emit func(Event), escape func()) (stop func(), err error) {
	hookOnce.Do(func() {
		hookCallback = syscall.NewCallback(hookProc)
		keyCallback = syscall.NewCallback(keyProc)
		for code, scan := range extendedScan {
			scanToCode[scan] = int(code)
		}
	})
	keyWatch = escapeWatch{}
	if escape != nil {
		hookEscape.Store(&escape)
	} else {
		hookEscape.Store(nil)
	}

	ready := make(chan error, 1)
	done := make(chan struct{})
	var threadID uintptr
	go func() {
		// A low-level hook is called on the thread that installed it, which
		// must keep pumping messages.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(done)
		threadID, _, _ = procGetCurrentThreadID.Call()

		// Movement is measured against a fixed pointer; parked at an edge it
		// could not report movement past that edge.
		w, _, _ := procGetSystemMetrics.Call(smCXScreen)
		h, _, _ := procGetSystemMetrics.Call(smCYScreen)
		procSetCursorPos.Call(w/2, h/2)

		hookEmit.Store(&emit)
		hook, _, err := procSetWindowsHookEx.Call(whMouseLL, hookCallback, 0, 0)
		if hook == 0 {
			hookEmit.Store(nil)
			ready <- err
			return
		}
		// The keyboard goes the same way; without it the mouse still works.
		keys, _, _ := procSetWindowsHookEx.Call(whKeyboardLL, keyCallback, 0, 0)
		if keys != 0 {
			defer procUnhookWindowsHookEx.Call(keys)
		}
		ready <- nil

		var msg struct {
			Hwnd    uintptr
			Message uint32
			WParam  uintptr
			LParam  uintptr
			Time    uint32
			Pt      point
			Private uint32
		}
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
		}
		hookEmit.Store(nil)
		procUnhookWindowsHookEx.Call(hook)
	}()

	if err := <-ready; err != nil {
		return nil, errors.Join(errors.New("cannot install the mouse hook"), err)
	}
	return func() {
		procPostThreadMessage.Call(threadID, wmQuit, 0, 0)
		<-done
	}, nil
}

// Supported says whether this build can capture and replay mouse input.
const Supported = true
