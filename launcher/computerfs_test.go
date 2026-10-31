//go:build linux

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fsWorld is a home folder, a media folder with one "mounted" disk and a
// computerFS over them.
type fsWorld struct {
	t     *testing.T
	c     *computerFS
	home  string
	media string
	disk  string
	h     http.Handler
}

func newFSWorld(t *testing.T) *fsWorld {
	base := t.TempDir()
	w := &fsWorld{t: t, home: filepath.Join(base, "home"), media: filepath.Join(base, "media")}
	w.disk = filepath.Join(w.media, "STICK")
	for _, d := range []string{w.home + "/Documents", w.home + "/oros/data", w.home + "/.ssh", w.disk, w.media + "/GONE"} {
		must(t, os.MkdirAll(d, 0o755))
	}
	must(t, os.WriteFile(filepath.Join(w.home, "Documents", "a.txt"), []byte("hello"), 0o644))
	must(t, os.WriteFile(filepath.Join(w.home, ".ssh", "id"), []byte("secret"), 0o600))
	t.Setenv("XDG_DATA_HOME", "")
	w.c = newComputerFS(w.home, w.media, "")
	w.c.mounted = func(dir string) bool { return dir == w.disk }
	old := fsAPI
	fsAPI = w.c
	t.Cleanup(func() { fsAPI = old })
	w.h = newHandler(testApp(t))
	return w
}

// req sends a request as the orOS page would.
func (w *fsWorld) req(method, target string, body io.Reader, mod ...func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, body)
	r.Host = "localhost:47800"
	r.AddCookie(&http.Cookie{Name: fsCookie, Value: w.c.token})
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set(fsHeader, "1")
	for _, m := range mod {
		m(r)
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

func (w *fsWorld) list(v string) ([]entry, int) {
	rec := w.req("GET", fsPrefix+"list?path="+url.QueryEscape(v), nil)
	var l []entry
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	return l, rec.Code
}

func (w *fsWorld) op(o opRequest) (map[string]any, int) {
	b, _ := json.Marshal(o)
	rec := w.req("POST", fsPrefix+"op", bytes.NewReader(b))
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return m, rec.Code
}

func (w *fsWorld) put(v, body, extra string) *httptest.ResponseRecorder {
	return w.req("PUT", fsPrefix+"raw?path="+url.QueryEscape(v)+extra, strings.NewReader(body))
}

func names(l []entry) string {
	var n []string
	for _, e := range l {
		n = append(n, e.Name)
	}
	return strings.Join(n, ",")
}

func TestFSOffByDefault(t *testing.T) {
	old := fsAPI
	fsAPI = nil
	defer func() { fsAPI = old }()
	h := newHandler(testApp(t))
	if w := get(h, "localhost:47800", fsPrefix+"roots"); w.Code != 404 {
		t.Fatalf("got %d", w.Code)
	}
	if w := get(h, "localhost:47800", "/"); w.Header().Get("Set-Cookie") != "" {
		t.Fatal("cookie set while off")
	}
}

func TestFSCookieOnPagesOnly(t *testing.T) {
	w := newFSWorld(t)
	ck := get(w.h, "localhost:47800", "/").Header().Get("Set-Cookie")
	for _, want := range []string{fsCookie + "=" + w.c.token, "HttpOnly", "SameSite=Strict", "Path=" + fsPrefix} {
		if !strings.Contains(ck, want) {
			t.Fatalf("cookie %q lacks %q", ck, want)
		}
	}
	if ck := get(w.h, "localhost:47800", "/shell.js").Header().Get("Set-Cookie"); ck != "" {
		t.Fatalf("script got a cookie: %q", ck)
	}
}

func TestFSRefusesOthers(t *testing.T) {
	w := newFSWorld(t)
	cases := map[string]func(*http.Request){
		"no cookie":     func(r *http.Request) { r.Header.Del("Cookie") },
		"wrong cookie":  func(r *http.Request) { r.Header.Set("Cookie", fsCookie+"=nope") },
		"cross-site":    func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		"same-site":     func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "same-site") },
		"no fetch-site": func(r *http.Request) { r.Header.Del("Sec-Fetch-Site") },
		"no header":     func(r *http.Request) { r.Header.Del(fsHeader) },
		"other origin":  func(r *http.Request) { r.Header.Set("Origin", "http://localhost:8080") },
		"foreign host":  func(r *http.Request) { r.Host = "evil.example:47800" },
	}
	for name, mod := range cases {
		if rec := w.req("GET", fsPrefix+"list?path=/home", nil, mod); rec.Code != 403 {
			t.Errorf("%s: list got %d", name, rec.Code)
		}
		if rec := w.req("POST", fsPrefix+"op", strings.NewReader(`{"op":"mkdir","path":"/home/x"}`), mod); rec.Code != 403 {
			t.Errorf("%s: op got %d", name, rec.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(w.home, "x")); err == nil {
		t.Fatal("a refused call made a folder")
	}
	// An <img> source carries no custom header but is same-origin.
	rec := w.req("GET", fsPrefix+"raw?path=/home/Documents/a.txt", nil, func(r *http.Request) { r.Header.Del(fsHeader) })
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("raw: %d %q", rec.Code, rec.Body.String())
	}
	// ...but a write always needs the header.
	rec = w.req("PUT", fsPrefix+"raw?path=/home/b.txt", strings.NewReader("x"), func(r *http.Request) { r.Header.Del(fsHeader) })
	if rec.Code != 403 {
		t.Fatalf("put without header: %d", rec.Code)
	}
}

