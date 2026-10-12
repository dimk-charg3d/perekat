package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/link"
	"github.com/charg3d/perekat/internal/protocol"
)

// File browsing: a paired perekat device may list, read and change files
// here. The whole file system the user can reach is open to it, unless file
// access is switched off in the settings; network folders stay open always.

const (
	fsTimeout = 20 * time.Second
	// fsLongTimeout is for operations that work through whole folders.
	fsLongTimeout = 30 * time.Minute
	// fsMaxEntries keeps the listing of a huge folder to a sane size.
	fsMaxEntries = 5000
	// fsDrives is the path that stands for the list of drives on Windows.
	fsDrives = "/"
	// fsChunk is the most file content one packet carries.
	fsChunk = 512 * 1024

	// SelfDevice names this device where a device is expected, so the same
	// calls work on local files.
	SelfDevice = "self"
)

// FSRequest asks a device to do something with its files.
type FSRequest struct {
	ID string `json:"id"`
	// Op is one of: list, stat, places, mkdir, create, read, write, truncate,
	// send and recv (a whole file over a side connection),
	// rename, move, copy, replace (rename over), extract, archive, convert, preview.dir, search, props, zip, unzip, delete (to the trash), remove (one file or empty
	// folder, for good), purge (a whole tree, for good), thumb,
	// share.create, share.remove.
	Op   string `json:"op"`
	Path string `json:"path"`
	// To is the new name for rename, the destination folder for move and
	// copy, and the name of a new network folder.
	To     string `json:"to,omitempty"`
	Offset int64  `json:"offset,omitempty"`
	// Length is how many bytes to read, or the size of a thumbnail in pixels.
	Length int    `json:"length,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Data   []byte `json:"data,omitempty"`
	// Port names the side connection that carries a whole file, for recv.
	Port int `json:"port,omitempty"`
	// Paths names several files at once, for packing.
	Paths []string `json:"paths,omitempty"`
	// Unique makes mkdir and create pick a free name instead of failing or
	// replacing what is there.
	Unique bool `json:"unique,omitempty"`
}

// FSEntry is one file or folder in a listing.
type FSEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Dir      bool   `json:"dir"`
	Size     int64  `json:"size"`
	Modified int64  `json:"modified"`
	// Kind tells well-known folders apart: home, downloads, pictures and so on.
	Kind string `json:"kind,omitempty"`
}

// FSResult is the answer to an FSRequest.
type FSResult struct {
	ID    string `json:"id"`
	Error string `json:"error,omitempty"`
	// Path is the folder that was listed, or the file or folder that was
	// made, and Parent the folder above a listed one, empty at the top.
	Path      string    `json:"path,omitempty"`
	Parent    string    `json:"parent,omitempty"`
	Entries   []FSEntry `json:"entries"`
	Truncated bool      `json:"truncated,omitempty"`
	Entry     *FSEntry  `json:"entry,omitempty"`
	Data      []byte    `json:"data,omitempty"`
	Shares    []Share   `json:"shares,omitempty"`
	// Port and Size describe the side connection a file is offered on.
	Port int   `json:"port,omitempty"`
	Size int64 `json:"size,omitempty"`
	// Count is how many files a folder holds; Free and Total describe the
	// disk it is on.
	Count int64 `json:"count,omitempty"`
	Free  int64 `json:"free,omitempty"`
	Total int64 `json:"total,omitempty"`
}

// FS performs a file operation on a device, or on this one when query is
// SelfDevice, and waits for the answer.
func (c *Core) FS(query string, req FSRequest) (FSResult, error) {
	if query == SelfDevice {
		res, err := c.serveFS(nil, req, true)
		if res.Entries == nil {
			res.Entries = []FSEntry{}
		}
		return res, err
	}
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
	timeout := fsTimeout
	switch req.Op {
	case "copy", "move", "delete", "purge", "zip", "unzip", "recv":
		timeout = fsLongTimeout
	case "convert":
		timeout = 2 * time.Minute
	}
	select {
	case res := <-answer:
		if res.Error != "" {
			return FSResult{}, errors.New(res.Error)
		}
		return res, nil
	case <-l.Done():
		return FSResult{}, errors.New("device is not connected")
	case <-time.After(timeout):
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
		res, err := c.serveFS(l, req, false)
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

// serveFS carries out a request. trusted marks the requests of this device's
// own user, which no setting restricts.
func (c *Core) serveFS(l *link.Link, req FSRequest, trusted bool) (FSResult, error) {
	from := ""
	if l != nil {
		from = l.Peer.DeviceName
	}
	// File access can be switched off for everybody or for one device.
	open := trusted || (c.fileAccess() && (l == nil || !c.peerSync(l.Peer.DeviceID).FileAccessOff))
	allowed := func(path string) bool {
		return open || c.inShare(path) || within(path, previewDir())
	}
	denied := errors.New("file access is switched off on that device")

	switch req.Op {
	case "places":
		res := FSResult{Shares: c.Shares()}
		if open {
			res.Entries = places()
		}
		return res, nil
	case "preview.dir":
		if office() == "" {
			return FSResult{}, errors.New("no LibreOffice on this device")
		}
		return FSResult{Path: previewDir()}, os.MkdirAll(previewDir(), 0o700)
	case "share.create":
		share, err := c.CreateShare(req.To)
		return FSResult{Path: share.Path}, err
	case "share.remove":
		return FSResult{}, c.RemoveShare(req.Path)
	case "list":
		if req.Path == "" || req.Path == fsDrives {
			if !open {
				return FSResult{}, denied
			}
			return listDir(req.Path)
		}
	}

	path := filepath.Clean(req.Path)
	if !filepath.IsAbs(path) {
		return FSResult{}, errors.New("not a full path")
	}
	if !allowed(path) {
		return FSResult{}, denied
	}

	// What only looks at files.
	switch req.Op {
	case "list":
		return listDir(path)
	case "stat":
		entry, err := statEntry(path)
		return FSResult{Entry: entry}, err
	case "read":
		data, err := readChunk(path, req.Offset, req.Length)
		return FSResult{Data: data}, err
	case "send":
		return c.offerFile(l, path)
	case "thumb":
		data, err := c.thumbnail(path, req.Length)
		return FSResult{Data: data}, err
	case "search":
		return searchFiles(path, req.To)
	case "props":
		return properties(path)
	case "zip":
		for _, p := range req.Paths {
			if !filepath.IsAbs(p) || !allowed(filepath.Clean(p)) {
				return FSResult{}, denied
			}
		}
		made, err := packZip(req.Paths, path)
		return FSResult{Path: made}, err
	case "unzip":
		to := filepath.Clean(req.To)
		if !filepath.IsAbs(to) || !allowed(to) {
			return FSResult{}, denied
		}
		made, err := unpack(path, to)
		return FSResult{Path: made}, err
	case "extract":
		data, err := extractHTML(path)
		return FSResult{Data: data}, err
	case "archive":
		entries, truncated, err := archiveEntries(path)
		return FSResult{Entries: entries, Truncated: truncated}, err
	case "convert":
		pdf, err := convertToPDF(path)
		return FSResult{Path: pdf}, err
	}

	// What makes something new.
	switch req.Op {
	case "mkdir", "create":
		if req.Unique {
			path = uniquePath(path)
		}
		var err error
		if req.Op == "mkdir" {
			err = os.Mkdir(path, 0o755)
		} else {
			flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			if req.Unique {
				flags |= os.O_EXCL
			}
			var f *os.File
			if f, err = os.OpenFile(path, flags, 0o644); err == nil {
				err = f.Close()
			}
		}
		return FSResult{Path: path}, plain(err)
	}

	// The rest changes something that must be there.
	if _, err := os.Lstat(path); err != nil {
		return FSResult{}, plain(err)
	}
	switch req.Op {
	case "write":
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return FSResult{}, plain(err)
		}
		_, err = f.WriteAt(req.Data, req.Offset)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return FSResult{}, plain(err)
	case "truncate":
		return FSResult{}, plain(os.Truncate(path, req.Size))
	case "recv":
		return FSResult{}, c.acceptFile(l, path, req.Port, req.Size)
	case "copy":
		if !filepath.IsAbs(req.To) || !allowed(filepath.Clean(req.To)) {
			return FSResult{}, errors.New("not a folder that can be copied to")
		}
		dst := uniquePath(filepath.Join(filepath.Clean(req.To), filepath.Base(path)))
		if within(dst, path) {
			return FSResult{}, errors.New("a folder cannot be copied into itself")
		}
		if err := copyTree(path, dst); err != nil {
			os.RemoveAll(dst)
			return FSResult{}, plain(err)
		}
		return FSResult{Path: dst}, nil
	}

	if c.protected(path) {
		return FSResult{}, errors.New("this folder cannot be changed")
	}
	switch req.Op {
	case "delete":
		c.log.Info("moving to trash", "asked by", from, "path", path)
		return FSResult{}, desktop.Trash(path)
	case "remove":
		return FSResult{}, plain(os.Remove(path))
	case "purge":
		c.log.Info("deleting", "asked by", from, "path", path)
		return FSResult{}, plain(os.RemoveAll(path))
	case "replace":
		// What a program does when it saves: the new file takes the place
		// of the old one in a single step.
		to := filepath.Clean(req.To)
		if !filepath.IsAbs(to) || !allowed(to) || c.protected(to) {
			return FSResult{}, errors.New("not a place that can be written to")
		}
		return FSResult{}, plain(os.Rename(path, to))
	case "rename":
		if req.To == "" || req.To != filepath.Base(req.To) || strings.ContainsAny(req.To, `/\`) {
			return FSResult{}, errors.New("not a valid name")
		}
		return FSResult{}, moveTo(path, filepath.Join(filepath.Dir(path), req.To))
	case "move":
		if !filepath.IsAbs(req.To) || !allowed(filepath.Clean(req.To)) {
			return FSResult{}, errors.New("not a folder that can be moved to")
		}
		dst := filepath.Join(filepath.Clean(req.To), filepath.Base(path))
		if within(dst, path) {
			return FSResult{}, errors.New("a folder cannot be moved into itself")
		}
		return FSResult{Path: dst}, moveTo(path, dst)
	}
	return FSResult{}, errors.New("unknown file operation")
}

// protected reports whether path is a folder perekat itself relies on.
func (c *Core) protected(path string) bool {
	home, _ := os.UserHomeDir()
	if path == filepath.Dir(path) || path == home || path == sharesRoot() || path == NetDir() {
		return true
	}
	for _, share := range c.Shares() {
		if share.Path == path {
			return true
		}
	}
	return false
}

// within reports whether path is dir or lies inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// uniquePath returns path, or path with a number added when it is taken.
func uniquePath(path string) string {
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	candidate := path
	for i := 1; i < 1000; i++ {
		if _, err := os.Lstat(candidate); err != nil {
			break
		}
		candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	return candidate
}

// moveTo renames without ever replacing something that is already there.
func moveTo(from, to string) error {
	if from == to {
		return nil
	}
	if _, err := os.Lstat(to); err == nil {
		return errors.New("something with this name is already there")
	}
	err := os.Rename(from, to)
	if err == nil {
		return nil
	}
	// Another disk: rename cannot cross over, so copy and drop the original.
	if cerr := copyTree(from, to); cerr != nil {
		os.RemoveAll(to)
		return plain(err)
	}
	return plain(os.RemoveAll(from))
}

// copyTree copies a file, or a folder with everything in it.
func copyTree(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(from)
		if err != nil {
			return err
		}
		return os.Symlink(target, to)
	case info.IsDir():
		if err := os.Mkdir(to, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(from)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyTree(filepath.Join(from, e.Name()), filepath.Join(to, e.Name())); err != nil {
				return err
			}
		}
		return nil
	case !info.Mode().IsRegular():
		return nil
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		os.Chtimes(to, info.ModTime(), info.ModTime())
	}
	return err
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

func readChunk(path string, offset int64, length int) ([]byte, error) {
	if length <= 0 || length > fsChunk {
		length = fsChunk
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, plain(err)
	}
	defer f.Close()
	buf := make([]byte, length)
	n, err := f.ReadAt(buf, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, plain(err)
	}
	return buf[:n], nil
}

func statEntry(path string) (*FSEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		if info, err = os.Lstat(path); err != nil {
			return nil, plain(err)
		}
	}
	return entryOf(path, info), nil
}

func entryOf(path string, info os.FileInfo) *FSEntry {
	entry := &FSEntry{Name: filepath.Base(path), Path: path, Dir: info.IsDir(), Modified: info.ModTime().Unix()}
	if !entry.Dir {
		entry.Size = info.Size()
	}
	return entry
}

// places lists the folders people look for first, as far as they exist.
func places() []FSEntry {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := []FSEntry{{Name: filepath.Base(home), Path: home, Dir: true, Kind: "home"}}
	known := []struct{ kind, names string }{
		{"desktop", "Desktop,Рабочий стол"},
		{"documents", "Documents,Документы"},
		{"downloads", "Downloads,Download,Загрузки"},
		{"pictures", "Pictures,Изображения"},
		{"camera", "DCIM"},
		{"music", "Music,Музыка"},
		{"videos", "Videos,Movies,Видео"},
	}
	for _, k := range known {
		for _, name := range strings.Split(k.names, ",") {
			path := filepath.Join(home, name)
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				out = append(out, FSEntry{Name: name, Path: path, Dir: true, Kind: k.kind})
				break
			}
		}
	}
	if roots := desktop.Roots(); len(roots) > 0 {
		for _, root := range roots {
			out = append(out, FSEntry{Name: root, Path: root, Dir: true, Kind: "root"})
		}
	} else if filepath.Separator == '/' {
		out = append(out, FSEntry{Name: "/", Path: "/", Dir: true, Kind: "root"})
	}
	return out
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
		res.Entries = append(res.Entries, *entryOf(full, info))
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
