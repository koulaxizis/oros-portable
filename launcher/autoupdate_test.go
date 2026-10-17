package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const testCommit = "5ea7e32af879112a67059f1b21fd51c766200e1b"

// orosZip builds a GitHub-style archive of a tiny orOS with the commit in
// the archive comment.
func orosZip(t *testing.T, ver string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"oros-main/index.html": "<!doctype html><title>orOS</title>",
		"oros-main/shell.js":   `var APP_VERSION = "` + ver + `";`,
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.SetComment(testCommit)
	_ = zw.Close()
	return buf.Bytes()
}

func installedLayout(t *testing.T) layout {
	t.Helper()
	root, data := t.TempDir(), t.TempDir()
	t.Setenv("OROS_DATA_DIR", data)
	_ = os.WriteFile(filepath.Join(root, installedMarker), nil, 0o644)
	writeApp(t, filepath.Join(root, "app"), "2.70.00")
	return newLayout(root)
}

func TestAppUpdateIsStagedThenAppliedAtNextStart(t *testing.T) {
	lay := installedLayout(t)
	var zips int32
	body := orosZip(t, "2.70.00")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sha":
			_, _ = w.Write([]byte(testCommit))
		case "/main.zip":
			atomic.AddInt32(&zips, 1)
			_, _ = w.Write(body)
		}
	}))
	defer srv.Close()
	oldC, oldU := commitAPI, updateURL
	commitAPI, updateURL = srv.URL+"/sha", srv.URL+"/main.zip"
	defer func() { commitAPI, updateURL = oldC, oldU }()

	if err := checkAppUpdate(lay); err != nil {
		t.Fatal(err)
	}
	if appCommit(stagedAppDir(lay)) != testCommit {
		t.Fatal("the new orOS was not staged with its commit")
	}
	if lay.appDir() != filepath.Join(lay.root, "app") {
		t.Fatal("a staged update must not be served before the next start")
	}
	if err := checkAppUpdate(lay); err != nil || zips != 1 {
		t.Fatalf("the same commit was downloaded again (%d downloads, %v)", zips, err)
	}

	applyStagedApp(lay)
	if got := lay.appDir(); got != filepath.Join(lay.data, "app") || appCommit(got) != testCommit {
		t.Fatalf("after the next start orOS serves %s", got)
	}
	if err := checkAppUpdate(lay); err != nil || zips != 1 {
		t.Fatalf("the served commit was downloaded again (%d downloads)", zips)
	}
}

func TestAutoUpdatesOnlyForInstalledReleaseBuilds(t *testing.T) {
	if autoUpdatesOn(newLayout(t.TempDir())) {
		t.Fatal("portable copies must not update themselves")
	}
	lay := installedLayout(t)
	old := version
	defer func() { version = old }()
	version = "0.0.0-abc1234"
	if autoUpdatesOn(lay) {
		t.Fatal("test builds must not update themselves")
	}
	version = "0.2.0"
	if !autoUpdatesOn(lay) {
		t.Fatal("installed release builds update themselves")
	}
	_ = os.WriteFile(filepath.Join(lay.data, "no-auto-update"), nil, 0o644)
	if autoUpdatesOn(lay) {
		t.Fatal("no-auto-update must turn updates off")
	}
}

func releaseServer(t *testing.T, tag string, files map[string][]byte, sums map[string]string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			rel := release{Tag: tag, Page: srv.URL + "/page"}
			for name := range files {
				rel.Assets = append(rel.Assets, asset{name, srv.URL + "/f/" + name})
			}
			for name := range sums {
				rel.Assets = append(rel.Assets, asset{name, srv.URL + "/s/" + name})
			}
			_ = json.NewEncoder(w).Encode(rel)
			return
		}
		if b, ok := files[strings.TrimPrefix(r.URL.Path, "/f/")]; ok {
			_, _ = w.Write(b)
			return
		}
		if s, ok := sums[strings.TrimPrefix(r.URL.Path, "/s/")]; ok {
			_, _ = w.Write([]byte(s))
			return
		}
		http.NotFound(w, r)
	}))
	old := releaseAPI
	releaseAPI = srv.URL + "/latest"
	t.Cleanup(func() { releaseAPI = old; srv.Close() })
	return srv
}

func sumOf(b []byte, name string) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]) + "  " + name + "\n"
}

func TestDownloadChecksChecksum(t *testing.T) {
	good := []byte("new setup")
	releaseServer(t, "v0.3.0",
		map[string][]byte{"a.exe": good, "b.exe": []byte("tampered")},
		map[string]string{"a.exe.sha256": sumOf(good, "a.exe"), "b.exe.sha256": sumOf(good, "b.exe")})
	rel, err := latestRelease()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if _, err := download(rel, "a.exe", filepath.Join(dir, "a.exe")); err != nil {
		t.Fatal(err)
	}
	if _, err := download(rel, "b.exe", filepath.Join(dir, "b.exe")); err == nil {
		t.Fatal("a file that does not match its checksum was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.exe")); err == nil {
		t.Fatal("the bad download was kept")
	}
	if _, err := download(rel, "c.exe", filepath.Join(dir, "c.exe")); err == nil {
		t.Fatal("a missing asset was accepted")
	}
}

func TestReplaceAppImageKeepsTheUsersFileName(t *testing.T) {
	img := []byte("new appimage")
	name := appImageName("0.3.0")
	releaseServer(t, "v0.3.0", map[string][]byte{name: img}, map[string]string{name + ".sha256": sumOf(img, name)})
	rel, _ := latestRelease()
	mine := filepath.Join(t.TempDir(), "my orOS.AppImage")
	_ = os.WriteFile(mine, []byte("old"), 0o755)
	if err := replaceAppImage(rel, mine); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(mine); string(b) != "new appimage" {
		t.Fatalf("AppImage not replaced: %q", b)
	}
	if st, _ := os.Stat(mine); st.Mode()&0o111 == 0 {
		t.Fatal("the new AppImage is not executable")
	}
}

func TestPendingSetupPicksNewestAndCleansUp(t *testing.T) {
	lay := installedLayout(t)
	old := version
	defer func() { version = old }()
	version = "0.2.0"
	dir := updatesDir(lay)
	_ = os.MkdirAll(dir, 0o755)
	for _, v := range []string{"0.1.9", "0.2.0", "0.2.1", "0.3.0"} {
		_ = os.WriteFile(filepath.Join(dir, setupName(v)), nil, 0o644)
	}
	path, ver := pendingSetup(lay)
	if ver != "0.3.0" || filepath.Base(path) != setupName("0.3.0") {
		t.Fatalf("picked %s %s", path, ver)
	}
	left, _ := os.ReadDir(dir)
	if len(left) != 1 {
		t.Fatalf("older setups were kept: %v", left)
	}
	version = "0.3.0" // after the upgrade
	if path, _ := pendingSetup(lay); path != "" {
		t.Fatal("the installed version is not pending")
	}
}

func TestNoProgramUpdateForSameOrOlderRelease(t *testing.T) {
	lay := installedLayout(t)
	old := version
	defer func() { version = old }()
	version = "0.3.0"
	releaseServer(t, "v0.3.0", nil, nil)
	if err := checkProgramUpdate(lay); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(lay.data, "notified-release")); err == nil {
		t.Fatal("told about a release that is not newer")
	}
}