func TestFSListHidesPrivateAndUnmounted(t *testing.T) {
	w := newFSWorld(t)
	l, code := w.list("/home")
	if code != 200 || names(l) != "Documents" {
		t.Fatalf("home: %d %s", code, names(l))
	}
	l, _ = w.list("/disks")
	if names(l) != "STICK" {
		t.Fatalf("disks: %s", names(l))
	}
	for _, v := range []string{"/home/oros", "/home/oros/data", "/home/.ssh", "/home/.ssh/id", "/disks/GONE", "/home/../etc", "/etc", "home", "/home/Documents/../../x"} {
		if _, code := w.list(v); code == 200 {
			t.Errorf("%s listed", v)
		}
		if rec := w.req("GET", fsPrefix+"raw?path="+url.QueryEscape(v), nil); rec.Code == 200 {
			t.Errorf("%s read", v)
		}
	}
}

func TestFSLinksStayInside(t *testing.T) {
	w := newFSWorld(t)
	outside := t.TempDir()
	must(t, os.WriteFile(filepath.Join(outside, "s"), []byte("x"), 0o644))
	must(t, os.Symlink(outside, filepath.Join(w.home, "Documents", "out")))
	must(t, os.Symlink(filepath.Join(w.home, "oros"), filepath.Join(w.home, "Documents", "prof")))
	must(t, os.Symlink("a.txt", filepath.Join(w.home, "Documents", "in.txt")))
	l, _ := w.list("/home/Documents")
	if names(l) != "a.txt,in.txt" {
		t.Fatalf("got %s", names(l))
	}
	for _, v := range []string{"/home/Documents/out/s", "/home/Documents/prof/data"} {
		if rec := w.req("GET", fsPrefix+"raw?path="+url.QueryEscape(v), nil); rec.Code == 200 {
			t.Errorf("%s read through a link", v)
		}
		if rec := w.put(v+"2", "x", ""); rec.Code == 200 {
			t.Errorf("%s written through a link", v)
		}
	}
}

func TestFSRawIsSandboxedAndRanged(t *testing.T) {
	w := newFSWorld(t)
	must(t, os.WriteFile(filepath.Join(w.home, "Documents", "p.html"), []byte("<script>x</script>"), 0o644))
	rec := w.req("GET", fsPrefix+"raw?path=/home/Documents/p.html", nil)
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("html served as %q", ct)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") {
		t.Fatalf("csp %q", csp)
	}
	rec = w.req("GET", fsPrefix+"raw?path=/home/Documents/a.txt", nil, func(r *http.Request) { r.Header.Set("Range", "bytes=1-3") })
	if rec.Code != 206 || rec.Body.String() != "ell" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body.String())
	}
}

