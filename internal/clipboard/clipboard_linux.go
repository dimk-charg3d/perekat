//go:build linux && !phone

// Package clipboard watches and sets the text clipboard.
package clipboard

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// The Wayland clipboard is reached through wl-clipboard, which works on any
// compositor with the data-control protocol (Hyprland, Sway, KDE).

// Watch calls onChange with the clipboard text now and after every change,
// until ctx is cancelled. Entries a password manager marks as secret and
// entries without text are skipped.
func Watch(ctx context.Context, onChange func(string)) error {
	cmd := exec.CommandContext(ctx, "wl-paste", "--watch", "echo")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("clipboard needs the wl-clipboard package: %w", err)
	}
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		if text, ok := read(); ok {
			onChange(text)
		} else if OnImage != nil {
			if png := readImage(); png != nil {
				OnImage(png)
			}
		}
	}
	return cmd.Wait()
}

func read() (string, bool) {
	types, err := exec.Command("wl-paste", "--list-types").Output()
	if err != nil {
		return "", false
	}
	hasText := false
	for _, t := range strings.Split(string(types), "\n") {
		switch {
		case t == "x-kde-passwordManagerHint":
			hint, _ := exec.Command("wl-paste", "--no-newline", "--type", t).Output()
			if strings.TrimSpace(string(hint)) == "secret" {
				return "", false
			}
		case strings.HasPrefix(t, "text/plain"), t == "UTF8_STRING", t == "STRING", t == "TEXT":
			hasText = true
		}
	}
	if !hasText {
		return "", false
	}
	text, err := exec.Command("wl-paste", "--no-newline", "--type", "text").Output()
	if err != nil {
		return "", false
	}
	return string(text), true
}

// OnImage, if set, is called with a copied picture, as PNG, when the
// clipboard holds a picture and no text: a screenshot, say.
var OnImage func(png []byte)

// maxImage keeps a huge picture from being carried around.
const maxImage = 12 << 20

func readImage() []byte {
	types, err := exec.Command("wl-paste", "--list-types").Output()
	if err != nil || !strings.Contains("\n"+string(types), "\nimage/png\n") {
		return nil
	}
	png, err := exec.Command("wl-paste", "--type", "image/png").Output()
	if err != nil || len(png) == 0 || len(png) > maxImage {
		return nil
	}
	return png
}

// SetImage puts a PNG picture into the clipboard.
func SetImage(png []byte) error {
	cmd := exec.Command("wl-copy", "--type", "image/png")
	cmd.Stdin = bytes.NewReader(png)
	return cmd.Run()
}

// Set replaces the clipboard text.
func Set(text string) error {
	cmd := exec.Command("wl-copy", "--type", "text/plain")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
