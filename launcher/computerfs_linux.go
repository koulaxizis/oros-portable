//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The Linux side of computerfs.go: mounts, disk space, the freedesktop
// Trash and the disk helper.

func isMountpoint(dir string) bool {
	b, _ := os.ReadFile("/proc/self/mounts")
	for _, line := range strings.Split(string(b), "\n") {
		if f := strings.Fields(line); len(f) > 1 && unescapeMount(f[1]) == dir {
			return true
		}
	}
	return false
}

// unescapeMount undoes the octal escapes of /proc/mounts (\040 = space).
func unescapeMount(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func diskSpace(dir string) (free, total int64, ro bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), int64(st.Blocks) * int64(st.Bsize), st.Flags&stRdonly != 0
}

const stRdonly = 1 // ST_RDONLY

func (c *computerFS) disks() []rootInfo {
	out := []rootInfo{}
	ents, err := os.ReadDir(c.media)
	if err != nil {
		return out
	}
	for _, e := range ents {
		dir := filepath.Join(c.media, e.Name())
		if strings.HasPrefix(e.Name(), ".") || !e.IsDir() || !c.mounted(dir) {
			continue
		}
		out = append(out, c.rootInfo("/disks/"+e.Name(), e.Name(), dir))
	}
	return out
}

// ---------- Trash (freedesktop.org Trash specification 1.0) ----------

// trashDir is where p goes: the home Trash for the home folder, the disk's
// own .Trash-<uid> for a disk. top is what Path= in .trashinfo is relative
// to ("" = absolute).
func (c *computerFS) trashDir(vroot string) (dir, top string) {
	if vroot == "/home" {
		d := os.Getenv("XDG_DATA_HOME")
		home := c.homeDir()
		if d == "" || !strings.HasPrefix(d, home+"/") || home != c.home {
			d = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(d, "Trash"), ""
	}
	root := filepath.Join(c.media, strings.TrimPrefix(vroot, "/disks/"))
	return filepath.Join(root, fmt.Sprintf(".Trash-%d", c.uid)), root
}

// escapeTrashPath percent-encodes a path for Path= in .trashinfo, as
// RFC 2396 asks: everything but unreserved characters and "/".
func escapeTrashPath(p string) string {
	var b strings.Builder
	for _, by := range []byte(p) {
		switch {
		case by >= 'A' && by <= 'Z', by >= 'a' && by <= 'z', by >= '0' && by <= '9',
			strings.IndexByte("-._~/", by) >= 0:
			b.WriteByte(by)
		default:
			fmt.Fprintf(&b, "%%%02X", by)
		}
	}
	return b.String()
}

func unescapeTrashPath(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
				b.WriteByte(byte(n))
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func (c *computerFS) trash(p place) (string, error) {
	dir, top := c.trashDir(p.vroot)
	if err := os.MkdirAll(filepath.Join(dir, "files"), 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "info"), 0o700); err != nil {
		return "", err
	}
	orig := p.real
	if top != "" {
		orig = strings.TrimPrefix(strings.TrimPrefix(p.real, top), "/")
	}
	base := filepath.Base(p.real)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", escapeTrashPath(orig), time.Now().Format("2006-01-02T15:04:05"))
	for i := 1; i < 10000; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s.%d%s", stem, i, ext)
		}
		ip := filepath.Join(dir, "info", name+".trashinfo")
		f, err := os.OpenFile(ip, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.WriteString(info)
		cerr := f.Close()
		if werr == nil {
			werr = cerr
		}
		fp := filepath.Join(dir, "files", name)
		if werr == nil {
			if _, err := os.Lstat(fp); err == nil {
				_ = os.Remove(ip)
				continue
			}
			werr = os.Rename(p.real, fp)
		}
		if werr != nil {
			_ = os.Remove(ip)
			return "", werr
		}
		return name, nil
	}
	return "", fsErr(http.StatusConflict, "exists", "the Trash has too many items of this name")
}

func (c *computerFS) trashRoots() []string {
	roots := []string{"/home"}
	for _, d := range c.disks() {
		roots = append(roots, d.Path)
	}
	return roots
}

