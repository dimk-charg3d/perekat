// Package pointer tells where the mouse pointer is and moves it there. A
// keyboard and mouse shared between two computers need it to notice the
// pointer reaching the edge of the screen.
package pointer

import (
	"syscall"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procGetCursorPos = user32.NewProc("GetCursorPos")
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procGetMetrics   = user32.NewProc("GetSystemMetrics")
)

// The rectangle of all the screens together.
const (
	smXVirtualScreen  = 76
	smYVirtualScreen  = 77
	smCXVirtualScreen = 78
	smCYVirtualScreen = 79
)

// Supported says whether the pointer can be followed on this machine.
func Supported() bool { return true }

// Position returns where the pointer is.
func Position() (x, y int, ok bool) {
	var p struct{ X, Y int32 }
	r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return int(p.X), int(p.Y), r != 0
}

func metric(n uintptr) int {
	r, _, _ := procGetMetrics.Call(n)
	return int(int32(r))
}

// Bounds returns the rectangle all the screens together cover.
func Bounds() (minX, minY, maxX, maxY int, ok bool) {
	minX, minY = metric(smXVirtualScreen), metric(smYVirtualScreen)
	w, h := metric(smCXVirtualScreen), metric(smCYVirtualScreen)
	return minX, minY, minX + w, minY + h, w > 0 && h > 0
}

// Warp puts the pointer at a point of the screen.
func Warp(x, y int) {
	procSetCursorPos.Call(uintptr(x), uintptr(y))
}
