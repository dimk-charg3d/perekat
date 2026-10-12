//go:build linux && !phone

// Package netfs shows the network folders of other devices as folders of
// this machine: a file system mounted under core.NetDir with a folder per
// device and, inside it, a folder per network folder. Nothing is kept here;
// every read and write goes to the device that owns the file.
package netfs

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"

	"github.com/charg3d/perekat/internal/core"
)

// fresh is how long what a device said about a file is taken for true.
const fresh = 2 * time.Second

type netFS struct {
	core *core.Core

	mu    sync.Mutex
	stats map[string]cached
}

type cached struct {
	entry core.FSEntry
	at    time.Time
}

// node is any file or folder of the mounted tree. Where it points is worked
// out from its place in the tree, so renaming a folder needs no bookkeeping.
type node struct {
	fs.Inode
	n *netFS

	mu      sync.Mutex
	block   []byte
	blockAt int64
	blockOf time.Time
}

var (
	_ fs.NodeLookuper  = (*node)(nil)
	_ fs.NodeReaddirer = (*node)(nil)
	_ fs.NodeGetattrer = (*node)(nil)
	_ fs.NodeSetattrer = (*node)(nil)
	_ fs.NodeOpener    = (*node)(nil)
	_ fs.NodeReader    = (*node)(nil)
	_ fs.NodeWriter    = (*node)(nil)
	_ fs.NodeCreater   = (*node)(nil)
	_ fs.NodeMkdirer   = (*node)(nil)
	_ fs.NodeUnlinker  = (*node)(nil)
	_ fs.NodeRmdirer   = (*node)(nil)
	_ fs.NodeRenamer   = (*node)(nil)
)

// Mount shows the network folders under core.NetDir until unmount is called.
// Failing to mount is not fatal: the folders stay reachable in the window.
func Mount(c *core.Core, log *slog.Logger) (unmount func()) {
	dir := core.NetDir()
	// A mount left behind by a daemon that was killed would block this one.
	exec.Command("fusermount3", "-u", "-z", dir).Run()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Warn("network folders are not mounted", "err", err)
		return func() {}
	}
	n := &netFS{core: c, stats: map[string]cached{}}
	timeout := time.Second
	server, err := fs.Mount(dir, &node{n: n}, &fs.Options{
		MountOptions: fuse.MountOptions{FsName: "perekat", Name: "perekat", MaxWrite: 256 * 1024},
		EntryTimeout: &timeout,
		AttrTimeout:  &timeout,
		UID:          uint32(os.Getuid()),
		GID:          uint32(os.Getgid()),
	})
	if err != nil {
		log.Warn("network folders are not mounted", "dir", dir, "err", err)
		return func() {}
	}
	log.Info("network folders mounted", "dir", dir)
	return func() { server.Unmount() }
}

// deviceNames gives every connected device that offers network folders a
// folder name, telling apart devices that share a name.
func (n *netFS) deviceNames() map[string]core.DeviceInfo {
	out := map[string]core.DeviceInfo{}
	for _, d := range n.core.Devices() {
		if !d.Connected || !d.Paired || len(d.Shares) == 0 {
			continue
		}
		name := strings.ReplaceAll(d.Name, "/", "_")
		if _, taken := out[name]; taken {
			name += " (" + d.ID[:4] + ")"
		}
		out[name] = d
	}
	return out
}

