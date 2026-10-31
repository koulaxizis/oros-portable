//go:build linux || windows

package main

// Computer files: the orOS Files app's "This computer" place.
//
// On orOS USB (the live stick and orOS installed on a disk) the launcher
// lets the orOS page read and write the user's own folders and the disks
// opened under /media/oros, through a small API under /__oros/fs/
// (OROS_COMPUTER_FILES=1, which oros-usb's session sets).
//
// orOS Portable and the desktop apps on Linux offer the same, once the
// user agreed for this computer (fsconsent.go; the desktop apps are on by
// default): the home folder, and disks opened by udisks under
// /run/media/<user> or /media/<user>, with the launcher as its own disk
// helper (hostdisks_linux.go). The orOS Portable folder on the stick and
// the data folder are never reachable (private).
//
// Who can call it: only the orOS page itself. Every request needs
//   - the Host check that the whole server does (no DNS rebinding);
//   - the cookie oros_fs, a random key made at each start and set (HttpOnly,
//     SameSite=Strict, Path=/__oros/fs/) on the orOS pages this server
//     sends;
//   - Sec-Fetch-Site: same-origin, which Chromium sends with every request
//     to localhost, so another site, another localhost port or the Internet
//     window (another profile, without the cookie) is refused;
//   - except plain file reads (an <img> or <video> source), the header
//     X-Oros-FS: 1, which no page of another origin can send without a
//     CORS preflight that this server never answers.
//
// What it can reach: "/home/..." is the home folder and "/disks/<name>/..."
// a disk mounted under /media/oros (only while it is mounted). Names that
// start with "." and the home folder's own "oros" (the launcher, the
// browser profiles) are never listed or reachable, and no symbolic link
// leads out of its place. Nothing is ever run, nothing is deleted outright
// outside the Trash: removing moves to the freedesktop Trash (the home
// one, or the disk's own .Trash-<uid>), where Files can put it back or
// empty it. File reads carry "Content-Security-Policy: sandbox" so a web
// page kept on the disk can never run as orOS.
//
// Writes go to a hidden temporary file in the same folder, are flushed and
// then renamed, so a cut-off write never leaves half a file. A write with
// ifmtime refuses (412) when the file changed since it was read. A move to
// another disk copies, reads the copy back against the original's SHA-256
// and only then puts the original in the Trash.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

const (
	fsCookie = "oros_fs"
	fsHeader = "X-Oros-FS"
)

// computerFS serves the API. home and media are the real folders behind
// "/home" and "/disks"; helper is the program that lists and mounts disks
// (oros-system on orOS USB), mounted tells whether a folder is a mount
// point. Tests set their own.
type computerFS struct {
	token   string
	home    string
	media   string
	helper  string
	mounted func(dir string) bool
	sudo    func(args ...string) (string, error)
	run     func(args ...string) (string, error)
	uid     int
	// consent decides whether the API answers (nil: always, as in tests);
	// private are real folders that are never listed or reachable.
	consent *fsConsent
	private []string

	mu     sync.Mutex
	writes map[string]int // active writes per disk, so a disk is not taken away mid-write
}

// fsAPI serves "This computer" on Linux (nil elsewhere, or with
// OROS_NO_COMPUTER_FILES); whether it answers is fsConsent's call.
var fsAPI = newComputerFSFromEnv()

func newComputerFSFromEnv() *computerFS {
	if os.Getenv("OROS_NO_COMPUTER_FILES") != "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("computer files: %v", err)
		return nil
	}
	root, err := rootDir()
	if err != nil {
		return nil
	}
	lay := newLayout(root)
	if os.Getenv("OROS_COMPUTER_FILES") == "1" { // orOS USB
		media := os.Getenv("OROS_FS_MEDIA")
		if media == "" {
			media = "/media/oros"
		}
		c := newComputerFS(home, media, os.Getenv("OROS_FS_HELPER"))
		c.consent = newFSConsent(lay, true)
		return c
	}
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if p := os.Getenv("APPIMAGE"); p != "" {
		exe = p
	}
	c := newComputerFS(home, hostMedia(), exe)
	c.sudo = c.run // udisks needs no root (and Windows has no mounting)
	c.consent = newFSConsent(lay, false)
	c.private = realPaths(lay.root, lay.data)
	return c
}

