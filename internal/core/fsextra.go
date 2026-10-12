package core

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The conveniences of the file manager that work through whole folders:
// search, sizes, packing and unpacking.

const (
	searchLimit  = 400
	searchBudget = 8 * time.Second
	sizeBudget   = 10 * time.Second
)

// searchFiles finds what has query in its name, anywhere under dir.
func searchFiles(dir, query string) (FSResult, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return FSResult{}, errors.New("nothing to look for")
	}
	res := FSResult{Path: dir}
	deadline := time.Now().Add(searchBudget)
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fs.SkipDir
		}
		if path == dir {
			return nil
		}
		if time.Now().After(deadline) || len(res.Entries) >= searchLimit {
			res.Truncated = true
			return fs.SkipAll
		}
		name := d.Name()
		// Places that are not files, and the insides of hidden folders.
		if d.IsDir() && (strings.HasPrefix(name, ".") || path == "/proc" || path == "/sys" || path == "/dev" || path == NetDir()) {
			return fs.SkipDir
		}
		if strings.Contains(strings.ToLower(name), query) {
			if info, err := d.Info(); err == nil {
				res.Entries = append(res.Entries, *entryOf(path, info))
			}
		}
		return nil
	})
	return res, nil
}

// properties describes a file or a folder: for a folder, how much is in it.
func properties(path string) (FSResult, error) {
	entry, err := statEntry(path)
	if err != nil {
		return FSResult{}, err
	}
	res := FSResult{Entry: entry}
	res.Free, res.Total = diskFree(path)
	if !entry.Dir {
		return res, nil
	}
	deadline := time.Now().Add(sizeBudget)
	filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fs.SkipDir
		}
		if time.Now().After(deadline) {
			res.Truncated = true
			return fs.SkipAll
		}
		if p == NetDir() {
			return fs.SkipDir
		}
		if !d.IsDir() {
			res.Count++
			if info, err := d.Info(); err == nil && info.Mode().IsRegular() {
				entry.Size += info.Size()
			}
		}
		return nil
	})
	return res, nil
}

// packZip puts files and folders into a new zip archive in dir.
func packZip(paths []string, dir string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("nothing to pack")
	}
	name := "Архив"
	if len(paths) == 1 {
		name = strings.TrimSuffix(filepath.Base(paths[0]), filepath.Ext(paths[0]))
	}
	target := uniquePath(filepath.Join(dir, name+".zip"))
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", plain(err)
	}
	zw := zip.NewWriter(out)
	fail := func(err error) (string, error) {
		zw.Close()
		out.Close()
		os.Remove(target)
		return "", plain(err)
	}
	for _, root := range paths {
		root = filepath.Clean(root)
		base := filepath.Dir(root)
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if path == target {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(base, path)
			header, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(rel)
			if d.IsDir() {
				header.Name += "/"
				_, err := zw.CreateHeader(header)
				return err
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			header.Method = zip.Deflate
			w, err := zw.CreateHeader(header)
			if err != nil {
				return err
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(w, f)
			return err
		})
		if err != nil {
			return fail(err)
		}
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	return target, plain(out.Close())
}

// unpack extracts a zip or tar archive into a new folder in dir, named
// after the archive.
func unpack(archive, dir string) (string, error) {
	name := filepath.Base(archive)
	for _, ext := range []string{".tar.gz", ".tgz", ".tar", ".zip"} {
		if strings.HasSuffix(strings.ToLower(name), ext) {
			name = name[:len(name)-len(ext)]
			break
		}
	}
	target := uniquePath(filepath.Join(dir, name))
	if err := os.Mkdir(target, 0o755); err != nil {
		return "", plain(err)
	}
	// place returns where an entry goes, refusing names that climb out.
	place := func(entry string) (string, error) {
		full := filepath.Join(target, filepath.FromSlash(entry))
		if !within(full, target) {
			return "", errors.New("the archive tries to write outside its folder")
		}
		return full, nil
	}
	write := func(full string, mode os.FileMode, r io.Reader) error {
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm()|0o600)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, r)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return err
	}
	err := func() error {
		lower := strings.ToLower(archive)
		if strings.HasSuffix(lower, ".tar") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
			f, err := os.Open(archive)
			if err != nil {
				return err
			}
			defer f.Close()
			var r io.Reader = f
			if !strings.HasSuffix(lower, ".tar") {
				gz, err := gzip.NewReader(f)
				if err != nil {
					return errors.New("this archive is damaged")
				}
				defer gz.Close()
				r = gz
			}
			for tr := tar.NewReader(r); ; {
				h, err := tr.Next()
				if err == io.EOF {
					return nil
				}
				if err != nil {
					return errors.New("this archive is damaged")
				}
				full, err := place(h.Name)
				if err != nil {
					return err
				}
				switch h.Typeflag {
				case tar.TypeDir:
					err = os.MkdirAll(full, 0o755)
				case tar.TypeReg:
					err = write(full, h.FileInfo().Mode(), tr)
				}
				if err != nil {
					return err
				}
			}
		}
		z, err := zip.OpenReader(archive)
		if err != nil {
			return errors.New("this archive cannot be read")
		}
		defer z.Close()
		for _, f := range z.File {
			full, err := place(f.Name)
			if err != nil {
				return err
			}
			if f.FileInfo().IsDir() {
				if err := os.MkdirAll(full, 0o755); err != nil {
					return err
				}
				continue
			}
			if !f.Mode().IsRegular() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = write(full, f.Mode(), rc)
			rc.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}()
	if err != nil {
		os.RemoveAll(target)
		return "", plain(err)
	}
	return target, nil
}

// UIState is what the window remembers between runs: bookmarks, recent
// files, the chosen view. The daemon keeps it and does not look inside.
func (c *Core) UIState() []byte {
	data, err := os.ReadFile(filepath.Join(c.dir, "ui.json"))
	if err != nil || len(data) == 0 {
		return []byte("{}")
	}
	return data
}

// SaveUIState replaces what the window remembers.
func (c *Core) SaveUIState(data []byte) error {
	if len(data) > 256*1024 {
		return errors.New("too much to remember")
	}
	return os.WriteFile(filepath.Join(c.dir, "ui.json"), data, 0o600)
}
