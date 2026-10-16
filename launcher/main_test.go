package main

import (
	"archive/zip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func testApp(t *testing.T) string {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>orOS</h1>"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "shell.js"), []byte("1"), 0o644))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func get(h http.Handler, host, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Host = host
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHandlerServesWithPinnedTypes(t *testing.T) {
	h := newHandler(testApp(t))
	w := get(h, "localhost:47800", "/shell.js")
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("got %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w := get(h, "127.0.0.1:47800", "/"); w.Code != 200 {
		t.Fatalf("index: %d", w.Code)
	}
}

func TestHandlerRejectsForeignHost(t *testing.T) {
	h := newHandler(testApp(t))
	for _, host := range []string{"evil.example:47800", "localhost:80", "localhost"} {
		if w := get(h, host, "/"); w.Code != http.StatusForbidden {
			t.Fatalf("%s: got %d", host, w.Code)
		}
	}
}

func TestHandlerRejectsWrites(t *testing.T) {
	h := newHandler(testApp(t))
	r := httptest.NewRequest("POST", "/index.html", nil)
	r.Host = "localhost:47800"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", w.Code)
	}
}

func TestMarker(t *testing.T) {
	w := get(newHandler(testApp(t)), "localhost:47800", markerPath)
	if w.Body.String() != markerBody {
		t.Fatalf("got %q", w.Body.String())
	}
}

func writeZip(t *testing.T, files map[string]string) string {
	p := filepath.Join(t.TempDir(), "a.zip")
	f, err := os.Create(p)
	must(t, err)
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		must(t, err)
		_, err = w.Write([]byte(body))
		must(t, err)
	}
	must(t, zw.Close())
	must(t, f.Close())
	return p
}

func TestUnpackStripsRootAndSkipsNonAppFolders(t *testing.T) {
	z := writeZip(t, map[string]string{
		"oros-main/index.html":       "x",
		"oros-main/notes/index.html": "n",
		"oros-main/relay/core.js":    "r",
		"oros-main/tests/a.test.js":  "t",
	})
	dest := filepath.Join(t.TempDir(), "app.new")
	must(t, unpackApp(z, dest))
	if _, err := os.Stat(filepath.Join(dest, "notes", "index.html")); err != nil {
		t.Fatal(err)
	}
	for _, skipped := range []string{"relay", "tests"} {
		if _, err := os.Stat(filepath.Join(dest, skipped)); err == nil {
			t.Fatalf("%s was unpacked", skipped)
		}
	}
}

func TestUnpackRejectsPathTraversal(t *testing.T) {
	z := writeZip(t, map[string]string{
		"oros-main/index.html":    "x",
		"oros-main/../../evil.js": "e",
	})
	if err := unpackApp(z, filepath.Join(t.TempDir(), "app.new")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestSwapKeepsPreviousVersion(t *testing.T) {
	root := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(root, "app"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "app", "v"), []byte("1"), 0o644))
	must(t, os.MkdirAll(filepath.Join(root, "app.new"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "app.new", "v"), []byte("2"), 0o644))
	must(t, swapApp(root, filepath.Join(root, "app.new")))
	if b, _ := os.ReadFile(filepath.Join(root, "app", "v")); string(b) != "2" {
		t.Fatalf("app has %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "app.old", "v")); string(b) != "1" {
		t.Fatalf("app.old has %q", b)
	}
}

func TestProfileInUse(t *testing.T) {
	dir := t.TempDir()
	if profileInUse(dir) {
		t.Fatal("empty profile reported in use")
	}
	must(t, os.Symlink(fmt.Sprintf("host-%d", os.Getpid()), filepath.Join(dir, "SingletonLock")))
	if !profileInUse(dir) {
		t.Fatal("live lock not seen")
	}
	must(t, os.Remove(filepath.Join(dir, "SingletonLock")))
	must(t, os.Symlink("host-999999999", filepath.Join(dir, "SingletonLock")))
	if profileInUse(dir) {
		t.Fatal("stale lock reported in use")
	}
}
