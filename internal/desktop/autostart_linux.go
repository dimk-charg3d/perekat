//go:build linux && !phone

package desktop

import (
	"fmt"
	"os"
	"path/filepath"
)

func autostartFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "autostart", "perekat.desktop")
}

// CanAutostart says whether this system can start perekat at login.
const CanAutostart = true

// Autostart reports whether perekat starts when the user logs in.
func Autostart() bool {
	_, err := os.Stat(autostartFile())
	return err == nil
}

// SetAutostart makes perekat start, without its window, when the user logs in.
func SetAutostart(on bool) error {
	file := autostartFile()
	if !on {
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	entry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=Перекат\nExec=%s\nIcon=perekat\nX-GNOME-Autostart-enabled=true\n", startCommand())
	return os.WriteFile(file, []byte(entry), 0o644)
}

// startCommand is what brings perekat up in the background: the window
// program started hidden, or the bare daemon when there is no window program.
func startCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return "perekat daemon"
	}
	if filepath.Base(exe) == "perekat-gui" {
		return fmt.Sprintf("%q -hidden", exe)
	}
	if gui := filepath.Join(filepath.Dir(exe), "perekat-gui"); fileExists(gui) {
		return fmt.Sprintf("%q -hidden", gui)
	}
	return fmt.Sprintf("%q daemon", exe)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
