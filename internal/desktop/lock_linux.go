//go:build linux && !phone

package desktop

import (
	"os/exec"
	"strings"
	"time"
)

// Lock locks the screen. The session is asked first, which is all it takes
// where something listens for that; otherwise a screen locker is started.
func Lock() error {
	exec.Command("loginctl", "lock-session").Run()
	lockers := []string{"hyprlock", "swaylock", "gtklock", "waylock"}
	running := func() bool {
		for _, name := range lockers {
			if exec.Command("pgrep", "-x", name).Run() == nil {
				return true
			}
		}
		return false
	}
	time.Sleep(700 * time.Millisecond)
	if running() {
		return nil
	}
	for _, name := range lockers {
		if path, err := exec.LookPath(name); err == nil {
			return exec.Command(path).Start()
		}
	}
	return nil
}

// playing lists the media players that are playing right now.
func playing() []string {
	out, err := exec.Command("playerctl", "-a", "metadata", "--format", "{{playerName}} {{status}}").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if name, ok := strings.CutSuffix(strings.TrimSpace(line), " Playing"); ok {
			names = append(names, name)
		}
	}
	return names
}

// PauseMedia pauses whatever is playing and returns what it paused, for
// ResumeMedia to start again.
func PauseMedia() []string {
	names := playing()
	for _, name := range names {
		exec.Command("playerctl", "-p", name, "pause").Run()
	}
	return names
}

// ResumeMedia starts the players PauseMedia stopped.
func ResumeMedia(names []string) {
	for _, name := range names {
		exec.Command("playerctl", "-p", name, "play").Run()
	}
}

// NotifyAction shows a notification with a button and reports whether the
// button was pressed. It returns when the notification is gone.
func NotifyAction(title, body, button string) bool {
	out, _ := exec.Command("notify-send", "--app-name=Перекат", "-A", "act="+button, title, body).Output()
	return strings.TrimSpace(string(out)) == "act"
}
