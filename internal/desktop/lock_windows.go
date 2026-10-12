package desktop

import "os/exec"

// Lock locks the screen.
func Lock() error {
	return exec.Command("rundll32.exe", "user32.dll,LockWorkStation").Run()
}

// PauseMedia does nothing here yet.
func PauseMedia() []string { return nil }

// ResumeMedia does nothing here yet.
func ResumeMedia(names []string) {}

// NotifyAction shows nothing here yet and reports no press.
func NotifyAction(title, body, button string) bool { return false }