// realPaths resolves links, so a private folder is found by any path.
func realPaths(dirs ...string) []string {
	var out []string
	for _, d := range dirs {
		if r, err := filepath.EvalSymlinks(d); err == nil {
			out = append(out, r)
		} else if a, err := filepath.Abs(d); err == nil {
			out = append(out, a)
		}
	}
	return out
}

func (c *computerFS) isPrivate(real string) bool {
	for _, p := range c.private {
		if within(real, p) {
			return true
		}
	}
	return false
}

func (c *computerFS) on() bool { return c.consent == nil || c.consent.on() }

func newComputerFS(home, media, helper string) *computerFS {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	c := &computerFS{
		token: hex.EncodeToString(b), home: home, media: media, helper: helper,
		mounted: isMountpoint, uid: os.Getuid(), writes: map[string]int{},
	}
	c.run = func(args ...string) (string, error) {
		out, err := exec.Command(c.helper, args...).CombinedOutput()
		return string(out), err
	}
	c.sudo = func(args ...string) (string, error) {
		out, err := exec.Command("sudo", append([]string{"-n", c.helper}, args...)...).CombinedOutput()
		return string(out), err
	}
	return c
}

// setCookie gives the orOS page its key; called for every HTML page.
func (c *computerFS) setCookie(w http.ResponseWriter) {
	w.Header().Add("Set-Cookie", fsCookie+"="+c.token+"; Path="+fsPrefix+"; HttpOnly; SameSite=Strict")
}

// ---------- errors ----------

type fsError struct {
	status int
	code   string
	msg    string
}

func (e *fsError) Error() string { return e.msg }

func fsErr(status int, code, msg string) error { return &fsError{status, code, msg} }

var (
	errNotFound = fsErr(http.StatusNotFound, "notfound", "not found")
	errHidden   = fsErr(http.StatusForbidden, "denied", "this place is not shown")
	errBadPath  = fsErr(http.StatusBadRequest, "badpath", "bad path")
)

func mapErr(err error) *fsError {
	var fe *fsError
	if errors.As(err, &fe) {
		return fe
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return &fsError{http.StatusNotFound, "notfound", "not found"}
	case errors.Is(err, fs.ErrExist):
		return &fsError{http.StatusConflict, "exists", "a file with this name is already there"}
	case errors.Is(err, syscall.EROFS):
		return &fsError{http.StatusForbidden, "readonly", "this disk is read-only"}
	case errors.Is(err, fs.ErrPermission):
		return &fsError{http.StatusForbidden, "denied", "no permission"}
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return &fsError{http.StatusInsufficientStorage, "full", "the disk is full"}
	case errors.Is(err, syscall.ENOTEMPTY):
		return &fsError{http.StatusConflict, "notempty", "the folder is not empty"}
	case errors.Is(err, syscall.EBUSY):
		return &fsError{http.StatusConflict, "busy", "the disk is in use"}
	}
	return &fsError{http.StatusInternalServerError, "io", err.Error()}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	e := mapErr(err)
	if e.status >= 500 {
		log.Printf("computer files: %v", err)
	}
	writeJSON(w, e.status, map[string]string{"error": e.code, "message": e.msg})
}

// ---------- paths ----------

// place is a resolved virtual path.
type place struct {
	vpath string // cleaned "/home/a/b"
	root  string // real folder of its root ("/home/oros", "/media/oros/STICK")
	vroot string // "/home" or "/disks/STICK"
	real  string // real path (not resolved through links)
}

