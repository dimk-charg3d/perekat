//go:build linux && !phone

package input

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

// Capture reads evdev devices directly and injection goes through uinput, so
// both work the same under any compositor. Both need access to /dev/input and
// /dev/uinput, which membership in the "input" group grants.

const virtualName = "perekat virtual mouse"

const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03

	relX      = 0x00
	relY      = 0x01
	relHWheel = 0x06
	relWheel  = 0x08

	absX = 0x00
	absY = 0x01

	btnLeft       = 0x110
	btnRight      = 0x111
	btnMiddle     = 0x112
	btnSide       = 0x113
	btnExtra      = 0x114
	btnToolFinger = 0x145

	uiDevCreate = 0x5501
	uiDevSetup  = 0x405c5503
	uiSetEvBit  = 0x40045564
	uiSetKeyBit = 0x40045565
	uiSetRelBit = 0x40045566
	evIOCGrab   = 0x40044590

	busVirtual = 0x06

	// touchpadSpan is how many pixels a swipe across the whole touchpad moves.
	touchpadSpan = 1200
)

var buttonCodes = map[uint16]string{
	btnLeft:   "left",
	btnRight:  "right",
	btnMiddle: "middle",
	btnSide:   "back",
	btnExtra:  "forward",
}

type rawEvent struct {
	Time  syscall.Timeval
	Type  uint16
	Code  uint16
	Value int32
}

const rawEventSize = int(unsafe.Sizeof(rawEvent{}))

func ioctl(f *os.File, req uintptr, arg uintptr) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	err = rc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	})
	if err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}

// evIOCGBit builds the request that reads the capability bits of one event type.
func evIOCGBit(ev, size uintptr) uintptr { return 0x80000000 | size<<16 | 'E'<<8 | (0x20 + ev) }
func evIOCGName(size uintptr) uintptr    { return 0x80000000 | size<<16 | 'E'<<8 | 0x06 }
func evIOCGAbs(axis uintptr) uintptr     { return 0x80184540 + axis }

type uinputInjector struct {
	mu sync.Mutex
	f  *os.File
}

