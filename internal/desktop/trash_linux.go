//go:build linux && !phone

package desktop

import (
	"errors"
	"os/exec"
	"strings"
)

// Trash moves a file or a folder to the trash can, from where it can be restored.
func Trash(path string) error {
	out, err := exec.Command("gio", "trash", "--", path).CombinedOutput()
	if errors.Is(err, exec.ErrNotFound) {
		return errors.New("moving to the trash needs the gio tool (package glib2)")
	}
	if err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// Roots lists the top-level locations when there is more than one; on Linux
// everything hangs off a single root.
func Roots() []string { return nil }