// resolve maps a virtual path to a real one inside its root. "/", "/disks"
// are virtual folders and come back with real == "".
func (c *computerFS) resolve(v string) (place, error) {
	if v == "" || !strings.HasPrefix(v, "/") || strings.ContainsRune(v, 0) || strings.Contains(v, "\\") {
		return place{}, errBadPath
	}
	clean := path.Clean(v)
	segs := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	for _, s := range segs {
		if s == ".." {
			return place{}, errBadPath
		}
	}
	if clean == "/" || clean == "/disks" {
		return place{vpath: clean}, nil
	}
	var p place
	var rest []string
	switch segs[0] {
	case "home":
		p.root, p.vroot, rest = c.home, "/home", segs[1:]
		if len(rest) > 0 && rest[0] == "oros" {
			return place{}, errHidden
		}
	case "disks":
		name := segs[1]
		if strings.HasPrefix(name, ".") {
			return place{}, errHidden
		}
		root, found := c.diskRoot(name)
		p.root, p.vroot, rest = root, "/disks/"+name, segs[2:]
		if !found {
			return place{}, errNotFound
		}
	default:
		return place{}, errNotFound
	}
	for _, s := range rest {
		if strings.HasPrefix(s, ".") {
			return place{}, errHidden
		}
		if !nameOK(s) {
			return place{}, errBadPath
		}
	}
	p.vpath = clean
	p.real = filepath.Join(append([]string{p.root}, rest...)...)
	if err := c.inside(p); err != nil {
		return place{}, err
	}
	if systemHidden(p.root, rest) {
		return place{}, errHidden
	}
	return p, nil
}

// inside checks that the deepest existing part of p resolves (through
// links) to a place inside p's root.
func (c *computerFS) inside(p place) error {
	root, err := filepath.EvalSymlinks(p.root)
	if err != nil {
		return err
	}
	probe := p.real
	for {
		r, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if !within(r, root) {
				return errHidden
			}
			if c.isPrivate(r) {
				return errHidden
			}
			// A link may not lead to a hidden name either.
			rel := strings.Split(strings.TrimPrefix(r, root), string(filepath.Separator))
			for i, s := range rel {
				if strings.HasPrefix(s, ".") || (i == 1 && p.vroot == "/home" && s == "oros") {
					return errHidden
				}
			}
			return nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(probe)
		if parent == probe || len(parent) < len(p.root) {
			return errNotFound
		}
		probe = parent
	}
}

// vchild is the virtual path of name inside folder v.
func vchild(v, name string) string {
	if v == "/" {
		return "/" + name
	}
	return v + "/" + name
}

// ---------- entries ----------

type entry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"` // ms
}

func toEntry(vpath string, fi fs.FileInfo) entry {
	e := entry{Name: path.Base(vpath), Path: vpath, Dir: fi.IsDir(), MTime: fi.ModTime().UnixMilli()}
	if !e.Dir {
		e.Size = fi.Size()
	}
	return e
}

type rootInfo struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	ReadOnly bool   `json:"readonly"`
	Free     int64  `json:"free"`
	Total    int64  `json:"total"`
}

func (c *computerFS) rootInfo(vroot, name, real string) rootInfo {
	ri := rootInfo{Path: vroot, Name: name}
	ri.Free, ri.Total, ri.ReadOnly = diskSpace(real)
	return ri
}

func (c *computerFS) list(v string) ([]entry, error) {
	p, err := c.resolve(v)
	if err != nil {
		return nil, err
	}
	switch p.vpath {
	case "/":
		return []entry{{Name: "home", Path: "/home", Dir: true}, {Name: "disks", Path: "/disks", Dir: true}}, nil
	case "/disks":
		out := []entry{}
		for _, d := range c.disks() {
			out = append(out, entry{Name: d.Name, Path: d.Path, Dir: true})
		}
		return out, nil
	}
	ents, err := os.ReadDir(p.real)
	if err != nil {
		return nil, err
	}
	out := []entry{}
	for _, de := range ents {
		name := de.Name()
		if strings.HasPrefix(name, ".") || (p.vpath == "/home" && name == "oros") {
			continue
		}
		cv := vchild(p.vpath, name)
		cp, err := c.resolve(cv)
		if err != nil {
			continue // a link that leads out
		}
		fi, err := os.Stat(cp.real)
		if err != nil {
			continue
		}
		if !fi.IsDir() && !fi.Mode().IsRegular() {
			continue // devices, sockets, pipes
		}
		out = append(out, toEntry(cv, fi))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *computerFS) stat(v string) (entry, error) {
	p, err := c.resolve(v)
	if err != nil {
		return entry{}, err
	}
	if p.real == "" {
		return entry{Name: path.Base(p.vpath), Path: p.vpath, Dir: true}, nil
	}
	fi, err := os.Stat(p.real)
	if err != nil {
		return entry{}, err
	}
	return toEntry(p.vpath, fi), nil
}

// writable resolves v for a change: a real place, not a root itself.
func (c *computerFS) writable(v string) (place, error) {
	p, err := c.resolve(v)
	if err != nil {
		return place{}, err
	}
	if p.real == "" || p.vpath == p.vroot {
		return place{}, fsErr(http.StatusForbidden, "denied", "this place cannot be changed")
	}
	return p, nil
}

func (c *computerFS) begin(p place) func() {
	c.mu.Lock()
	c.writes[p.vroot]++
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		c.writes[p.vroot]--
		if c.writes[p.vroot] <= 0 {
			delete(c.writes, p.vroot)
		}
		c.mu.Unlock()
	}
}