// NewInjector creates a virtual mouse.
func NewInjector() (Injector, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	setup := struct {
		Bustype, Vendor, Product, Version uint16
		Name                              [80]byte
		FFEffectsMax                      uint32
	}{Bustype: busVirtual, Vendor: 0x1209, Product: 0x7065, Version: 1}
	copy(setup.Name[:], virtualName)

	steps := []struct{ req, arg uintptr }{
		{uiSetEvBit, evKey}, {uiSetEvBit, evRel},
		{uiSetKeyBit, btnLeft}, {uiSetKeyBit, btnRight}, {uiSetKeyBit, btnMiddle},
		{uiSetKeyBit, btnSide}, {uiSetKeyBit, btnExtra},
		{uiSetRelBit, relX}, {uiSetRelBit, relY}, {uiSetRelBit, relWheel}, {uiSetRelBit, relHWheel},
	}
	// Every key of a keyboard, for a keyboard shared with another computer;
	// the kernel repeats the ones held down.
	steps = append(steps, struct{ req, arg uintptr }{uiSetEvBit, evRep})
	for code := 1; code <= maxKey; code++ {
		steps = append(steps, struct{ req, arg uintptr }{uiSetKeyBit, uintptr(code)})
	}
	steps = append(steps,
		struct{ req, arg uintptr }{uiDevSetup, uintptr(unsafe.Pointer(&setup))},
		struct{ req, arg uintptr }{uiDevCreate, 0},
	)
	for _, s := range steps {
		if err := ioctl(f, s.req, s.arg); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &uinputInjector{f: f}, nil
}

func (u *uinputInjector) Inject(e Event) error {
	evs := make([]rawEvent, 0, 6)
	add := func(typ, code uint16, value int) {
		if value != 0 || typ == evSyn || typ == evKey {
			evs = append(evs, rawEvent{Type: typ, Code: code, Value: int32(value)})
		}
	}
	add(evRel, relX, e.DX)
	add(evRel, relY, e.DY)
	add(evRel, relWheel, e.Wheel)
	add(evRel, relHWheel, e.HWheel)
	for code, name := range buttonCodes {
		if name == e.Button {
			pressed := 0
			if e.Pressed {
				pressed = 1
			}
			add(evKey, code, pressed)
		}
	}
	if e.Text != "" {
		// Typing text needs the layout worked out; wtype does that.
		go exec.Command("wtype", "--", e.Text).Run()
	}
	if e.Code > 0 && e.Code <= maxKey {
		pressed := 0
		if e.Pressed {
			pressed = 1
		}
		add(evKey, uint16(e.Code), pressed)
	}
	if code, ok := Keys[e.Key]; ok {
		add(evKey, code, 1)
		add(evSyn, 0, 0)
		add(evKey, code, 0)
	}
	if len(evs) == 0 {
		return nil
	}
	add(evSyn, 0, 0)

	u.mu.Lock()
	defer u.mu.Unlock()
	_, err := u.f.Write(unsafe.Slice((*byte)(unsafe.Pointer(&evs[0])), len(evs)*rawEventSize))
	return err
}

func (u *uinputInjector) Close() error {
	return u.f.Close()
}

type deviceKind int

const (
	kindNone deviceKind = iota
	kindMouse
	kindTouchpad
	kindKeyboard
)

const (
	evRep = 0x14
	// maxKey is the last code of a keyboard key; above it come buttons.
	maxKey   = 248
	keyA     = 30
	keyEnter = 28
)

// StartCapture takes exclusive hold of every mouse, touchpad and keyboard, so
// they stop acting on this machine, and reports what they do to emit. The
// returned function gives the devices back. escape is called when the left
// Ctrl is pressed twice quickly: the way out that needs nothing but the
// keyboard.
func StartCapture(emit func(Event), escape func()) (stop func(), err error) {
	paths, _ := filepath.Glob("/dev/input/event*")
	var files []*os.File
	var lastErr error
	for _, path := range paths {
		f, err := os.OpenFile(path, os.O_RDONLY, 0)
		if err != nil {
			lastErr = err
			continue
		}
		kind, scale := classify(f)
		if kind == kindNone {
			f.Close()
			continue
		}
		if err := ioctl(f, evIOCGrab, 1); err != nil {
			lastErr = err
			f.Close()
			continue
		}
		files = append(files, f)
		if kind == kindKeyboard {
			go readKeyboard(f, emit, escape)
			continue
		}
		go readDevice(f, kind, scale, emit)
	}
	if len(files) == 0 {
		if lastErr != nil {
			return nil, errors.Join(errors.New("no mouse could be captured (is the user in the input group?)"), lastErr)
		}
		return nil, errors.New("no mouse found")
	}
	return func() {
		for _, f := range files {
			f.Close()
		}
	}, nil
}

func hasBit(bits []byte, n int) bool {
	return n/8 < len(bits) && bits[n/8]&(1<<(n%8)) != 0
}

// classify tells mice and touchpads from other devices. For a touchpad it also
// returns the factor that turns its coordinates into pixels.
func classify(f *os.File) (deviceKind, float64) {
	var name [80]byte
	ioctl(f, evIOCGName(uintptr(len(name))), uintptr(unsafe.Pointer(&name[0])))
	if string(bytes.TrimRight(name[:], "\x00")) == virtualName {
		return kindNone, 0
	}

	var keys [96]byte
	ioctl(f, evIOCGBit(evKey, uintptr(len(keys))), uintptr(unsafe.Pointer(&keys[0])))
	if hasBit(keys[:], btnToolFinger) {
		var info struct{ Value, Min, Max, Fuzz, Flat, Res int32 }
		if err := ioctl(f, evIOCGAbs(absX), uintptr(unsafe.Pointer(&info))); err != nil || info.Max <= info.Min {
			return kindNone, 0
		}
		return kindTouchpad, touchpadSpan / float64(info.Max-info.Min)
	}

	var rel [8]byte
	ioctl(f, evIOCGBit(evRel, uintptr(len(rel))), uintptr(unsafe.Pointer(&rel[0])))
	if hasBit(rel[:], relX) && hasBit(rel[:], relY) && hasBit(keys[:], btnLeft) {
		return kindMouse, 0
	}
	if hasBit(keys[:], keyA) && hasBit(keys[:], keyEnter) {
		return kindKeyboard, 0
	}
	return kindNone, 0
}

func readKeyboard(f *os.File, emit func(Event), escape func()) {
	var watch escapeWatch
	buf := make([]byte, rawEventSize*64)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		for off := 0; off+rawEventSize <= n; off += rawEventSize {
			ev := (*rawEvent)(unsafe.Pointer(&buf[off]))
			if ev.Type != evKey || ev.Code > maxKey {
				continue
			}
			if watch.key(int(ev.Code), ev.Value != 0) && escape != nil {
				// Let the key go on the other machine before leaving it.
				emit(Event{Code: int(ev.Code), Pressed: false})
				escape()
				continue
			}
			emit(Event{Code: int(ev.Code), Pressed: ev.Value != 0})
		}
	}
}

func readDevice(f *os.File, kind deviceKind, scale float64, emit func(Event)) {
	var (
		motion         Event
		lastX, lastY   int32
		haveX, haveY   bool
		fracX, fracY   float64
		buf            = make([]byte, rawEventSize*64)
		flushIfPending = func() {
			if motion != (Event{}) {
				emit(motion)
				motion = Event{}
			}
		}
	)
	for {
		n, err := f.Read(buf)
		if err != nil {
			return
		}
		for off := 0; off+rawEventSize <= n; off += rawEventSize {
			ev := (*rawEvent)(unsafe.Pointer(&buf[off]))
			switch ev.Type {
			case evRel:
				switch ev.Code {
				case relX:
					motion.DX += int(ev.Value)
				case relY:
					motion.DY += int(ev.Value)
				case relWheel:
					motion.Wheel += int(ev.Value)
				case relHWheel:
					motion.HWheel += int(ev.Value)
				}
			case evKey:
				// A finger landing or lifting restarts touchpad tracking, so the
				// pointer does not jump by the distance between two touches.
				haveX, haveY = false, false
				if name, ok := buttonCodes[ev.Code]; ok && ev.Value != 2 {
					flushIfPending()
					emit(Event{Button: name, Pressed: ev.Value == 1})
				}
			case evAbs:
				if kind != kindTouchpad {
					continue
				}
				switch ev.Code {
				case absX:
					if haveX {
						fracX += float64(ev.Value-lastX) * scale
					}
					lastX, haveX = ev.Value, true
				case absY:
					if haveY {
						fracY += float64(ev.Value-lastY) * scale
					}
					lastY, haveY = ev.Value, true
				}
			case evSyn:
				if dx := int(fracX); dx != 0 {
					motion.DX += dx
					fracX -= float64(dx)
				}
				if dy := int(fracY); dy != 0 {
					motion.DY += dy
					fracY -= float64(dy)
				}
				flushIfPending()
			}
		}
	}
}

// Supported says whether this build can capture and replay mouse input.
const Supported = true