func (c *computerFS) trashList() []trashItem {
	out := []trashItem{}
	for _, vroot := range c.trashRoots() {
		dir, top := c.trashDir(vroot)
		ents, err := os.ReadDir(filepath.Join(dir, "info"))
		if err != nil {
			continue
		}
		for _, e := range ents {
			id, ok := strings.CutSuffix(e.Name(), ".trashinfo")
			if !ok {
				continue
			}
			it, err := c.trashItem(vroot, dir, top, id)
			if err == nil {
				out = append(out, it)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

func (c *computerFS) trashItem(vroot, dir, top, id string) (trashItem, error) {
	if id == "" || strings.ContainsAny(id, "/\x00") || id == "." || id == ".." {
		return trashItem{}, errBadPath
	}
	b, err := os.ReadFile(filepath.Join(dir, "info", id+".trashinfo"))
	if err != nil {
		return trashItem{}, err
	}
	fi, err := os.Lstat(filepath.Join(dir, "files", id))
	if err != nil {
		return trashItem{}, err
	}
	it := trashItem{Root: vroot, ID: id, Dir: fi.IsDir(), MTime: fi.ModTime().UnixMilli()}
	if !it.Dir {
		it.Size = fi.Size()
	}
	var orig string
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "Path="); ok {
			orig = unescapeTrashPath(strings.TrimSpace(v))
		} else if v, ok := strings.CutPrefix(line, "DeletionDate="); ok {
			it.At = strings.TrimSpace(v)
		}
	}
	it.Name = filepath.Base(orig)
	if it.Name == "." || it.Name == "/" || orig == "" {
		it.Name = id
	}
	if top != "" && !strings.HasPrefix(orig, "/") {
		it.From = vroot + "/" + orig
	} else if home := c.homeDir(); orig == home || strings.HasPrefix(orig, home+"/") {
		it.From = "/home" + strings.TrimPrefix(orig, home)
	}
	return it, nil
}

func (c *computerFS) trashRoot(vroot string) (dir, top string, err error) {
	if vroot != "/home" {
		p, err := c.resolve(vroot)
		if err != nil || p.vpath != p.vroot || !strings.HasPrefix(vroot, "/disks/") {
			return "", "", errNotFound
		}
	}
	dir, top = c.trashDir(vroot)
	return dir, top, nil
}

func (c *computerFS) restore(vroot, id string) (string, error) {
	dir, top, err := c.trashRoot(vroot)
	if err != nil {
		return "", err
	}
	it, err := c.trashItem(vroot, dir, top, id)
	if err != nil {
		return "", err
	}
	if it.From == "" {
		return "", fsErr(http.StatusConflict, "nowhere", "this item came from outside orOS's places")
	}
	p, err := c.writable(it.From)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(p.real); err == nil {
		return "", fs.ErrExist
	}
	if err := os.MkdirAll(filepath.Dir(p.real), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(filepath.Join(dir, "files", id), p.real); err != nil {
		return "", err
	}
	_ = os.Remove(filepath.Join(dir, "info", id+".trashinfo"))
	return it.From, nil
}

// purge deletes one item from a Trash for good (Empty Trash).
func (c *computerFS) purge(vroot, id string) error {
	dir, top, err := c.trashRoot(vroot)
	if err != nil {
		return err
	}
	if _, err := c.trashItem(vroot, dir, top, id); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(dir, "files", id)); err != nil {
		return err
	}
	return os.Remove(filepath.Join(dir, "info", id+".trashinfo"))
}

// ---------- disks (oros-system) ----------

var devPath = regexp.MustCompile(`^/dev/[a-z0-9]+$`)

func (c *computerFS) diskList() (json.RawMessage, error) {
	if c.helper == "" {
		return json.RawMessage("[]"), nil
	}
	out, err := c.run("disks")
	if err != nil {
		return nil, fsErr(http.StatusInternalServerError, "io", strings.TrimSpace(out))
	}
	var probe []any
	if json.Unmarshal([]byte(out), &probe) != nil {
		return nil, fsErr(http.StatusInternalServerError, "io", "the disk list cannot be read")
	}
	return json.RawMessage(out), nil
}

func (c *computerFS) mount(dev string, off bool) (string, error) {
	if c.helper == "" {
		return "", errNotFound
	}
	if !devPath.MatchString(dev) {
		return "", errBadPath
	}
	args := []string{"mount", "--dev", dev}
	if off {
		for _, d := range c.disks() {
			if c.busy(d.Path) {
				return "", fsErr(http.StatusConflict, "busy", "a copy to this disk is still running")
			}
		}
		args = append(args, "--unmount")
	}
	out, err := c.sudo(args...)
	if err != nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		msg := strings.TrimPrefix(lines[len(lines)-1], "Error: ")
		if strings.Contains(out, "busy") {
			return "", fsErr(http.StatusConflict, "busy", msg)
		}
		return "", fsErr(http.StatusInternalServerError, "io", msg)
	}
	if off {
		return "", nil
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	dir := lines[len(lines)-1]
	if filepath.Dir(dir) != c.media {
		return "", nil
	}
	return "/disks/" + filepath.Base(dir), nil
}

// diskRoot is the folder of disk name under the media folder, while it is
// mounted.
func (c *computerFS) diskRoot(name string) (string, bool) {
	dir := filepath.Join(c.media, name)
	return dir, c.mounted(dir)
}

// within: path is root or inside it.
func within(path, root string) bool {
	return path == root || strings.HasPrefix(path, strings.TrimSuffix(root, "/")+"/")
}

func crossDevice(err error) bool { return errors.Is(err, syscall.EXDEV) }

// nameOK: every name is fine on Linux except the ones resolve refuses.
func nameOK(string) bool { return true }

// systemHidden: Linux hides by the leading dot only.
func systemHidden(string, []string) bool { return false }

// openRecycleBin is Windows only; Linux has its own Trash list.
func openRecycleBin() error {
	return fsErr(http.StatusBadRequest, "badop", "unknown operation")
}
