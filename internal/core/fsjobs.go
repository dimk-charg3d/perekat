package core

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/charg3d/perekat/internal/desktop"
	"github.com/charg3d/perekat/internal/link"
)

// Work on files that takes a while runs in the background and shows among
// the transfers: copying between devices, downloading, uploading.

// deviceName returns the id and the name of a device for the transfer list.
func (c *Core) deviceName(query string) (id, name string) {
	if query == SelfDevice {
		return c.links.Self.DeviceID, c.links.Self.DeviceName
	}
	if l, err := c.pairedLink(query); err == nil {
		return l.Peer.DeviceID, l.Peer.DeviceName
	}
	return query, query
}

// joinPath joins a folder and a name the way the device that owns the folder
// writes its paths.
func joinPath(dir, name string) string {
	sep := "/"
	if strings.Contains(dir, `\`) && !strings.Contains(dir, "/") {
		sep = `\`
	}
	return strings.TrimRight(dir, sep) + sep + name
}

// FSPaste copies files and folders into a folder, on the same device or on
// another one, or moves them there. It returns at once; the work shows among
// the transfers. An empty folder on this device means the downloads folder.
func (c *Core) FSPaste(src string, paths []string, dst, dstDir string, move bool) error {
	if len(paths) == 0 {
		return errors.New("nothing to copy")
	}
	if dst == SelfDevice && dstDir == "" {
		dstDir = c.downloadDir()
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
	}
	if dstDir == "" {
		return errors.New("no folder to copy to")
	}
	if _, err := c.FS(dst, FSRequest{Op: "stat", Path: dstDir}); err != nil {
		return err
	}
	// The list is shown under the device that is not this one.
	shown := dst
	if dst == SelfDevice {
		shown = src
	}
	id, name := c.deviceName(shown)
	go func() {
		for _, path := range paths {
			c.pasteOne(src, path, dst, dstDir, move, id, name)
		}
	}()
	return nil
}

func (c *Core) pasteOne(src, path, dst, dstDir string, move bool, id, device string) {
	title := filepath.Base(strings.ReplaceAll(path, `\`, "/"))
	if src == dst {
		// The device does it itself, nothing crosses the network.
		t := c.startTransferFor(id, device, title, 0, false)
		op := "copy"
		if move {
			op = "move"
		}
		_, err := c.FS(src, FSRequest{Op: op, Path: path, To: dstDir})
		c.finishTransfer(t, "", err)
		return
	}

	stat, err := c.FS(src, FSRequest{Op: "stat", Path: path})
	if err != nil || stat.Entry == nil {
		t := c.startTransferFor(id, device, title, 0, dst == SelfDevice)
		c.finishTransfer(t, "", errors.Join(err, errors.New("not found")))
		return
	}
	// A folder is walked first, so the progress has a total to show.
	type item struct {
		entry FSEntry
		rel   []string
	}
	var items []item
	var total int64
	var walk func(e FSEntry, rel []string) error
	walk = func(e FSEntry, rel []string) error {
		items = append(items, item{e, rel})
		if !e.Dir {
			total += e.Size
			return nil
		}
		list, err := c.FS(src, FSRequest{Op: "list", Path: e.Path})
		if err != nil {
			return err
		}
		for _, child := range list.Entries {
			if err := walk(child, append(append([]string{}, rel...), child.Name)); err != nil {
				return err
			}
		}
		return nil
	}
	t := c.startTransferFor(id, device, title, 0, dst == SelfDevice)
	if err := walk(*stat.Entry, nil); err != nil {
		c.finishTransfer(t, "", err)
		return
	}
	c.transferMu.Lock()
	t.Size = total
	c.transferMu.Unlock()

	// The top item takes a free name; what is inside keeps its names.
	top := ""
	err = func() error {
		for _, it := range items {
			target := top
			for _, part := range it.rel {
				target = joinPath(target, part)
			}
			op := "create"
			if it.entry.Dir {
				op = "mkdir"
			}
			req := FSRequest{Op: op, Path: target, Unique: top == ""}
			if top == "" {
				req.Path = joinPath(dstDir, title)
			}
			made, err := c.FS(dst, req)
			if err != nil {
				return err
			}
			if top == "" {
				top = made.Path
			}
			if it.entry.Dir {
				continue
			}
			if err := c.copyContent(src, it.entry.Path, dst, made.Path, t); err != nil {
				return err
			}
		}
		return nil
	}()
	if err != nil && top != "" {
		c.FS(dst, FSRequest{Op: "purge", Path: top})
	}
	if err == nil && move {
		_, err = c.FS(src, FSRequest{Op: "purge", Path: path})
	}
	shownPath := ""
	if dst == SelfDevice {
		shownPath = top
	}
	c.finishTransfer(t, shownPath, err)
	if err == nil && dst == SelfDevice && src != SelfDevice {
		desktop.Notify("Файл от "+device, top)
	}
}

// errNoStream means the other device is too old to send whole files over a
// side connection, and the content has to go piece by piece.
var errNoStream = errors.New("no stream")

// offerFile opens a side connection for a file and says where it is.
func (c *Core) offerFile(l *link.Link, path string) (FSResult, error) {
	if l == nil {
		return FSResult{}, errors.New("unknown file operation")
	}
	f, err := os.Open(path)
	if err != nil {
		return FSResult{}, plain(err)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return FSResult{}, errors.New("not a file")
	}
	id, wait, _, err := l.Payload.Offer()
	if err != nil {
		f.Close()
		return FSResult{}, err
	}
	go func() {
		defer f.Close()
		if err := c.links.SendPayload(l, wait, f); err != nil {
			c.log.Warn("file was not sent", "device", l.Peer.DeviceName, "path", path, "err", err)
		}
	}()
	return FSResult{Port: id, Size: info.Size()}, nil
}

// acceptFile fills a file from a side connection the other device offers.
func (c *Core) acceptFile(l *link.Link, path string, port int, size int64) error {
	if l == nil {
		return errors.New("unknown file operation")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return plain(err)
	}
	err = c.links.FetchPayload(context.Background(), l, port, f, size)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// streamContent moves a whole file between this device and another one
// over a side connection, which is many times faster than piece by piece.
func (c *Core) streamContent(src, from, dst, to string, t *transfer) error {
	switch {
	case dst == SelfDevice && src != SelfDevice:
		l, err := c.pairedLink(src)
		if err != nil {
			return err
		}
		offer, err := c.FS(src, FSRequest{Op: "send", Path: from})
		if err != nil {
			if strings.Contains(err.Error(), "unknown file operation") {
				return errNoStream
			}
			return err
		}
		f, err := os.OpenFile(to, os.O_WRONLY|os.O_TRUNC, 0)
		if err != nil {
			return err
		}
		err = c.links.FetchPayload(context.Background(), l, offer.Port, counter{t: t, w: f}, offer.Size)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	case src == SelfDevice && dst != SelfDevice:
		l, err := c.pairedLink(dst)
		if err != nil {
			return err
		}
		f, err := os.Open(from)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		id, wait, cancel, err := l.Payload.Offer()
		if err != nil {
			return err
		}
		sent := make(chan error, 1)
		started := make(chan struct{})
		go func() {
			select {
			case <-started:
				sent <- c.links.SendPayload(l, wait, counter{t: t, r: f})
			case <-l.Done():
				cancel()
				sent <- errors.New("device is not connected")
			}
		}()
		close(started)
		_, err = c.FS(dst, FSRequest{Op: "recv", Path: to, Port: id, Size: info.Size()})
		if err != nil {
			cancel()
		}
		if err != nil && strings.Contains(err.Error(), "unknown file operation") {
			<-sent
			t.done.Store(0)
			return errNoStream
		}
		if serr := <-sent; err == nil {
			err = serr
		}
		return err
	}
	return errNoStream
}

func (c *Core) copyContent(src, from, dst, to string, t *transfer) error {
	if err := c.streamContent(src, from, dst, to, t); !errors.Is(err, errNoStream) {
		return err
	}
	for offset := int64(0); ; {
		chunk, err := c.FS(src, FSRequest{Op: "read", Path: from, Offset: offset, Length: fsChunk})
		if err != nil {
			return err
		}
		if len(chunk.Data) == 0 {
			return nil
		}
		if _, err := c.FS(dst, FSRequest{Op: "write", Path: to, Offset: offset, Data: chunk.Data}); err != nil {
			return err
		}
		offset += int64(len(chunk.Data))
		t.done.Add(int64(len(chunk.Data)))
	}
}

// FSUpload writes what r delivers into a new file in a folder of a device.
func (c *Core) FSUpload(dst, dir, name string, size int64, r io.Reader) error {
	id, device := c.deviceName(dst)
	t := c.startTransferFor(id, device, safeName(name), size, false)
	made, err := c.FS(dst, FSRequest{Op: "create", Path: joinPath(dir, safeName(name)), Unique: true})
	if err == nil {
		buf := make([]byte, fsChunk)
		for offset := int64(0); err == nil; {
			n, rerr := io.ReadFull(r, buf)
			if n > 0 {
				_, err = c.FS(dst, FSRequest{Op: "write", Path: made.Path, Offset: offset, Data: buf[:n]})
				offset += int64(n)
				t.done.Add(int64(n))
			}
			if rerr != nil {
				if !errors.Is(rerr, io.EOF) && !errors.Is(rerr, io.ErrUnexpectedEOF) && err == nil {
					err = rerr
				}
				break
			}
		}
		if err != nil {
			c.FS(dst, FSRequest{Op: "remove", Path: made.Path})
		}
	}
	c.finishTransfer(t, "", err)
	return err
}

// FSOpen opens a file with the program this machine uses for it; a file of
// another device is fetched into the cache first.
func (c *Core) FSOpen(query, path string) error {
	if query == SelfDevice {
		return desktop.Open(path)
	}
	stat, err := c.FS(query, FSRequest{Op: "stat", Path: path})
	if err != nil {
		return err
	}
	if stat.Entry == nil || stat.Entry.Dir {
		return errors.New("only files can be opened")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	id, device := c.deviceName(query)
	dir := filepath.Join(cache, "perekat", "open", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	local := filepath.Join(dir, safeName(stat.Entry.Name))
	go func() {
		t := c.startTransferFor(id, device, stat.Entry.Name, stat.Entry.Size, true)
		_, err := c.FS(SelfDevice, FSRequest{Op: "create", Path: local})
		if err == nil {
			err = c.copyContent(query, path, SelfDevice, local, t)
		}
		c.finishTransfer(t, local, err)
		if err == nil {
			desktop.Open(local)
		}
	}()
	return nil
}

// remoteFile reads a file of a device as if it were here.
type remoteFile struct {
	c      *Core
	device string
	path   string
	size   int64
	offset int64
	// block is the last piece fetched; reading on from it costs no request.
	block   []byte
	blockAt int64
}

// FSReader opens a file of a device for reading at any position.
func (c *Core) FSReader(query, path string) (io.ReadSeeker, FSEntry, error) {
	stat, err := c.FS(query, FSRequest{Op: "stat", Path: path})
	if err != nil {
		return nil, FSEntry{}, err
	}
	if stat.Entry == nil || stat.Entry.Dir {
		return nil, FSEntry{}, errors.New("not a file")
	}
	return &remoteFile{c: c, device: query, path: path, size: stat.Entry.Size}, *stat.Entry, nil
}

func (f *remoteFile) Read(p []byte) (int, error) {
	if f.offset >= f.size {
		return 0, io.EOF
	}
	if f.offset < f.blockAt || f.offset >= f.blockAt+int64(len(f.block)) {
		res, err := f.c.FS(f.device, FSRequest{Op: "read", Path: f.path, Offset: f.offset, Length: fsChunk})
		if err != nil {
			return 0, err
		}
		if len(res.Data) == 0 {
			return 0, io.EOF
		}
		f.block, f.blockAt = res.Data, f.offset
	}
	n := copy(p, f.block[f.offset-f.blockAt:])
	f.offset += int64(n)
	return n, nil
}

func (f *remoteFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += f.offset
	case io.SeekEnd:
		offset += f.size
	}
	if offset < 0 {
		return 0, errors.New("negative position")
	}
	f.offset = offset
	return offset, nil
}