func TestFSWriteSafely(t *testing.T) {
	w := newFSWorld(t)
	if rec := w.put("/home/Documents/a.txt", "new", ""); rec.Code != 409 {
		t.Fatalf("overwrite without replace: %d", rec.Code)
	}
	if rec := w.put("/home/Documents/a.txt", "new", "&replace=1&ifmtime=1"); rec.Code != 412 {
		t.Fatalf("stale ifmtime: %d", rec.Code)
	}
	fi, _ := os.Stat(filepath.Join(w.home, "Documents", "a.txt"))
	rec := w.put("/home/Documents/a.txt", "new", "&replace=1&ifmtime="+strconv.FormatInt(fi.ModTime().UnixMilli(), 10))
	if rec.Code != 200 {
		t.Fatalf("replace: %d %s", rec.Code, rec.Body.String())
	}
	if b, _ := os.ReadFile(filepath.Join(w.home, "Documents", "a.txt")); string(b) != "new" {
		t.Fatalf("got %q", b)
	}
	if rec := w.put("/home", "x", "&replace=1"); rec.Code != 403 {
		t.Fatalf("root written: %d", rec.Code)
	}
	if rec := w.put("/disks/STICK/b.bin", "abc", ""); rec.Code != 200 {
		t.Fatalf("disk write: %d", rec.Code)
	}
	// A write cut off halfway leaves neither half a file nor a temp file.
	rec = w.req("PUT", fsPrefix+"raw?path=/home/Documents/cut.txt", io.MultiReader(strings.NewReader("half"), failReader{}))
	if rec.Code == 200 {
		t.Fatal("broken body accepted")
	}
	ents, _ := os.ReadDir(filepath.Join(w.home, "Documents"))
	for _, e := range ents {
		if e.Name() != "a.txt" {
			t.Fatalf("left behind: %s", e.Name())
		}
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestFSOps(t *testing.T) {
	w := newFSWorld(t)
	if _, code := w.op(opRequest{Op: "mkdir", Path: "/home/Pictures"}); code != 200 {
		t.Fatalf("mkdir %d", code)
	}
	if _, code := w.op(opRequest{Op: "copy", Path: "/home/Documents", To: "/disks/STICK/Docs"}); code != 200 {
		t.Fatalf("copy %d", code)
	}
	if b, _ := os.ReadFile(filepath.Join(w.disk, "Docs", "a.txt")); string(b) != "hello" {
		t.Fatalf("copied %q", b)
	}
	if _, code := w.op(opRequest{Op: "copy", Path: "/home/Documents", To: "/home/Documents/inner"}); code != 400 {
		t.Fatalf("copy into itself %d", code)
	}
	if _, code := w.op(opRequest{Op: "rename", Path: "/home/Documents/a.txt", To: "/home/Pictures/a.txt"}); code != 200 {
		t.Fatalf("rename %d", code)
	}
	if _, code := w.op(opRequest{Op: "move", Path: "/home/Pictures/a.txt", To: "/disks/STICK/Docs/a.txt"}); code != 409 {
		t.Fatalf("move over a file %d", code)
	}
	if _, code := w.op(opRequest{Op: "rename", Path: "/home", To: "/home2"}); code == 200 {
		t.Fatal("root renamed")
	}
	if _, code := w.op(opRequest{Op: "mkdir", Path: "/home/.config"}); code != 403 {
		t.Fatalf("hidden mkdir %d", code)
	}
	if _, code := w.op(opRequest{Op: "frobnicate"}); code != 400 {
		t.Fatalf("unknown op %d", code)
	}
}

func TestFSTrashRoundTrip(t *testing.T) {
	w := newFSWorld(t)
	must(t, os.WriteFile(filepath.Join(w.home, "Documents", "b c.txt"), []byte("1"), 0o644))
	m, code := w.op(opRequest{Op: "trash", Path: "/home/Documents/b c.txt"})
	if code != 200 || m["root"] != "/home" {
		t.Fatalf("trash %d %v", code, m)
	}
	// A second item of the same name gets its own place.
	must(t, os.WriteFile(filepath.Join(w.home, "Documents", "b c.txt"), []byte("2"), 0o644))
	if _, code := w.op(opRequest{Op: "trash", Path: "/home/Documents/b c.txt"}); code != 200 {
		t.Fatal("second trash")
	}
	info, _ := os.ReadFile(filepath.Join(w.home, ".local/share/Trash/info/b c.txt.trashinfo"))
	if !strings.Contains(string(info), "Path="+filepath.Join(w.home, "Documents")+"/b%20c.txt") || !strings.Contains(string(info), "DeletionDate=") {
		t.Fatalf("trashinfo %q", info)
	}
	must(t, os.WriteFile(filepath.Join(w.disk, "d.txt"), []byte("3"), 0o644))
	if m, code := w.op(opRequest{Op: "trash", Path: "/disks/STICK/d.txt"}); code != 200 || m["root"] != "/disks/STICK" {
		t.Fatalf("disk trash %d %v", code, m)
	}
	if _, err := os.Stat(filepath.Join(w.disk, ".Trash-"+strconv.Itoa(os.Getuid()), "files", "d.txt")); err != nil {
		t.Fatal("not in the disk's own Trash")
	}
	rec := w.req("GET", fsPrefix+"trash", nil)
	var items []trashItem
	must(t, json.Unmarshal(rec.Body.Bytes(), &items))
	if len(items) != 3 {
		t.Fatalf("trash list %s", rec.Body.String())
	}
	from := map[string]trashItem{}
	for _, it := range items {
		from[it.From+"|"+it.ID] = it
	}
	if _, ok := from["/disks/STICK/d.txt|d.txt"]; !ok {
		t.Fatalf("disk item: %v", items)
	}
	if _, ok := from["/home/Documents/b c.txt|b c.2.txt"]; !ok {
		t.Fatalf("second item: %v", items)
	}
	// Restore refuses to overwrite, then works once the place is free.
	must(t, os.WriteFile(filepath.Join(w.home, "Documents", "b c.txt"), []byte("new"), 0o644))
	if _, code := w.op(opRequest{Op: "restore", Root: "/home", ID: "b c.2.txt"}); code != 409 {
		t.Fatalf("restore over a file %d", code)
	}
	must(t, os.Remove(filepath.Join(w.home, "Documents", "b c.txt")))
	if _, code := w.op(opRequest{Op: "restore", Root: "/home", ID: "b c.2.txt"}); code != 200 {
		t.Fatal("restore")
	}
	if b, _ := os.ReadFile(filepath.Join(w.home, "Documents", "b c.txt")); string(b) != "2" {
		t.Fatalf("restored %q", b)
	}
	if _, code := w.op(opRequest{Op: "purge", Root: "/home", ID: "b c.txt"}); code != 200 {
		t.Fatal("purge")
	}
	for _, bad := range []opRequest{{Op: "purge", Root: "/home", ID: "../../Documents"}, {Op: "purge", Root: "/etc", ID: "x"}, {Op: "restore", Root: "/disks/GONE", ID: "x"}} {
		if _, code := w.op(bad); code == 200 {
			t.Errorf("%v accepted", bad)
		}
	}
	if _, err := os.Stat(filepath.Join(w.home, "Documents")); err != nil {
		t.Fatal("purge escaped the Trash")
	}
}

func TestFSMoveAcrossDisksVerifiesAndKeepsOriginalInTrash(t *testing.T) {
	w := newFSWorld(t)
	// Across file systems os.Rename fails with EXDEV; the temp folders of
	// a test share one, so copyTree is checked directly here.
	src := filepath.Join(w.home, "Documents")
	dst := filepath.Join(w.disk, "Docs")
	must(t, copyTree(src, dst, true))
	if b, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(b) != "hello" {
		t.Fatalf("got %q", b)
	}
	old := time.Now().Add(-48 * time.Hour)
	must(t, os.Chtimes(filepath.Join(src, "a.txt"), old, old))
	must(t, os.RemoveAll(dst))
	must(t, copyTree(src, dst, true))
	fi, _ := os.Stat(filepath.Join(dst, "a.txt"))
	if !fi.ModTime().Equal(old.Truncate(time.Second)) && fi.ModTime().Sub(old).Abs() > time.Second {
		t.Fatalf("time not kept: %v", fi.ModTime())
	}
}

func TestFSMountGoesThroughHelper(t *testing.T) {
	w := newFSWorld(t)
	w.c.helper = "/opt/oros/oros-system"
	var got []string
	w.c.sudo = func(args ...string) (string, error) {
		got = args
		return w.disk + "\n", nil
	}
	w.c.run = func(args ...string) (string, error) { return `[{"path":"/dev/sdb1"}]`, nil }
	m, code := w.op(opRequest{Op: "mount", Dev: "/dev/sdb1"})
	if code != 200 || m["path"] != "/disks/STICK" || strings.Join(got, " ") != "mount --dev /dev/sdb1" {
		t.Fatalf("%d %v %v", code, m, got)
	}
	if _, code := w.op(opRequest{Op: "mount", Dev: "/dev/sdb1; rm -rf /"}); code != 400 {
		t.Fatalf("bad device %d", code)
	}
	if rec := w.req("GET", fsPrefix+"disks", nil); rec.Code != 200 || !strings.Contains(rec.Body.String(), "sdb1") {
		t.Fatalf("disks %d %s", rec.Code, rec.Body.String())
	}
	done := w.c.begin(place{vroot: "/disks/STICK"})
	if _, code := w.op(opRequest{Op: "unmount", Dev: "/dev/sdb1"}); code != 409 {
		t.Fatalf("unmount during a write %d", code)
	}
	done()
	if _, code := w.op(opRequest{Op: "unmount", Dev: "/dev/sdb1"}); code != 200 || strings.Join(got, " ") != "mount --dev /dev/sdb1 --unmount" {
		t.Fatalf("unmount %d %v", code, got)
	}
}

func TestUnescapeMount(t *testing.T) {
	if got := unescapeMount(`/media/oros/My\040Disk`); got != "/media/oros/My Disk" {
		t.Fatalf("got %q", got)
	}
}

func TestFSMoveToAnotherFileSystem(t *testing.T) {
	w := newFSWorld(t)
	shm, err := os.MkdirTemp("/dev/shm", "orosfs")
	if err != nil {
		t.Skip("no /dev/shm")
	}
	defer os.RemoveAll(shm)
	var a, b syscall.Stat_t
	if syscall.Stat(shm, &a) != nil || syscall.Stat(w.home, &b) != nil || a.Dev == b.Dev {
		t.Skip("/dev/shm is on the same file system")
	}
	disk := filepath.Join(shm, "SHM")
	must(t, os.MkdirAll(disk, 0o755))
	w.c.media = shm
	w.c.mounted = func(dir string) bool { return dir == disk }
	if _, code := w.op(opRequest{Op: "move", Path: "/home/Documents", To: "/disks/SHM/Docs"}); code != 200 {
		t.Fatalf("move %d", code)
	}
	if b, _ := os.ReadFile(filepath.Join(disk, "Docs", "a.txt")); string(b) != "hello" {
		t.Fatalf("moved %q", b)
	}
	if _, err := os.Stat(filepath.Join(w.home, "Documents")); err == nil {
		t.Fatal("original still in place")
	}
	if _, err := os.Stat(filepath.Join(w.home, ".local/share/Trash/files/Documents/a.txt")); err != nil {
		t.Fatal("original not in the Trash")
	}
}
