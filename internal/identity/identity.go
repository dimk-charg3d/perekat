// Package identity stores what makes this installation a distinct device.
package identity

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Device is the persistent identity of this machine.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Dir returns the per-user configuration directory of perekat.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "perekat"), nil
}

// LoadOrCreate reads the identity from dir, generating and saving one on first run.
func LoadOrCreate(dir string) (Device, error) {
	path := filepath.Join(dir, "identity.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var d Device
		if err := json.Unmarshal(data, &d); err != nil {
			return Device{}, fmt.Errorf("parse %s: %w", path, err)
		}
		return d, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Device{}, err
	}

	name, err := os.Hostname()
	if err != nil || name == "" {
		name = "perekat"
	}
	d := Device{ID: newID(), Name: name, Type: "desktop"}
	data, err = json.MarshalIndent(d, "", "  ")
	if err != nil {
		return Device{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Device{}, err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Device{}, err
	}
	return d, nil
}

// newID returns a random UUIDv4 with underscores, the form protocol version 8 expects.
func newID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x_%x_%x_%x_%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
