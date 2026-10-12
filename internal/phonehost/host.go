// Package phonehost is the bridge to the Android application that runs the
// daemon on a phone. The daemon is a separate process there and cannot reach
// the clipboard, notifications, sounds or the battery, so it asks the
// application: requests wait in a queue the application drains over the
// local API, and the application reports back the same way.
package phonehost

import (
	"context"
	"sync"
)

// Event is something the application should do.
type Event struct {
	// Type is clipboard, notify or ring.
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

var (
	events    = make(chan Event, 64)
	clipboard = make(chan string, 8)

	mu          sync.Mutex
	downloadDir string
	charge      = -1
	charging    bool
)

// Push queues an event for the application, dropping it if nobody collects.
func Push(e Event) {
	select {
	case events <- e:
	default:
	}
}

// Next waits for the next event; ok is false if ctx ends first.
func Next(ctx context.Context) (e Event, ok bool) {
	select {
	case e = <-events:
		return e, true
	case <-ctx.Done():
		return Event{}, false
	}
}

// SetDownloadDir sets the folder received files go to.
func SetDownloadDir(dir string) {
	mu.Lock()
	defer mu.Unlock()
	downloadDir = dir
}

// DownloadDir returns the folder received files go to.
func DownloadDir() string {
	mu.Lock()
	defer mu.Unlock()
	return downloadDir
}

// SetBattery records what the application reports about the battery.
func SetBattery(percent int, isCharging bool) {
	mu.Lock()
	defer mu.Unlock()
	charge, charging = percent, isCharging
}

// Battery returns the last reported charge, or -1 if none was.
func Battery() (percent int, isCharging bool) {
	mu.Lock()
	defer mu.Unlock()
	return charge, charging
}

// ClipboardChanged records what the user copied on the phone.
func ClipboardChanged(text string) {
	select {
	case clipboard <- text:
	default:
	}
}

// Clipboard delivers what ClipboardChanged reports.
func Clipboard() <-chan string { return clipboard }
