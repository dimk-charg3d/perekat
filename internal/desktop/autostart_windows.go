package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// CanAutostart says whether this system can start perekat at login.
const CanAutostart = true

// Autostart reports whether perekat starts when the user logs in.
func Autostart() bool {
	return exec.Command("reg", "query", runKey, "/v", "Perekat").Run() == nil
}

// SetAutostart makes perekat start, without its window, when the user logs in.
func SetAutostart(on bool) error {
	if !on {
		exec.Command("reg", "delete", runKey, "/v", "Perekat", "/f").Run()
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	command := `"` + exe + `" daemon`
	gui := filepath.Join(filepath.Dir(exe), "perekat-gui.exe")
	if strings.EqualFold(filepath.Base(exe), "perekat-gui.exe") {
		command = `"` + exe + `" -hidden`
	} else if _, err := os.Stat(gui); err == nil {
		command = `"` + gui + `" -hidden`
	}
	return exec.Command("reg", "add", runKey, "/v", "Perekat", "/t", "REG_SZ", "/d", command, "/f").Run()
}
