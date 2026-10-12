//go:build linux && !phone

package desktop

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SendTarget is a device files can be sent to from the file manager.
type SendTarget struct {
	ID, Name string
}

// SetSendMenu keeps the entries "Send to ... via Perekat" in the menu the
// file manager shows for a file. They are a service menu of KDE's file
// manager, one action per paired device; with no devices the menu goes away.
func SetSendMenu(targets []SendTarget) {
	data, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		data = dir
	} else {
		data = filepath.Join(data, ".local", "share")
	}
	file := filepath.Join(data, "kio", "servicemenus", "perekat.desktop")
	cli := sendProgram()
	if len(targets) == 0 || cli == "" {
		os.Remove(file)
		return
	}
	var b bytes.Buffer
	var actions []string
	for i := range targets {
		actions = append(actions, fmt.Sprintf("send%d", i))
	}
	fmt.Fprintf(&b, "[Desktop Entry]\nType=Service\nMimeType=all/allfiles;\nX-KDE-Priority=TopLevel\nIcon=perekat\nActions=%s;\n", strings.Join(actions, ";"))
	for i, target := range targets {
		// A name is text in a menu and a word of a command line; neither may
		// be broken by what a device calls itself.
		name := strings.NewReplacer("\n", " ", "\r", " ").Replace(target.Name)
		fmt.Fprintf(&b, "\n[Desktop Action send%d]\nName=Отправить на «%s» через Перекат\nName[en]=Send to '%s' via Perekat\nIcon=perekat\nExec=\"%s\" send -notify %s %%F\n",
			i, name, name, cli, target.ID)
	}
	if old, err := os.ReadFile(file); err == nil && bytes.Equal(old, b.Bytes()) {
		return
	}
	if os.MkdirAll(filepath.Dir(file), 0o755) != nil {
		return
	}
	// The file manager runs only service menus that are marked executable.
	os.WriteFile(file, b.Bytes(), 0o755)
	os.Chmod(file, 0o755)
}

// sendProgram finds the command-line program that sends files.
func sendProgram() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if filepath.Base(exe) == "perekat" {
		return exe
	}
	if cli := filepath.Join(filepath.Dir(exe), "perekat"); fileExists(cli) {
		return cli
	}
	return ""
}
