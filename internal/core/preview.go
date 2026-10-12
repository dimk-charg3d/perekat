package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Previews of office documents. A computer with LibreOffice turns a document
// into a PDF that looks the way the document does; any such computer among
// the paired ones will do, the file is carried to it. Without one the plain
// view of office.go is used.

// Preview tells the interface how to show a file.
type Preview struct {
	// Kind is "pdf" for a PDF to draw, or "html" for a ready page.
	Kind string `json:"kind"`
	// Device and Path say where the PDF is.
	Device string `json:"device,omitempty"`
	Path   string `json:"path,omitempty"`
	HTML   string `json:"html,omitempty"`
	// Exact is false for the plain view, which drops the layout.
	Exact bool `json:"exact"`
}

// previewDir is where converted documents are kept; paired devices may read
// and write here whatever the file access setting says.
func previewDir() string {
	cache, err := os.UserCacheDir()
	if err != nil {
		cache = os.TempDir()
	}
	return filepath.Join(cache, "perekat", "preview")
}

var (
	officeOnce sync.Once
	officePath string
	officeMu   sync.Mutex
)

// office finds LibreOffice; empty if this machine has none.
func office() string {
	officeOnce.Do(func() {
		candidates := []string{"soffice", "libreoffice"}
		if runtime.GOOS == "windows" {
			candidates = append(candidates, `C:\Program Files\LibreOffice\program\soffice.exe`, `C:\Program Files (x86)\LibreOffice\program\soffice.exe`)
		}
		for _, c := range candidates {
			if p, err := exec.LookPath(c); err == nil {
				officePath = p
				break
			}
		}
		// Yesterday's previews are of no use.
		entries, _ := os.ReadDir(previewDir())
		for _, e := range entries {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				os.RemoveAll(filepath.Join(previewDir(), e.Name()))
			}
		}
	})
	return officePath
}

// convertToPDF makes a PDF of a document with LibreOffice and returns where
// it is. The same document is not converted twice.
func convertToPDF(path string) (string, error) {
	program := office()
	if program == "" {
		return "", errors.New("no LibreOffice on this device")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", plain(err)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d", path, info.Size(), info.ModTime().UnixNano())))
	dir := filepath.Join(previewDir(), hex.EncodeToString(sum[:8]))
	pdf := filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+".pdf")
	if _, err := os.Stat(pdf); err == nil {
		return pdf, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	// One at a time, and with a profile of its own, so that a LibreOffice
	// the user has open is not disturbed.
	officeMu.Lock()
	defer officeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	profile := "file://" + filepath.ToSlash(filepath.Join(previewDir(), "profile"))
	if runtime.GOOS == "windows" {
		profile = "file:///" + filepath.ToSlash(filepath.Join(previewDir(), "profile"))
	}
	cmd := exec.CommandContext(ctx, program, "-env:UserInstallation="+profile, "--headless", "--norestore", "--convert-to", "pdf", "--outdir", dir, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("LibreOffice could not convert the file: %s", strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(pdf); err != nil {
		return "", errors.New("LibreOffice could not convert the file")
	}
	return pdf, nil
}

// FSPreview prepares an office document of a device for showing.
func (c *Core) FSPreview(query, path string) (Preview, error) {
	stat, err := c.FS(query, FSRequest{Op: "stat", Path: path})
	if err != nil || stat.Entry == nil {
		return Preview{}, errors.Join(err, errors.New("not found"))
	}

	// Who can convert: the device that has the file, else this one, else
	// any other computer that is connected.
	var converters []string
	for _, d := range c.Devices() {
		if d.Connected && d.Paired && d.CanConvert {
			if d.ID == query {
				converters = append([]string{d.ID}, converters...)
			} else {
				converters = append(converters, d.ID)
			}
		}
	}
	if office() != "" {
		if query == SelfDevice {
			converters = append([]string{SelfDevice}, converters...)
		} else if len(converters) > 0 && converters[0] == query {
			converters = append(converters[:1], append([]string{SelfDevice}, converters[1:]...)...)
		} else {
			converters = append([]string{SelfDevice}, converters...)
		}
	}
	for _, conv := range converters {
		source := path
		if conv != query {
			// Carry the document to the converter first.
			dir, err := c.FS(conv, FSRequest{Op: "preview.dir"})
			if err != nil {
				continue
			}
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%d", query, path, stat.Entry.Size, stat.Entry.Modified)))
			source = joinPath(dir.Path, hex.EncodeToString(sum[:8])+strings.ToLower(filepath.Ext(stat.Entry.Name)))
			have, err := c.FS(conv, FSRequest{Op: "stat", Path: source})
			if err != nil || have.Entry == nil || have.Entry.Size != stat.Entry.Size {
				if _, err := c.FS(conv, FSRequest{Op: "create", Path: source}); err != nil {
					continue
				}
				id, name := c.deviceName(query)
				t := c.startTransferFor(id, name, stat.Entry.Name, stat.Entry.Size, false)
				err = c.copyContent(query, path, conv, source, t)
				c.finishTransfer(t, "", err)
				if err != nil {
					c.FS(conv, FSRequest{Op: "remove", Path: source})
					continue
				}
			}
		}
		made, err := c.FS(conv, FSRequest{Op: "convert", Path: source})
		if err != nil {
			c.log.Warn("document was not converted", "by", conv, "err", err)
			continue
		}
		return Preview{Kind: "pdf", Device: conv, Path: made.Path, Exact: true}, nil
	}

	page, err := c.FS(query, FSRequest{Op: "extract", Path: path})
	if err != nil {
		return Preview{}, err
	}
	return Preview{Kind: "html", HTML: string(page.Data)}, nil
}
