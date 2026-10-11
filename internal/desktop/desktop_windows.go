//go:build windows

// Package desktop wraps the bits of the operating system perekat touches:
// notifications, sounds, the downloads folder, the shell and the battery.
package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procMessageBeep          = user32.NewProc("MessageBeep")
	procGetSystemPowerStatus = kernel32.NewProc("GetSystemPowerStatus")
)

// DownloadsDir returns the folder received files are saved to.
func DownloadsDir() string {
	return filepath.Join(os.Getenv("USERPROFILE"), "Downloads")
}

// Notify is not implemented on Windows yet: a toast needs an installed
// application identity. The window shows the same events.
func Notify(title, body string) {}

// Open opens a URL or a file with the default application.
func Open(target string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
}

// Shell prepares a command line for the user's shell.
func Shell(command string) *exec.Cmd {
	cmd := exec.Command("cmd", "/C", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}

// Ring plays a sound that helps find this machine.
func Ring() {
	const mbIconExclamation = 0x30
	for range 5 {
		procMessageBeep.Call(mbIconExclamation)
		time.Sleep(700 * time.Millisecond)
	}
}

// Battery reports the battery charge; ok is false without a battery.
func Battery() (charge int, charging, ok bool) {
	var status struct {
		ACLineStatus        byte
		BatteryFlag         byte
		BatteryLifePercent  byte
		SystemStatusFlag    byte
		BatteryLifeTime     uint32
		BatteryFullLifeTime uint32
	}
	const noBattery, unknown = 128, 255
	r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&status)))
	if r == 0 || status.BatteryFlag&noBattery != 0 || status.BatteryLifePercent == unknown {
		return 0, false, false
	}
	return int(status.BatteryLifePercent), status.ACLineStatus == 1, true
}
