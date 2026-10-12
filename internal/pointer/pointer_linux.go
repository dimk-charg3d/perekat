//go:build linux && !phone

// Package pointer tells where the mouse pointer is and moves it there. A
// keyboard and mouse shared between two computers need it to notice the
// pointer reaching the edge of the screen.
//
// Wayland keeps the pointer's position from programs, so on Linux this works
// where the compositor offers it: on Hyprland, through its control socket.
package pointer

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func socket() string {
	dir := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "hypr")
	if sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE"); sig != "" {
		return filepath.Join(dir, sig, ".socket.sock")
	}
	// Started outside the session: the newest instance is the running one.
	entries, _ := os.ReadDir(dir)
	newest, path := time.Time{}, ""
	for _, e := range entries {
		candidate := filepath.Join(dir, e.Name(), ".socket.sock")
		if info, err := os.Stat(candidate); err == nil && info.ModTime().After(newest) {
			newest, path = info.ModTime(), candidate
		}
	}
	return path
}

func ask(command string) (string, bool) {
	path := socket()
	if path == "" {
		return "", false
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return "", false
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := conn.Write([]byte(command)); err != nil {
		return "", false
	}
	out, err := io.ReadAll(conn)
	return string(out), err == nil
}

// Supported says whether the pointer can be followed on this machine.
func Supported() bool {
	_, _, ok := Position()
	return ok
}

// Position returns where the pointer is.
func Position() (x, y int, ok bool) {
	out, ok := ask("cursorpos")
	if !ok {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimSpace(out), ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	x, errX := strconv.Atoi(strings.TrimSpace(parts[0]))
	y, errY := strconv.Atoi(strings.TrimSpace(parts[1]))
	return x, y, errX == nil && errY == nil
}

// Bounds returns the rectangle all the screens together cover.
func Bounds() (minX, minY, maxX, maxY int, ok bool) {
	out, ok := ask("j/monitors")
	if !ok {
		return 0, 0, 0, 0, false
	}
	var monitors []struct {
		X, Y, Width, Height int
		Scale               float64
		Transform           int
		Disabled            bool
	}
	if json.Unmarshal([]byte(out), &monitors) != nil {
		return 0, 0, 0, 0, false
	}
	first := true
	for _, m := range monitors {
		if m.Disabled || m.Scale <= 0 {
			continue
		}
		w, h := int(float64(m.Width)/m.Scale), int(float64(m.Height)/m.Scale)
		if m.Transform%2 == 1 {
			w, h = h, w
		}
		if first || m.X < minX {
			minX = m.X
		}
		if first || m.Y < minY {
			minY = m.Y
		}
		if first || m.X+w > maxX {
			maxX = m.X + w
		}
		if first || m.Y+h > maxY {
			maxY = m.Y + h
		}
		first = false
	}
	return minX, minY, maxX, maxY, !first
}

// Warp puts the pointer at a point of the screen.
func Warp(x, y int) {
	ask("dispatch movecursor " + strconv.Itoa(x) + " " + strconv.Itoa(y))
}