func (c *computerFS) busy(vroot string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes[vroot] > 0
}

// ---------- writing ----------

// tempName is a hidden file next to dst, so it is never listed.
func tempName(dst string) string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return filepath.Join(filepath.Dir(dst), ".oros-part-"+hex.EncodeToString(b))
}

// writeAtomic writes r to dst through a temporary file. replace=false
// refuses an existing dst; ifmtime > 0 refuses a dst whose time moved.
// It returns the written size and SHA-256.
func writeAtomic(dst string, r io.Reader, replace bool, ifmtime int64, mode fs.FileMode) (int64, []byte, error) {
	if err := checkTarget(dst, replace, ifmtime); err != nil {
		return 0, nil, err
	}
	tmp := tempName(dst)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return 0, nil, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), r)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// Checked again: the copy may have taken minutes.
		err = checkTarget(dst, replace, ifmtime)
	}
	if err == nil {
		err = os.Rename(tmp, dst)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return 0, nil, err
	}
	syncDir(filepath.Dir(dst))
	return n, h.Sum(nil), nil
}

func checkTarget(dst string, replace bool, ifmtime int64) error {
	fi, err := os.Stat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		if ifmtime > 0 {
			return fsErr(http.StatusPreconditionFailed, "changed", "the file was removed elsewhere")
		}
		return nil
	}
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return fs.ErrExist
	}
	if !replace {
		return fs.ErrExist
	}
	if ifmtime > 0 && fi.ModTime().UnixMilli() != ifmtime {
		return fsErr(http.StatusPreconditionFailed, "changed", "the file was changed elsewhere")
	}
	return nil
}

func syncDir(dir string) {
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

func hashFile(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// copyTree copies src to dst (which must not exist). verify reads every
// copied file back and compares it with the original.
func copyTree(src, dst string, verify bool) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, sum, err := writeAtomic(dst, io.TeeReader(f, h), false, 0, 0o644)
		f.Close()
		if err != nil {
			return err
		}
		_ = os.Chtimes(dst, fi.ModTime(), fi.ModTime())
		if verify {
			back, err := hashFile(dst)
			if err != nil {
				return err
			}
			if string(back) != string(sum) || string(sum) != string(h.Sum(nil)) {
				return fsErr(http.StatusInternalServerError, "verify", "the copy does not match the original")
			}
		}
		return nil
	}
	if err := os.Mkdir(dst, 0o755); err != nil {
		return err
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if !e.IsDir() && !e.Type().IsRegular() && e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		if e.Type()&fs.ModeSymlink != 0 {
			continue // links are not copied: they could point anywhere
		}
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name()), verify); err != nil {
			return err
		}
	}
	return nil
}

