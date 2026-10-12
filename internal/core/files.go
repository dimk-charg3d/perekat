package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
)

// File browsing: a paired perekat device may list, rename, move, trash and
// fetch files here. The whole file system the user can reach is open to it,
// unless file access is switched off in the settings.

const (
	fsTimeout = 20 * time.Second
	// fsMaxEntries keeps the listing of a huge folder to a sane size.
	fsMaxEntries = 5000
	// fsDrives is the path that stands for the list of drives on Windows.
	fsDrives = "/"
)

// FSRequest asks a device to do something with its files.
type FSRequest struct {
	ID string `json:"id"`
	// Op is list, rename, move, delete or download.
	Op   string `json:"op"`
	Path string `json:"path"`
	// To is the new name for rename and the destination folder for move.
	To string `json:"to,omitempty"`
}

// FSEntry is one file or folder in a listing.
type FSEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
}

// FSResult is the answer to an FSRequest.
type FSResult struct {
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
	// Path is the folder that was listed and Parent the one above it, empty
	// at the top.
	Path      string    `json:"path,omitempty"`
	Parent    string    `json:"parent,omitempty"`
	Entries   []FSEntry `json:"entries"`
	Truncated bool      `json:"truncated,omitempty"`
}

// FS performs a file operation on another device and waits for the answer.
func (c *Core) FS(query string, req FSRequest) (FSResult, error) {
	l, err := c.pairedLink(query)
	if err != nil {
		return FSResult{}, err
	}
	if !accepts(l.Peer, protocol.TypeFSRequest) {
		return FSResult{}, errors.New("device does not support file browsing (it is not running perekat)")
	}
	var raw [8]byte
	rand.Read(raw[:])
	req.ID = hex.EncodeToString(raw[:])

	answer := make(chan FSResult, 1)
	c.fsMu.Lock()
	if c.fsPending == nil {
		c.fsPending = map[string]chan FSResult{}
	}
	c.fsPending[req.ID] = answer
	c.fsMu.Unlock()
	defer func() {
		c.fsMu.Lock()
		delete(c.fsPending, req.ID)
		c.fsMu.Unlock()
	}()

	if err := send(l, protocol.TypeFSRequest, req); err != nil {
		return FSResult{}, err
	}
	select {
	case res := <-answer:
		if res.Error != "" {
			return FSResult{}, errors.New(res.Error)
		}
		return res, nil
	case <-time.After(fsTimeout):
		return FSResult{}, errors.New("the device did not answer")
	}
}

func (c *Core) handleFSResponse(p protocol.Packet) {
	var res FSResult
	if err := json.Unmarshal(p.Body, &res); err != nil {
		return
	}
	c.fsMu.Lock()
	answer := c.fsPending[res.ID]
	c.fsMu.Unlock()
	if answer != nil {
		select {
		case answer <- res:
		default:
		}
	}
}

func (c *Core) handleFSRequest(l *link.Link, p protocol.Packet) {
	var req FSRequest
	if err := json.Unmarshal(p.Body, &req); err != nil {
		return
	}
	// Listing a slow disk must not hold up the other packets of this link.
	go func() {
		res, err := c.serveFS(l, req)
		res.ID = req.ID
		if err != nil {
			res = FSResult{ID: req.ID, Error: err.Error()}
		}
		if res.Entries == nil {
			res.Entries = []FSEntry{}
		}
		send(l, protocol.TypeFSResponse, res)
	}()
}

func (c *Core) serveFS(l *link.Link, req FSRequest) (FSResult, error) {
	if !c.fileAccess() {
		return FSResult{}, errors.New("file access is switched off on that device")
	}
	if req.Op == "list" {
		return listDir(req.Path)
	}

	path := filepath.Clean(req.Path)
	if !filepath.IsAbs(path) {
		return FSResult{}, errors.New("not a full path")
	}
	home, _ := os.UserHomeDir()
	if path == filepath.Dir(path) || path == home {
		return FSResult{}, errors.New("this folder cannot be changed")
	}
	if _, err := os.Lstat(path); err != nil {
		return FSResult{}, plain(err)
	}

	switch req.Op {
	case "delete":
		c.log.Info("moving to trash on request", "device", l.Peer.DeviceName, "path", path)
		return FSResult{}, desktop.Trash(path)
	case "rename":
		if req.To == "" || req.To != filepath.Base(req.To) || strings.ContainsAny(req.To, `/\`) {
			return FSResult{}, errors.New("not a valid name")
		}
		return FSResult{}, moveTo(path, filepath.Join(filepath.Dir(path), req.To))
	case "move":
		if !filepath.IsAbs(req.To) {
			return FSResult{}, errors.New("not a full path")
		}
		return FSResult{}, moveTo(path, filepath.Join(filepath.Clean(req.To), filepath.Base(path)))
	case "download":
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return FSResult{}, errors.New("only files can be downloaded, not folders")
		}
		go func() {
			if err := c.sendFile(l, path); err != nil {
				c.log.Warn("requested file was not sent", "device", l.Peer.DeviceName, "path", path, "err", err)
			}
		}()
		return FSResult{}, nil
	}
	return FSResult{}, errors.New("unknown file operation")
}

// moveTo renames without ever replacing something that is already there.
func moveTo(from, to string) error {
	if from == to {
		return nil
	}
	if _, err := os.Lstat(to); err == nil {
		return errors.New("something with this name is already there")
	}
	return plain(os.Rename(from, to))
}

// plain strips the operation and the path from a file error, leaving the
// reason, which is what the person at the other device needs.
func plain(err error) error {
	var pathErr *os.PathError
	var linkErr *os.LinkError
	switch {
	case errors.As(err, &pathErr):
		return pathErr.Err
	case errors.As(err, &linkErr):
		return linkErr.Err
	}
	return err
}

func listDir(path string) (FSResult, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return FSResult{}, err
		}
		path = home
	}
	if roots := desktop.Roots(); path == fsDrives && len(roots) > 0 {
		res := FSResult{Path: fsDrives}
		for _, root := range roots {
			res.Entries = append(res.Entries, FSEntry{Name: root, Path: root, Dir: true})
		}
		return res, nil
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return FSResult{}, errors.New("not a full path")
	}
	dir, err := os.ReadDir(path)
	if err != nil {
		return FSResult{}, plain(err)
	}

	res := FSResult{Path: path, Parent: filepath.Dir(path)}
	if res.Parent == path {
		// The top of a drive leads to the list of drives; the top of the
		// only tree leads nowhere.
		res.Parent = ""
		if len(desktop.Roots()) > 0 {
			res.Parent = fsDrives
		}
	}
	for _, e := range dir {
		if len(res.Entries) == fsMaxEntries {
			res.Truncated = true
			break
		}
		full := filepath.Join(path, e.Name())
		// Stat follows links, so a link to a folder opens like a folder.
		info, err := os.Stat(full)
		if err != nil {
			if info, err = e.Info(); err != nil {
				continue
			}
		}
		entry := FSEntry{Name: e.Name(), Path: full, Dir: info.IsDir(), Modified: info.ModTime().Unix()}
		if !entry.Dir {
			entry.Size = info.Size()
		}
		res.Entries = append(res.Entries, entry)
	}
	sort.SliceStable(res.Entries, func(i, j int) bool {
		a, b := res.Entries[i], res.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return res, nil
}