// where resolves a node to the device and the path it stands for. depth is
// 0 for the top, 1 for a device, 2 and more for what lives on the device.
func (x *node) where(extra ...string) (device, remote string, depth int, ok bool) {
	parts := []string{}
	if p := x.Path(nil); p != "" {
		parts = strings.Split(p, "/")
	}
	parts = append(parts, extra...)
	if len(parts) == 0 {
		return "", "", 0, true
	}
	d, found := x.n.deviceNames()[parts[0]]
	if !found {
		return "", "", 0, false
	}
	if len(parts) == 1 {
		return d.ID, "", 1, true
	}
	for _, share := range d.Shares {
		if share.Name == parts[1] {
			remote = share.Path
			for _, part := range parts[2:] {
				sep := "/"
				if strings.Contains(remote, `\`) && !strings.Contains(remote, "/") {
					sep = `\`
				}
				remote = strings.TrimRight(remote, sep) + sep + part
			}
			return d.ID, remote, len(parts), true
		}
	}
	return "", "", 0, false
}

func (n *netFS) do(device string, req core.FSRequest) (core.FSResult, syscall.Errno) {
	res, err := n.core.FS(device, req)
	if err == nil {
		return res, 0
	}
	text := err.Error()
	for part, errno := range map[string]syscall.Errno{
		"no such file":        syscall.ENOENT,
		"cannot find":         syscall.ENOENT,
		"exists":              syscall.EEXIST,
		"already there":       syscall.EEXIST,
		"permission denied":   syscall.EACCES,
		"switched off":        syscall.EACCES,
		"cannot be changed":   syscall.EACCES,
		"not empty":           syscall.ENOTEMPTY,
		"is a directory":      syscall.EISDIR,
		"not a directory":     syscall.ENOTDIR,
		"no space":            syscall.ENOSPC,
		"read-only":           syscall.EROFS,
		"not connected":       syscall.EHOSTDOWN,
		"did not answer":      syscall.ETIMEDOUT,
		"file name too long":  syscall.ENAMETOOLONG,
		"cross-device":        syscall.EXDEV,
		"operation not perm":  syscall.EPERM,
		"too many open files": syscall.EMFILE,
	} {
		if strings.Contains(text, part) {
			return res, errno
		}
	}
	return res, syscall.EIO
}

func (n *netFS) remember(device string, e core.FSEntry) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.stats) > 20000 {
		n.stats = map[string]cached{}
	}
	n.stats[device+"\x00"+e.Path] = cached{e, time.Now()}
}

// changed forgets what is known, after something was changed through here.
func (n *netFS) changed() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stats = map[string]cached{}
}

func (n *netFS) stat(device, remote string) (core.FSEntry, syscall.Errno) {
	n.mu.Lock()
	c, ok := n.stats[device+"\x00"+remote]
	n.mu.Unlock()
	if ok && time.Since(c.at) < fresh {
		return c.entry, 0
	}
	res, errno := n.do(device, core.FSRequest{Op: "stat", Path: remote})
	if errno != 0 {
		return core.FSEntry{}, errno
	}
	if res.Entry == nil {
		return core.FSEntry{}, syscall.EIO
	}
	n.remember(device, *res.Entry)
	return *res.Entry, 0
}

func fill(out *fuse.Attr, e core.FSEntry) {
	out.Mode = 0o644
	out.Nlink = 1
	if e.Dir {
		out.Mode = 0o755
		out.Nlink = 2
	}
	out.Size = uint64(e.Size)
	out.Blocks = (out.Size + 511) / 512
	out.Mtime, out.Ctime, out.Atime = uint64(e.Modified), uint64(e.Modified), uint64(e.Modified)
	out.Uid, out.Gid = uint32(os.Getuid()), uint32(os.Getgid())
}

func mode(dir bool) uint32 {
	if dir {
		return fuse.S_IFDIR
	}
	return fuse.S_IFREG
}

// entry describes what a name inside this folder stands for.
func (x *node) entry(name string) (core.FSEntry, syscall.Errno) {
	device, remote, depth, ok := x.where(name)
	if !ok {
		return core.FSEntry{}, syscall.ENOENT
	}
	if depth <= 2 && remote == "" {
		return core.FSEntry{Name: name, Dir: true}, 0
	}
	return x.n.stat(device, remote)
}

func (x *node) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	e, errno := x.entry(name)
	if errno != 0 {
		return nil, errno
	}
	fill(&out.Attr, e)
	return x.NewInode(ctx, &node{n: x.n}, fs.StableAttr{Mode: mode(e.Dir)}), 0
}

func (x *node) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	device, remote, _, ok := x.where()
	if !ok {
		return syscall.ENOENT
	}
	e := core.FSEntry{Dir: true}
	if remote != "" {
		var errno syscall.Errno
		if e, errno = x.n.stat(device, remote); errno != 0 {
			return errno
		}
	}
	fill(&out.Attr, e)
	return 0
}

func (x *node) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	device, remote, depth, ok := x.where()
	if !ok {
		return nil, syscall.ENOENT
	}
	var list []fuse.DirEntry
	switch depth {
	case 0:
		for name := range x.n.deviceNames() {
			list = append(list, fuse.DirEntry{Name: name, Mode: fuse.S_IFDIR})
		}
	case 1:
		for _, d := range x.n.deviceNames() {
			if d.ID == device {
				for _, share := range d.Shares {
					list = append(list, fuse.DirEntry{Name: share.Name, Mode: fuse.S_IFDIR})
				}
			}
		}
	default:
		res, errno := x.n.do(device, core.FSRequest{Op: "list", Path: remote})
		if errno != 0 {
			return nil, errno
		}
		for _, e := range res.Entries {
			x.n.remember(device, e)
			list = append(list, fuse.DirEntry{Name: e.Name, Mode: mode(e.Dir)})
		}
	}
	return fs.NewListDirStream(list), 0
}

func (x *node) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	device, remote, depth, ok := x.where()
	if !ok || depth < 3 {
		return nil, 0, syscall.EISDIR
	}
	if flags&syscall.O_TRUNC != 0 {
		if _, errno := x.n.do(device, core.FSRequest{Op: "truncate", Path: remote}); errno != 0 {
			return nil, 0, errno
		}
		x.forget()
	}
	// The device may change the file at any time, so the kernel keeps no
	// copy of its content.
	return nil, fuse.FOPEN_DIRECT_IO, 0
}

// forget drops what this node and the file system remember about the file.
func (x *node) forget() {
	x.mu.Lock()
	x.block = nil
	x.mu.Unlock()
	x.n.changed()
}

func (x *node) Read(ctx context.Context, f fs.FileHandle, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	device, remote, _, ok := x.where()
	if !ok {
		return nil, syscall.ENOENT
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	// Programs read a file in small steps; one larger piece serves many.
	if x.block == nil || off < x.blockAt || off >= x.blockAt+int64(len(x.block)) || time.Since(x.blockOf) > fresh {
		res, errno := x.n.do(device, core.FSRequest{Op: "read", Path: remote, Offset: off})
		if errno != 0 {
			return nil, errno
		}
		x.block, x.blockAt, x.blockOf = res.Data, off, time.Now()
	}
	start := int(off - x.blockAt)
	if start >= len(x.block) {
		return fuse.ReadResultData(nil), 0
	}
	return fuse.ReadResultData(x.block[start:min(len(x.block), start+len(dest))]), 0
}

func (x *node) Write(ctx context.Context, f fs.FileHandle, data []byte, off int64) (uint32, syscall.Errno) {
	device, remote, _, ok := x.where()
	if !ok {
		return 0, syscall.ENOENT
	}
	if _, errno := x.n.do(device, core.FSRequest{Op: "write", Path: remote, Offset: off, Data: data}); errno != 0 {
		return 0, errno
	}
	x.forget()
	return uint32(len(data)), 0
}

func (x *node) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	device, remote, depth, ok := x.where()
	if !ok {
		return syscall.ENOENT
	}
	// Only the size can be set; owners, modes and times are the device's.
	if size, has := in.GetSize(); has && depth >= 3 {
		if _, errno := x.n.do(device, core.FSRequest{Op: "truncate", Path: remote, Size: int64(size)}); errno != 0 {
			return errno
		}
		x.forget()
	}
	return x.Getattr(ctx, f, out)
}

// make creates a file or a folder inside this one.
func (x *node) make(ctx context.Context, op, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	device, remote, depth, ok := x.where(name)
	if !ok || depth < 3 {
		return nil, syscall.EACCES
	}
	if _, errno := x.n.do(device, core.FSRequest{Op: op, Path: remote}); errno != 0 {
		return nil, errno
	}
	x.n.changed()
	e, errno := x.n.stat(device, remote)
	if errno != 0 {
		return nil, errno
	}
	fill(&out.Attr, e)
	return x.NewInode(ctx, &node{n: x.n}, fs.StableAttr{Mode: mode(e.Dir)}), 0
}

func (x *node) Create(ctx context.Context, name string, flags uint32, perm uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	inode, errno := x.make(ctx, "create", name, out)
	return inode, nil, fuse.FOPEN_DIRECT_IO, errno
}

func (x *node) Mkdir(ctx context.Context, name string, perm uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return x.make(ctx, "mkdir", name, out)
}

func (x *node) remove(name string) syscall.Errno {
	device, remote, depth, ok := x.where(name)
	if !ok || depth < 3 {
		return syscall.EACCES
	}
	_, errno := x.n.do(device, core.FSRequest{Op: "remove", Path: remote})
	x.n.changed()
	return errno
}

func (x *node) Unlink(ctx context.Context, name string) syscall.Errno { return x.remove(name) }

func (x *node) Rmdir(ctx context.Context, name string) syscall.Errno { return x.remove(name) }

func (x *node) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	device, from, depth, ok := x.where(name)
	target, isNode := newParent.(*node)
	if !ok || !isNode || depth < 3 {
		return syscall.EACCES
	}
	toDevice, to, toDepth, ok := target.where(newName)
	if !ok || toDepth < 3 {
		return syscall.EACCES
	}
	if toDevice != device {
		return syscall.EXDEV
	}
	_, errno := x.n.do(device, core.FSRequest{Op: "replace", Path: from, To: to})
	x.n.changed()
	return errno
}