type trashItem struct {
	Root  string `json:"root"` // "/home" or "/disks/NAME"
	ID    string `json:"id"`   // the name in the Trash
	Name  string `json:"name"`
	From  string `json:"from"` // virtual path it came from ("" when outside orOS's places)
	At    string `json:"at"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	MTime int64  `json:"mtime"`
}

// ---------- HTTP ----------

func (c *computerFS) allowed(r *http.Request, raw bool) bool {
	ck, err := r.Cookie(fsCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(ck.Value), []byte(c.token)) != 1 {
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
		return false
	}
	if raw && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		return true
	}
	return r.Header.Get(fsHeader) == "1"
}

func (c *computerFS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, fsPrefix)
	if !c.allowed(r, name == "raw") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "denied", "message": "only orOS may use this"})
		return
	}
	if name == "consent" {
		c.serveConsent(w, r)
		return
	}
	if !c.on() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "off", "message": "this computer's files are not shared with orOS"})
		return
	}
	q := r.URL.Query()
	switch {
	case name == "roots" && r.Method == http.MethodGet:
		home := c.rootInfo("/home", "home", c.home)
		// os tells orOS Files which Trash this is (Windows: the Recycle Bin).
		writeJSON(w, 200, map[string]any{"home": home, "disks": c.disks(), "os": runtime.GOOS})
	case name == "list" && r.Method == http.MethodGet:
		l, err := c.list(q.Get("path"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, l)
	case name == "stat" && r.Method == http.MethodGet:
		e, err := c.stat(q.Get("path"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, e)
	case name == "raw" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		c.serveRaw(w, r, q.Get("path"))
	case name == "raw" && r.Method == http.MethodPut:
		c.putRaw(w, r)
	case name == "trash" && r.Method == http.MethodGet:
		writeJSON(w, 200, c.trashList())
	case name == "disks" && r.Method == http.MethodGet:
		l, err := c.diskList()
		if err != nil {
			writeErr(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(l)
	case name == "op" && r.Method == http.MethodPost:
		c.op(w, r)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound", "message": "unknown call"})
	}
}

// Types that would run as a page are sent as text; everything else gets
// its own type. The sandbox makes any page an opaque origin without
// scripts, whatever the browser decides.
var rawAsText = map[string]bool{".html": true, ".htm": true, ".xhtml": true, ".shtml": true, ".mht": true, ".mhtml": true, ".xml": true, ".xsl": true, ".js": true, ".mjs": true}

func (c *computerFS) serveRaw(w http.ResponseWriter, r *http.Request, v string) {
	p, err := c.resolve(v)
	if err == nil && p.real == "" {
		err = errNotFound
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	f, err := os.Open(p.real)
	if err != nil {
		writeErr(w, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		writeErr(w, errNotFound)
		return
	}
	ext := strings.ToLower(filepath.Ext(p.real))
	h := w.Header()
	switch {
	case rawAsText[ext]:
		h.Set("Content-Type", "text/plain; charset=utf-8")
	case mimeTypes[ext] != "":
		h.Set("Content-Type", mimeTypes[ext])
	default:
		if t := mime.TypeByExtension(ext); t != "" {
			h.Set("Content-Type", t)
		} else {
			h.Set("Content-Type", "application/octet-stream")
		}
	}
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' data:; media-src 'self'; style-src 'unsafe-inline'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Oros-MTime", strconv.FormatInt(fi.ModTime().UnixMilli(), 10))
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (c *computerFS) putRaw(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, err := c.writable(q.Get("path"))
	if err != nil {
		writeErr(w, err)
		return
	}
	ifm, _ := strconv.ParseInt(q.Get("ifmtime"), 10, 64)
	done := c.begin(p)
	defer done()
	n, _, err := writeAtomic(p.real, r.Body, q.Get("replace") == "1", ifm, 0o644)
	if err != nil {
		writeErr(w, err)
		return
	}
	fi, err := os.Stat(p.real)
	if err != nil {
		writeErr(w, err)
		return
	}
	e := toEntry(p.vpath, fi)
	e.Size = n
	writeJSON(w, 200, e)
}

type opRequest struct {
	Op   string `json:"op"`
	Path string `json:"path"`
	To   string `json:"to"`
	Root string `json:"root"`
	ID   string `json:"id"`
	Dev  string `json:"dev"`
}

func (c *computerFS) op(w http.ResponseWriter, r *http.Request) {
	var req opRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeErr(w, errBadPath)
		return
	}
	res, err := c.do(req)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}

func (c *computerFS) do(req opRequest) (map[string]any, error) {
	ok := map[string]any{"ok": true}
	switch req.Op {
	case "mkdir":
		p, err := c.writable(req.Path)
		if err != nil {
			return nil, err
		}
		if err := os.Mkdir(p.real, 0o755); err != nil {
			return nil, err
		}
		return ok, nil
	case "rename", "move", "copy":
		var src place
		var err error
		if req.Op == "copy" {
			// A copy only reads its source; a whole disk may be copied too.
			if src, err = c.resolve(req.Path); err == nil && src.real == "" {
				err = errNotFound
			}
		} else {
			src, err = c.writable(req.Path)
		}
		if err != nil {
			return nil, err
		}
		dst, err := c.writable(req.To)
		if err != nil {
			return nil, err
		}
		if within(dst.real, src.real) {
			return nil, fsErr(http.StatusBadRequest, "inside", "a folder cannot go into itself")
		}
		if _, err := os.Lstat(dst.real); err == nil {
			return nil, fs.ErrExist
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
		if _, err := os.Stat(src.real); err != nil {
			return nil, err
		}
		done := c.begin(dst)
		defer done()
		if req.Op != "copy" {
			err := os.Rename(src.real, dst.real)
			if err == nil {
				return ok, nil
			}
			if !crossDevice(err) || req.Op == "rename" {
				return nil, err
			}
		}
		if err := copyTree(src.real, dst.real, req.Op == "move"); err != nil {
			// Leave nothing half-copied behind; the original is untouched.
			if c.trashPartial(dst) != nil {
				log.Printf("computer files: partial copy left at %s", dst.real)
			}
			return nil, err
		}
		if req.Op == "move" {
			if _, err := c.trash(src); err != nil {
				return map[string]any{"ok": true, "kept": true}, nil
			}
		}
		return ok, nil
	case "trash":
		p, err := c.writable(req.Path)
		if err != nil {
			return nil, err
		}
		done := c.begin(p)
		defer done()
		id, err := c.trash(p)
		if err != nil {
			return nil, err
		}
		root := p.vroot
		return map[string]any{"ok": true, "root": root, "id": id}, nil
	case "restore":
		to, err := c.restore(req.Root, req.ID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ok": true, "path": to}, nil
	case "purge":
		if err := c.purge(req.Root, req.ID); err != nil {
			return nil, err
		}
		return ok, nil
	case "recyclebin":
		// Windows: items go to the Recycle Bin and come back from there.
		if err := openRecycleBin(); err != nil {
			return nil, err
		}
		return ok, nil
	case "mount", "unmount":
		v, err := c.mount(req.Dev, req.Op == "unmount")
		if err != nil {
			return nil, err
		}
		if v != "" {
			return map[string]any{"ok": true, "path": v}, nil
		}
		return ok, nil
	}
	return nil, fsErr(http.StatusBadRequest, "badop", "unknown operation")
}

// trashPartial moves what a failed copy left at dst to the Trash.
func (c *computerFS) trashPartial(dst place) error {
	if _, err := os.Lstat(dst.real); err != nil {
		return nil
	}
	_, err := c.trash(dst)
	return err
}

// serveConsent: GET says "on", "off" or "ask" for this computer; POST
// {"on": true|false} keeps the user's answer (orOS asks once per computer
// and has a switch in Settings).
func (c *computerFS) serveConsent(w http.ResponseWriter, r *http.Request) {
	if c.consent == nil {
		writeJSON(w, 200, map[string]any{"state": "on", "fixed": true})
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, map[string]any{"state": c.consent.state(), "fixed": c.consent.always})
	case http.MethodPost:
		var req struct {
			On *bool `json:"on"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil || req.On == nil {
			writeErr(w, errBadPath)
			return
		}
		if err := c.consent.set(*req.On); err != nil {
			writeErr(w, err)
			return
		}
		log.Printf("computer files: %v for this computer", map[bool]string{true: "on", false: "off"}[*req.On])
		writeJSON(w, 200, map[string]any{"state": c.consent.state(), "fixed": c.consent.always})
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound", "message": "unknown call"})
	}
}
