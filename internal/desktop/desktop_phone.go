//go:build phone

// Package desktop wraps the bits of the operating system perekat touches:
// notifications, sounds, the downloads folder, the shell and the battery.
package desktop

import (
	"errors"
	"os"
	"os/exec"

	"github.com/charg3d/perekat/internal/phonehost"
)

// DownloadsDir returns the folder received files are saved to.
func DownloadsDir() string { return phonehost.DownloadDir() }

// Notify shows a notification.
func Notify(title, body string) {
	phonehost.Push(phonehost.Event{Type: "notify", Title: title, Body: body})
}

// Open is not available on a phone: links sent to it are ignored.
func Open(target string) error { return errors.New("not supported on a phone") }

// Shell prepares a command line for the system shell.
func Shell(command string) *exec.Cmd { return exec.Command("sh", "-c", command) }

// Ring plays a sound that helps find this phone.
func Ring() { phonehost.Push(phonehost.Event{Type: "ring"}) }

// Battery reports the battery charge.
func Battery() (charge int, charging, ok bool) {
	charge, charging = phonehost.Battery()
	return charge, charging, charge >= 0
}

// Trash deletes for good: a phone has no trash can.
func Trash(path string) error { return os.RemoveAll(path) }

// Roots lists the top-level locations when there is more than one.
func Roots() []string { return nil }
