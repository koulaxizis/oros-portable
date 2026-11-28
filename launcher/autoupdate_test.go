package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
		case "/a/" + testCommit + ".zip":
			atomic.AddInt32(&zips, 1)
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	fakeOrOSArchive(t, srv.URL)

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

// fakeOrOSArchive points the commit API and the archive downloads at srv.
func fakeOrOSArchive(t *testing.T, srv string) {
	t.Helper()
	oldC, oldU, oldA := commitAPI, updateURL, archiveURL
	commitAPI, updateURL, archiveURL = srv+"/sha", srv+"/main.zip", srv+"/a/%s.zip"
	t.Cleanup(func() { commitAPI, updateURL, archiveURL = oldC, oldU, oldA })
}

func TestAutoUpdatesOnlyForInstalledReleaseBuilds(t *testing.T) {
	if autoUpdatesOn(newLayout(t.TempDir())) {
		t.Fatal("a stick's program must not update itself")
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

func TestScoopAndDebInstallsAreRecognised(t *testing.T) {
	if !managedByScoop(layout{root: `C:\Users\me\scoop\apps\oros\0.2.0`}) && !managedByScoop(layout{root: "C:/Users/me/scoop/apps/oros/0.2.0"}) {
		t.Fatal("a Scoop install was not recognised")
	}
	if managedByScoop(layout{root: `C:\Users\me\AppData\Local\Programs\orOS`}) {
		t.Fatal("the setup's own install is not Scoop's")
	}
	if isDebInstall(layout{root: "/tmp/x"}) {
		t.Fatal("only /opt/oros is the .deb install")
	}
}

func TestPendingDebPicksNewest(t *testing.T) {
	lay := installedLayout(t)
	old := version
	defer func() { version = old }()
	version = "0.2.0"
	dir := updatesDir(lay)
	_ = os.MkdirAll(dir, 0o755)
	for _, v := range []string{"0.2.0", "0.3.0", "0.2.5"} {
		_ = os.WriteFile(filepath.Join(dir, debName(v)), nil, 0o644)
	}
	_ = os.WriteFile(filepath.Join(dir, setupName("0.9.0")), nil, 0o644)
	if path, ver := pendingFile(lay, "oros_", "_amd64.deb"); ver != "0.3.0" || filepath.Base(path) != debName("0.3.0") {
		t.Fatalf("picked %s %s", path, ver)
	}
	if triedBefore(lay, "0.3.0") || !triedBefore(lay, "0.3.0") {
		t.Fatal("a version is offered once")
	}
}

func TestNoteCheckRetriesSoonAfterAFailure(t *testing.T) {
	d := t.TempDir()
	lay := layout{root: d, data: d, updates: d, installed: true}
	if !dueForCheck(lay) {
		t.Fatal("first check not due")
	}
	noteCheck(lay, nil, nil)
	if dueForCheck(lay) {
		t.Fatal("a good check is followed by another one at once")
	}
	noteCheck(lay, errors.New("latest commit: 403 Forbidden"), nil)
	st, err := os.Stat(filepath.Join(d, checkStamp))
	if err != nil {
		t.Fatal(err)
	}
	if left := checkEvery - time.Since(st.ModTime()); left > retryAfter+time.Minute || left < retryAfter-time.Minute {
		t.Fatalf("next check in %v, want about %v", left, retryAfter)
	}
	b, _ := os.ReadFile(filepath.Join(d, updateLogFile))
	if !strings.Contains(string(b), "403 Forbidden") || strings.Count(string(b), "\n") != 2 {
		t.Fatalf("update.log:\n%s", b)
	}
}

func TestBrokenCheckIsTriedAgainSoon(t *testing.T) {
	saved := quickRetries
	defer func() { quickRetries = saved }()
	quickRetries = []time.Duration{time.Millisecond, time.Millisecond}

	calls := 0
	err := withRetries("test", func() error {
		calls++
		if calls < 3 {
			return errors.New("wsarecv: an established connection was aborted")
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err %v after %d calls, want success on the third", err, calls)
	}

	calls = 0
	err = withRetries("test", func() error { calls++; return errors.New("down") })
	if err == nil || calls != 3 {
		t.Fatalf("err %v after %d calls, want failure after 3", err, calls)
	}

	calls = 0
	if err := withRetries("test", func() error { calls++; return nil }); err != nil || calls != 1 {
		t.Fatalf("a good check ran %d times", calls)
	}
}

func TestUpdateLogStaysSmall(t *testing.T) {
	d := t.TempDir()
	lay := layout{root: d, data: d, updates: d}
	line := strings.Repeat("x", 200)
	for i := 0; i < 1000; i++ {
		updateLog(lay, line)
	}
	b, _ := os.ReadFile(filepath.Join(d, updateLogFile))
	if len(b) > maxUpdateLog+300 || !strings.HasSuffix(string(b), line+"\n") {
		t.Fatalf("update.log is %d bytes", len(b))
	}
}

// Chris 2026-11-21: a downloaded orOS swaps in while a window is open
// (stagedAppWaiting), only a new program waits for no window.
func TestStagedAppWaitingNeedsAnIndex(t *testing.T) {
	d := t.TempDir()
	lay := layout{root: d, data: d, updates: d, installed: true}
	if stagedAppWaiting(lay) {
		t.Fatal("nothing staged yet")
	}
	next := stagedAppDir(lay)
	if err := os.MkdirAll(next, 0o755); err != nil {
		t.Fatal(err)
	}
	if stagedAppWaiting(lay) {
		t.Fatal("a half-unpacked folder without index.html is not an app")
	}
	if err := os.WriteFile(filepath.Join(next, "index.html"), []byte("<!doctype html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !stagedAppWaiting(lay) {
		t.Fatal("a staged app with index.html is waiting")
	}
}

func stickLayout(t *testing.T, ver string) layout {
	t.Helper()
	root := t.TempDir()
	writeApp(t, filepath.Join(root, "app"), ver)
	return newLayout(root)
}

func TestPortableAndUSBUpdateTheirOrOS(t *testing.T) {
	t.Setenv("OROS_NO_AUTO_UPDATE", "")
	t.Setenv("OROS_APP_AUTO_UPDATE", "")
	old := version
	defer func() { version = old }()

	version = "0.9.10"
	lay := stickLayout(t, "8.0.0")
	if !appAutoUpdatesOn(lay) {
		t.Fatal("a portable release build must update its orOS")
	}
	if autoUpdatesOn(lay) {
		t.Fatal("a stick's program must not update itself")
	}
	_ = os.MkdirAll(lay.data, 0o755)
	_ = os.WriteFile(filepath.Join(lay.data, "no-auto-update"), nil, 0o644)
	if appAutoUpdatesOn(lay) {
		t.Fatal("no-auto-update must turn it off on a stick too")
	}

	version = "usb-0.1.0-beta.11" // orOS USB's launcher
	lay = stickLayout(t, "8.0.0")
	if appAutoUpdatesOn(lay) {
		t.Fatal("orOS USB without persistence (no OROS_APP_AUTO_UPDATE) must not download orOS at every start")
	}
	t.Setenv("OROS_APP_AUTO_UPDATE", "1")
	if !appAutoUpdatesOn(lay) {
		t.Fatal("orOS USB with persistence must update its orOS")
	}
	t.Setenv("OROS_NO_AUTO_UPDATE", "1")
	if appAutoUpdatesOn(lay) {
		t.Fatal("OROS_NO_AUTO_UPDATE must win")
	}

	version = "0.0.0-abc1234"
	t.Setenv("OROS_NO_AUTO_UPDATE", "")
	t.Setenv("OROS_APP_AUTO_UPDATE", "")
	if appAutoUpdatesOn(stickLayout(t, "8.0.0")) {
		t.Fatal("test builds must not update orOS")
	}
}

// On a stick (and orOS USB, whose app/ is a link to the image's orOS) the
// new orOS is staged next to app/ and replaces it at the next start.
func TestPortableStagesOrOSAndSwapsItInAtNextStart(t *testing.T) {
	lay := stickLayout(t, "2.60.00")
	image := filepath.Join(t.TempDir(), "image-app")
	writeApp(t, image, "2.60.00")
	cur := filepath.Join(lay.root, "app")
	_ = os.RemoveAll(cur)
	if err := os.Symlink(image, cur); err != nil {
		t.Skip("no symlinks here")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sha":
			_, _ = w.Write([]byte(testCommit))
		case "/a/" + testCommit + ".zip":
			_, _ = w.Write(orosZip(t, "2.70.00"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	fakeOrOSArchive(t, srv.URL)

	if err := checkAppUpdate(lay); err != nil {
		t.Fatal(err)
	}
	if appVersion(lay.appDir()) != "2.60.00" || !stagedAppWaiting(lay) {
		t.Fatal("the new orOS must wait in app.next until the next start")
	}
	applyStagedApp(lay)
	if appVersion(lay.appDir()) != "2.70.00" || appCommit(lay.appDir()) != testCommit {
		t.Fatalf("after the next start the stick serves %s", appVersion(lay.appDir()))
	}
	if st, err := os.Lstat(cur); err != nil || st.Mode()&os.ModeSymlink != 0 {
		t.Fatal("app/ must now be the downloaded copy, not the link to the image")
	}
	if appVersion(image) != "2.60.00" {
		t.Fatal("the image's orOS must not be touched")
	}
}

func TestAppUpdateTakesOnlyTheCommitGitHubNamed(t *testing.T) {
	lay := installedLayout(t)
	other := strings.Repeat("a", 40)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sha":
			_, _ = w.Write([]byte(other))
		case "/a/" + other + ".zip":
			_, _ = w.Write(orosZip(t, "2.80.00")) // names testCommit
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	fakeOrOSArchive(t, srv.URL)
	if err := checkAppUpdate(lay); err == nil {
		t.Fatal("an archive that names another commit must be refused")
	}
	if stagedAppWaiting(lay) {
		t.Fatal("nothing may be staged from it")
	}
	if _, err := os.Stat(stagedAppDir(lay) + ".tmp"); err == nil {
		t.Fatal("the refused download must be removed")
	}
}

func TestAppUpdateNeverGoesBackToAnOlderOrOS(t *testing.T) {
	lay := installedLayout(t) // 2.70.00
	var zips int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sha":
			_, _ = w.Write([]byte(testCommit))
		case "/a/" + testCommit + ".zip":
			atomic.AddInt32(&zips, 1)
			_, _ = w.Write(orosZip(t, "2.60.00"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	fakeOrOSArchive(t, srv.URL)
	if err := checkAppUpdate(lay); err != nil {
		t.Fatal(err)
	}
	if stagedAppWaiting(lay) {
		t.Fatal("an older orOS must not be staged")
	}
	if err := checkAppUpdate(lay); err != nil || zips != 1 {
		t.Fatalf("the refused commit was downloaded again (%d downloads, %v)", zips, err)
	}

	// A staged copy older than what runs is dropped at the start.
	writeApp(t, stagedAppDir(lay), "1.0.0")
	applyStagedApp(lay)
	if appVersion(lay.appDir()) != "2.70.00" || stagedAppWaiting(lay) {
		t.Fatal("an older staged orOS must be dropped, not applied")
	}
}

func TestUnpackRefusesWhatIsNotOrOS(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("x-main/index.html")
	_, _ = w.Write([]byte("<!doctype html>"))
	_ = zw.Close()
	z := filepath.Join(t.TempDir(), "a.zip")
	_ = os.WriteFile(z, buf.Bytes(), 0o644)
	if err := unpackApp(z, filepath.Join(t.TempDir(), "app.new")); err == nil {
		t.Fatal("an archive without shell.js's APP_VERSION is not orOS")
	}
}

func TestUnpackLeavesOutLinksAndExecBits(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"oros-main/index.html": "<!doctype html>",
		"oros-main/shell.js":   `var APP_VERSION = "9.0.0";`,
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	h := &zip.FileHeader{Name: "oros-main/run.sh"}
	h.SetMode(0o755)
	w, _ := zw.CreateHeader(h)
	_, _ = w.Write([]byte("#!/bin/sh\n"))
	l := &zip.FileHeader{Name: "oros-main/link"}
	l.SetMode(os.ModeSymlink | 0o777)
	w, _ = zw.CreateHeader(l)
	_, _ = w.Write([]byte("/etc/passwd"))
	_ = zw.Close()
	z := filepath.Join(t.TempDir(), "a.zip")
	_ = os.WriteFile(z, buf.Bytes(), 0o644)
	dest := filepath.Join(t.TempDir(), "app.new")
	if err := unpackApp(z, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(dest, "link")); err == nil {
		t.Fatal("a link in the archive was unpacked")
	}
	if st, err := os.Stat(filepath.Join(dest, "run.sh")); err != nil || st.Mode()&0o111 != 0 {
		t.Fatalf("files are written without execute bits (%v)", st.Mode())
	}
}
