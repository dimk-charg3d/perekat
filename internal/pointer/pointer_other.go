//go:build phone || (!linux && !windows)

// Package pointer tells where the mouse pointer is and moves it there.
package pointer

// Supported says whether the pointer can be followed on this machine.
func Supported() bool { return false }

// Position returns where the pointer is.
func Position() (x, y int, ok bool) { return 0, 0, false }

// Bounds returns the rectangle all the screens together cover.
func Bounds() (minX, minY, maxX, maxY int, ok bool) { return 0, 0, 0, 0, false }

// Warp puts the pointer at a point of the screen.
func Warp(x, y int) {}
