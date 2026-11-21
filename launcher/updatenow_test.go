package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	shaOld = "1111111111111111111111111111111111111111"
	shaNew = "2222222222222222222222222222222222222222"
)

func writeTestApp(t *testing.T, dir, ver, sha string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("x"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "shell.js"), []byte(`const APP_VERSION = "`+ver+`";`), 0o644)
	_ = os.WriteFile(filepath.Join(dir, commitFile), []byte(sha+"\n"), 0o644)
}

// fakeUpdate replaces GitHub: latest is the newest commit (or err), and a
// download brings orOS 9.0.0 at shaNew. It counts the downloads.
func fakeGitHubUpdate(t *testing.T, latest string, latestErr, fetchErr error) *int {
	t.Helper()
	oldL, oldF := updateLatest, updateFetch
	t.Cleanup(func() { updateLatest, updateFetch = oldL, oldF })
	n := 0
	updateLatest = func() (string, error) { return latest, latestErr }
	updateFetch = func(root string) error {
		n++
		if fetchErr != nil {
			return fetchErr
		}
		next := filepath.Join(root, "app.new")
		writeTestApp(t, next, "9.0.0", shaNew)
		return swapApp(root, next)
	}
	return &n
}

func updateTestLayout(t *testing.T) layout {
	d := t.TempDir()
	root, data := filepath.Join(d, "prog"), filepath.Join(d, "data")
	writeTestApp(t, filepath.Join(root, "app"), "8.0.0", shaOld)
	_ = os.MkdirAll(data, 0o755)
	return layout{root: root, data: data, updates: data, installed: true}
}

func TestUpdateNowSaysUpToDateWithoutDownloading(t *testing.T) {
	lay := updateTestLayout(t)
	n := fakeGitHubUpdate(t, shaOld, nil, nil)
	msg, err := updateNow(lay)
	if err != nil || *n != 0 {
		t.Fatalf("up to date: %v, %d downloads", err, *n)
	}
	if !strings.Contains(msg, "8.0.0") || msg != upToDateText("8.0.0") {
		t.Fatalf("message: %q", msg)
	}
}

func TestUpdateNowDownloadsANewerOrOS(t *testing.T) {
	lay := updateTestLayout(t)
	n := fakeGitHubUpdate(t, shaNew, nil, nil)
	msg, err := updateNow(lay)
	if err != nil || *n != 1 {
		t.Fatalf("newer: %v, %d downloads", err, *n)
	}
	if appVersion(lay.appDir()) != "9.0.0" || msg != updatedText("9.0.0") {
		t.Fatalf("now %s, message %q", appVersion(lay.appDir()), msg)
	}
	if b, _ := os.ReadFile(filepath.Join(lay.data, updateLogFile)); !strings.Contains(string(b), "9.0.0 is now in use") {
		t.Fatalf("update.log: %s", b)
	}
}

func TestUpdateNowSwapsInWhatTheBackgroundCheckDownloaded(t *testing.T) {
	lay := updateTestLayout(t)
	writeTestApp(t, stagedAppDir(lay), "9.0.0", shaNew)
	n := fakeGitHubUpdate(t, shaNew, nil, nil)
	msg, err := updateNow(lay)
	if err != nil || *n != 0 {
		t.Fatalf("staged: %v, %d downloads", err, *n)
	}
	if appVersion(lay.appDir()) != "9.0.0" || stagedAppWaiting(lay) || msg != updatedText("9.0.0") {
		t.Fatalf("now %s, staged %v, message %q", appVersion(lay.appDir()), stagedAppWaiting(lay), msg)
	}
}

func TestUpdateNowDropsAnOlderStagedOrOS(t *testing.T) {
	lay := updateTestLayout(t)
	writeTestApp(t, stagedAppDir(lay), "8.5.0", "3333333333333333333333333333333333333333")
	fakeGitHubUpdate(t, shaNew, nil, nil)
	if _, err := updateNow(lay); err != nil {
		t.Fatal(err)
	}
	if stagedAppWaiting(lay) {
		t.Fatal("an older app.next would swap 8.5.0 back in at the next start")
	}
}

func TestUpdateNowDownloadsWhenGitHubCannotBeAsked(t *testing.T) {
	lay := updateTestLayout(t)
	n := fakeGitHubUpdate(t, "", errors.New("latest commit: 403 rate limit"), nil)
	if _, err := updateNow(lay); err != nil || *n != 1 {
		t.Fatalf("%v, %d downloads", err, *n)
	}
}

func TestUpdateNowReportsAFailedDownload(t *testing.T) {
	lay := updateTestLayout(t)
	fakeGitHubUpdate(t, shaNew, nil, errors.New("connection aborted"))
	_, err := updateNow(lay)
	if err == nil || !strings.Contains(updateFailedText(err), "connection aborted") {
		t.Fatalf("err %v", err)
	}
	if appVersion(lay.appDir()) != "8.0.0" {
		t.Fatal("the running orOS must stay")
	}
	if b, _ := os.ReadFile(filepath.Join(lay.data, updateLogFile)); !strings.Contains(string(b), "failed: connection aborted") {
		t.Fatalf("update.log: %s", b)
	}
}

type fakeMenuItem struct{ titles []string }

func (f *fakeMenuItem) SetTitle(s string) { f.titles = append(f.titles, s) }
func (f *fakeMenuItem) Enable()           {}
func (f *fakeMenuItem) Disable()          {}

func TestTrayCheckRunsOnceAndServesTheNewOrOS(t *testing.T) {
	lay := updateTestLayout(t)
	n := fakeGitHubUpdate(t, shaNew, nil, nil)
	oldN := notifyFn
	defer func() { notifyFn = oldN }()
	var said []string
	notifyFn = func(s string) { said = append(said, s) }
	tr := &trayApp{lay: lay}
	tr.dir.Store(lay.appDir())

	updateBusy.Store(true) // the endpoint is at it: the click does nothing
	it := &fakeMenuItem{}
	tr.checkForUpdates(it)
	if *n != 0 || len(it.titles) != 0 {
		t.Fatal("a second update ran at the same time")
	}
	updateBusy.Store(false)

	tr.checkForUpdates(it)
	if *n != 1 || updateBusy.Load() {
		t.Fatalf("%d downloads, busy %v", *n, updateBusy.Load())
	}
	if tr.dir.Load().(string) != filepath.Join(lay.data, "app") {
		t.Fatalf("the tray still serves %s", tr.dir.Load())
	}
	if len(it.titles) != 2 || it.titles[0] != checkUpdatesTitle(true) || it.titles[1] != checkUpdatesTitle(false) {
		t.Fatalf("titles %q", it.titles)
	}
	if len(said) != 1 || said[0] != updatedText("9.0.0") {
		t.Fatalf("said %q", said)
	}
}

func TestCheckForUpdatesWordsInThreeLanguages(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the language comes from Windows itself")
	}
	seen := map[string]bool{}
	for _, l := range []string{"en_US.UTF-8", "el_GR.UTF-8", "de_DE.UTF-8"} {
		t.Setenv("LC_ALL", l)
		s := checkUpdatesTitle(false)
		if s == "" || seen[s] || checkUpdatesTitle(true) == s || seen[upToDateText("1")] {
			t.Fatalf("%s: %q", l, s)
		}
		seen[s], seen[upToDateText("1")] = true, true
	}
}
