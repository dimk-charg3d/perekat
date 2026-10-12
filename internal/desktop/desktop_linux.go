//go:build linux && !phone

// Package desktop wraps the bits of the operating system perekat touches:
// notifications, sounds, the downloads folder, the shell and the battery.
package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DownloadsDir returns the folder received files are saved to.
func DownloadsDir() string {
	if out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output(); err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			return dir
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

// Notify shows a desktop notification, if a notification service is running.
func Notify(title, body string) {
	exec.Command("notify-send", "--app-name=Перекат", title, body).Start()
}

// Open opens a URL or a file with the default application.
func Open(target string) error {
	return exec.Command("xdg-open", target).Start()
}

// Shell prepares a command line for the user's shell.
func Shell(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}

// Ring plays a sound that helps find this machine: a ringing phone, a few
// times over, loud enough to be heard from the next room.
func Ring() {
	sound := "/usr/share/sounds/freedesktop/stereo/phone-incoming-call.oga"
	for range 4 {
		if exec.Command("canberra-gtk-play", "--id=phone-incoming-call", "--volume=6").Run() != nil &&
			exec.Command("pw-play", sound).Run() != nil &&
			exec.Command("paplay", sound).Run() != nil {
			return
		}
	}
}

// Battery reports the charge of the first battery; ok is false without one.
func Battery() (charge int, charging, ok bool) {
	dirs, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, "capacity"))
		if err != nil {
			continue
		}
		charge, err = strconv.Atoi(strings.TrimSpace(string(raw)))
		if err != nil {
			continue
		}
		status, _ := os.ReadFile(filepath.Join(dir, "status"))
		state := strings.TrimSpace(string(status))
		return charge, state == "Charging" || state == "Full", true
	}
	return 0, false, false
}
