//go:build linux

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// DRIVE-OPEN: GET /__oros/fs/drive names the folders orOS Drive mirrors
// by "This computer"'s virtual paths, and leaves out what this API would
// not show or a remote path orOS must not open.
func TestDriveMirrors(t *testing.T) {
	w := newFSWorld(t)
	cfg := t.TempDir()
	t.Setenv("OROS_DRIVE_CONFIG", cfg)
	must(t, os.MkdirAll(filepath.Join(w.home, "Documents", "Books"), 0o755))
	must(t, os.MkdirAll(filepath.Join(w.disk, "Photos"), 0o755))
	outside := t.TempDir()

	get := func() ([]driveMirror, int) {
		rec := w.req("GET", fsPrefix+"drive", nil)
		var m struct {
			Folders []driveMirror `json:"folders"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return m.Folders, rec.Code
	}

	// No Drive settings: an empty list, not an error.
	if l, code := get(); code != 200 || len(l) != 0 {
		t.Fatalf("no settings: %d %v", code, l)
	}

	app := map[string]any{"device": "PC", "folders": []map[string]any{
		{"id": "a", "local": filepath.Join(w.home, "Documents", "Books"), "remote": "/internal/Drive/PC/Books"},
		{"id": "b", "local": filepath.Join(w.disk, "Photos"), "remote": "/internal/Drive/PC/Photos", "paused": true},
		{"id": "c", "local": outside, "remote": "/internal/Drive/PC/Out"},                                // not shown here
		{"id": "d", "local": filepath.Join(w.home, ".ssh"), "remote": "/internal/Drive/PC/ssh"},          // hidden
		{"id": "e", "local": filepath.Join(w.home, "oros", "data"), "remote": "/internal/Drive/PC/data"}, // private
		{"id": "f", "local": filepath.Join(w.home, "Documents"), "remote": "/internal/Drive/PC/../Mail"}, // bad remote
		{"id": "g", "local": filepath.Join(w.home, "Documents"), "remote": "/internal/Mail"},             // not Drive
		{"id": "h", "local": "Documents", "remote": "/internal/Drive/PC/Rel"},                            // not absolute
	}}
	b, _ := json.Marshal(app)
	must(t, os.WriteFile(filepath.Join(cfg, "app.json"), b, 0o600))

	l, code := get()
	if code != 200 || len(l) != 2 {
		t.Fatalf("got %d %+v", code, l)
	}
	if l[0] != (driveMirror{Path: "/home/Documents/Books", Remote: "/internal/Drive/PC/Books"}) {
		t.Fatalf("home folder: %+v", l[0])
	}
	if l[1] != (driveMirror{Path: "/disks/STICK/Photos", Remote: "/internal/Drive/PC/Photos", Paused: true}) {
		t.Fatalf("disk folder: %+v", l[1])
	}

	// Only the orOS page may ask (same rules as every call here).
	rec := w.req("GET", fsPrefix+"drive", nil, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if rec.Code != 403 {
		t.Fatalf("cross-site: %d", rec.Code)
	}
}
