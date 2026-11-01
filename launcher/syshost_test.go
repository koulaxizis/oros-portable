package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeOps stands in for the computer.
type fakeOps struct {
	vol, bright int
	muted       bool
	powered     []string
	noScreen    bool
	dl          string
}

func (f *fakeOps) volume() (int, bool, bool)  { return f.vol, f.muted, true }
func (f *fakeOps) setVolume(v int) error      { f.vol = v; return nil }
func (f *fakeOps) setMute(on bool) error      { f.muted = on; return nil }
func (f *fakeOps) battery() (int, bool, bool) { return 64, true, true }
func (f *fakeOps) brightness() (int, bool)    { return f.bright, !f.noScreen }
func (f *fakeOps) setBrightness(v int) error {
	if f.noScreen {
		return errors.New("no backlight")
	}
	f.bright = v
	return nil
}
func (f *fakeOps) power(a string) error { f.powered = append(f.powered, a); return nil }
func (f *fakeOps) downloads() string    { return f.dl }

func withFake(t *testing.T) *fakeOps {
	t.Helper()
	f := &fakeOps{vol: 40, bright: 70, dl: t.TempDir()}
	old := sysPlatform
	sysPlatform = f
	t.Cleanup(func() { sysPlatform = old })
	withFakeNet(t, &fakeNet{}) // no Wi-Fi unless a test gives one
	return f
}

func portableLayout(t *testing.T) layout {
	t.Helper()
	root := t.TempDir()
	writeApp(t, filepath.Join(root, "app"), "4.61.00")
	return newLayout(root)
}

// frame and unframe speak Chromium's native messaging format.
func frame(t *testing.T, msgs ...any) *bytes.Buffer {
	var b bytes.Buffer
	for _, m := range msgs {
		j, _ := json.Marshal(m)
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(j)))
		b.Write(j)
	}
	return &b
}

func unframe(t *testing.T, b *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for b.Len() > 0 {
		var n uint32
		if err := binary.Read(b, binary.LittleEndian, &n); err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b.Next(int(n)), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func TestHostCallIsRecognised(t *testing.T) {
	if !isHostCall([]string{"orOS", "chrome-extension://" + extID + "/", "--parent-window=0"}) {
		t.Fatal("Chromium's call was not recognised")
	}
	for _, a := range [][]string{{"orOS"}, {"orOS", "tray"}, {"orOS", "http://x"}} {
		if isHostCall(a) {
			t.Fatalf("%v taken for a host call", a)
		}
	}
}

func TestHostAnswersFramedMessages(t *testing.T) {
	f := withFake(t)
	lay := portableLayout(t)
	in := frame(t, sysRequest{Cmd: "status"}, sysRequest{Cmd: "volume.set", Value: 130}, map[string]any{"cmd": "wifi.scan"})
	in.Write([]byte{3, 0, 0, 0, '{', 'x', '}'}) // not JSON
	var out bytes.Buffer
	if err := runSysHost(lay, in, &out); err != nil {
		t.Fatal(err)
	}
	r := unframe(t, &out)
	if len(r) != 4 {
		t.Fatalf("%d answers", len(r))
	}
	st := r[0]
	if st["ok"] != true || st["platform"] != "portable" || st["volume"] != 40.0 || st["brightness"] != 70.0 || st["window"] != "fullscreen" {
		t.Fatalf("status: %v", st)
	}
	caps := st["caps"].(map[string]any)
	if caps["update"] != true || caps["window"] != true || caps["exit"] != true || caps["wifi"] != false {
		t.Fatalf("caps: %v", caps)
	}
	if r[1]["ok"] != true || f.vol != 100 {
		t.Fatalf("volume is clamped to 100: %v %d", r[1], f.vol)
	}
	if r[2]["ok"] != false || r[3]["ok"] != false || r[3]["error"] != "bad request" {
		t.Fatalf("unsupported and bad requests: %v %v", r[2], r[3])
	}
}

func TestHostRefusesHugeMessage(t *testing.T) {
	var in bytes.Buffer
	_ = binary.Write(&in, binary.LittleEndian, uint32(2<<20))
	if err := runSysHost(portableLayout(t), &in, &bytes.Buffer{}); err == nil {
		t.Fatal("a 2 MB message must be refused")
	}
}

func TestHandleSysCommands(t *testing.T) {
	f := withFake(t)
	lay := portableLayout(t)
	if r := handleSys(lay, sysRequest{Cmd: "brightness.set", Value: 1}); r["ok"] != true || f.bright != 5 {
		t.Fatalf("brightness never goes below 5: %v %d", r, f.bright)
	}
	if r := handleSys(lay, sysRequest{Cmd: "volume.mute", On: true}); r["ok"] != true || !f.muted {
		t.Fatal("mute")
	}
	for _, c := range []string{"power.sleep", "power.reboot", "power.off"} {
		if r := handleSys(lay, sysRequest{Cmd: c}); r["ok"] != true {
			t.Fatalf("%s: %v", c, r)
		}
	}
	if strings.Join(f.powered, ",") != "sleep,reboot,off" {
		t.Fatalf("power: %v", f.powered)
	}
	if r := handleSys(lay, sysRequest{Cmd: "power.halt"}); r["ok"] != false {
		t.Fatal("unknown power command accepted")
	}
	f.noScreen = true
	if r := handleSys(lay, sysRequest{Cmd: "brightness.set", Value: 50}); r["ok"] != false {
		t.Fatal("a failed brightness change must say so")
	}
	if _, ok := handleSys(lay, sysRequest{Cmd: "status"})["brightness"]; ok {
		t.Fatal("a screen without brightness must not report one")
	}
}

func TestPingTouchesFile(t *testing.T) {
	withFake(t)
	lay := portableLayout(t)
	handleSys(lay, sysRequest{Cmd: "ping"})
	st, err := os.Stat(filepath.Join(lay.data, pingFile))
	if err != nil || time.Since(st.ModTime()) > time.Minute {
		t.Fatalf("ping: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(lay.data, pingFile), old, old)
	handleSys(lay, sysRequest{Cmd: "ping"})
	st, _ = os.Stat(filepath.Join(lay.data, pingFile))
	if time.Since(st.ModTime()) > time.Minute {
		t.Fatal("a new ping must renew the time")
	}
}

func TestWindowModeIsKept(t *testing.T) {
	withFake(t)
	lay := portableLayout(t)
	if !loadSession(lay).Fullscreen {
		t.Fatal("the stick opens in full screen")
	}
	if r := handleSys(lay, sysRequest{Cmd: "window.mode", Mode: "window"}); r["ok"] != true {
		t.Fatal(r)
	}
	if loadSession(lay).Fullscreen || handleSys(lay, sysRequest{Cmd: "status"})["window"] != "window" {
		t.Fatal("the window choice was not kept")
	}
	if r := handleSys(lay, sysRequest{Cmd: "window.mode", Mode: "kiosk"}); r["ok"] != false {
		t.Fatal("unknown mode accepted")
	}
	inst := installedLayout(t)
	if loadSession(inst).Fullscreen {
		t.Fatal("the desktop apps open in a window")
	}
	st := handleSys(inst, sysRequest{Cmd: "status"})
	if st["caps"].(map[string]bool)["update"] {
		t.Fatal("installed copies update themselves; the panel must not offer it")
	}
	if r := handleSys(inst, sysRequest{Cmd: "update.start"}); r["ok"] != false {
		t.Fatal("update.start on an installed copy")
	}
}

func TestBrowserArgs(t *testing.T) {
	lay := portableLayout(t)
	t.Cleanup(unregisterHost)
	c := candidate{path: "chrome", profile: filepath.Join(lay.data, "profile"), bundled: true}
	if a := sysBrowserArgs(lay, c, appURL); a != nil {
		t.Fatalf("the lite edition has no extension: %v", a)
	}
	ext := filepath.Join(lay.root, sysExtFolder)
	must(t, os.MkdirAll(ext, 0o755))
	must(t, os.WriteFile(filepath.Join(ext, "manifest.json"), []byte("{}"), 0o644))

	a := strings.Join(sysBrowserArgs(lay, c, appURL), " ")
	if !strings.Contains(a, "--load-extension="+ext) || !strings.Contains(a, "--start-fullscreen") {
		t.Fatalf("main window: %s", a)
	}
	if a := strings.Join(sysBrowserArgs(lay, c, "http://localhost:47800"+quickPath), " "); strings.Contains(a, "fullscreen") || !strings.Contains(a, "--load-extension") {
		t.Fatalf("the small window loads the extension but never goes full screen: %s", a)
	}
	must(t, saveSession(lay, session{Fullscreen: false}))
	if a := strings.Join(sysBrowserArgs(lay, c, appURL), " "); strings.Contains(a, "fullscreen") {
		t.Fatalf("window mode: %s", a)
	}
	if a := sysBrowserArgs(lay, candidate{path: "msedge", profile: c.profile}, appURL); a != nil {
		t.Fatalf("other browsers refuse --load-extension: %v", a)
	}
	// The orOS add-on and uBlock Origin Lite load with it, in one list.
	addon, ubol := filepath.Join(lay.root, addonDir), filepath.Join(lay.root, ubolDir)
	for _, d := range []string{addon, ubol} {
		must(t, os.MkdirAll(d, 0o755))
		must(t, os.WriteFile(filepath.Join(d, "manifest.json"), []byte("{}"), 0o644))
	}
	if a := sysBrowserArgs(lay, c, appURL); len(a) != 1 || a[0] != "--load-extension="+ext+","+addon+","+ubol {
		t.Fatalf("extensions: %v", a)
	}
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile(filepath.Join(c.profile, "NativeMessagingHosts", hostName+".json"))
		if err != nil {
			t.Fatal("the host was not registered in the profile")
		}
		var m map[string]any
		must(t, json.Unmarshal(b, &m))
		if m["name"] != hostName || m["allowed_origins"].([]any)[0] != "chrome-extension://"+extID+"/" {
			t.Fatalf("manifest: %s", b)
		}
	}
}

func TestUpdateProgressGoesStale(t *testing.T) {
	lay := portableLayout(t)
	writeProgress(lay, updateProgress{Running: true, MB: 12})
	p, ok := readProgress(lay)
	if !ok || !p.Running || p.MB != 12 {
		t.Fatalf("%+v", p)
	}
	b, _ := json.Marshal(updateProgress{Running: true, At: time.Now().Add(-5 * time.Minute).Unix()})
	must(t, os.WriteFile(progressPath(lay), b, 0o644))
	p, _ = readProgress(lay)
	if p.Running || !p.Done || p.Error == "" {
		t.Fatalf("a stopped download must not look running: %+v", p)
	}
	if r := updateStatus(lay); r["running"] != false {
		t.Fatal(r)
	}
}

func TestStageUpdateForPortable(t *testing.T) {
	lay := portableLayout(t)
	body := orosZip(t, "4.62.00")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sha" {
			_, _ = w.Write([]byte(testCommit))
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	oldC, oldU := commitAPI, updateURL
	commitAPI, updateURL = srv.URL+"/sha", srv.URL+"/main.zip"
	defer func() { commitAPI, updateURL = oldC, oldU }()

	if r := updateCheck(lay); r["ok"] != true || r["available"] != true || r["staged"] != false {
		t.Fatalf("check: %v", r)
	}
	stageUpdate(lay)
	if p, _ := readProgress(lay); !p.Done || p.Error != "" || p.Running {
		t.Fatalf("progress: %+v", p)
	}
	if r := updateCheck(lay); r["available"] != false || r["staged"] != true {
		t.Fatalf("after staging: %v", r)
	}
	if lay.appDir() != filepath.Join(lay.root, "app") || appVersion(lay.appDir()) != "4.61.00" {
		t.Fatal("the running orOS must not change under the window")
	}
	applyStagedApp(lay)
	if appVersion(lay.appDir()) != "4.62.00" {
		t.Fatalf("the next start serves %s", appVersion(lay.appDir()))
	}
}

func TestReportZip(t *testing.T) {
	f := withFake(t)
	lay := portableLayout(t)
	must(t, os.MkdirAll(lay.data, 0o755))
	must(t, os.WriteFile(filepath.Join(lay.data, "launcher.log"), []byte("started"), 0o644))
	r := handleSys(lay, sysRequest{Cmd: "report"})
	if r["ok"] != true {
		t.Fatal(r)
	}
	z, err := zip.OpenReader(filepath.Join(f.dl, r["file"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var names []string
	for _, e := range z.File {
		names = append(names, e.Name)
	}
	if got := strings.Join(names, ","); got != "about.txt,launcher.log" {
		t.Fatalf("report holds %s", got)
	}
}

func TestWatchdog(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses sleep")
	}
	lay := portableLayout(t)
	t.Cleanup(unregisterHost)
	ext := filepath.Join(lay.root, sysExtFolder)
	must(t, os.MkdirAll(ext, 0o755))
	must(t, os.WriteFile(filepath.Join(ext, "manifest.json"), []byte("{}"), 0o644))
	c := candidate{path: "sleep", profile: filepath.Join(lay.data, "profile"), bundled: true}
	old := watchdogWait
	watchdogWait = 2 * time.Second
	defer func() { watchdogWait = old }()

	// Never pinged on this stick: not armed.
	start := time.Now()
	if gaveUp, err := runWatched(lay, c, appURL, exec.Command("sleep", "3"), true); gaveUp || err != nil || time.Since(start) < 3*time.Second {
		t.Fatalf("unarmed watchdog: %v %v", gaveUp, err)
	}
	// Pinged before, silent now: it gives up.
	old2 := time.Now().Add(-time.Hour)
	must(t, os.WriteFile(filepath.Join(lay.data, pingFile), nil, 0o644))
	must(t, os.Chtimes(filepath.Join(lay.data, pingFile), old2, old2))
	if gaveUp, _ := runWatched(lay, c, appURL, exec.Command("sleep", "30"), true); !gaveUp {
		t.Fatal("a silent window was not ended")
	}
	// A ping in time: it waits for the window.
	cmd := exec.Command("sleep", "4")
	go func() { time.Sleep(time.Second); touch(filepath.Join(lay.data, pingFile)) }()
	if gaveUp, err := runWatched(lay, c, appURL, cmd, true); gaveUp || err != nil {
		t.Fatalf("a living window was ended: %v", err)
	}
	// The second try (without the GPU) is never watched.
	must(t, os.Chtimes(filepath.Join(lay.data, pingFile), old2, old2))
	if gaveUp, _ := runWatched(lay, c, appURL, exec.Command("sleep", "3"), false); gaveUp {
		t.Fatal("the second try was watched")
	}
}

func TestInternetWindow(t *testing.T) {
	withFake(t)
	lay := portableLayout(t)
	t.Setenv("OROS_PORTABLE_BROWSER", "")
	if handleSys(lay, sysRequest{Cmd: "status"})["caps"].(map[string]bool)["web"] {
		t.Fatal("the lite edition offers no Internet window")
	}
	if r := handleSys(lay, sysRequest{Cmd: "web.open"}); r["ok"] != false {
		t.Fatal("web.open without a bundled browser")
	}
	t.Setenv("OROS_PORTABLE_BROWSER", "/stick/browser/chrome")
	var got []string
	old := webStart
	webStart = func(name string, args ...string) error { got = append([]string{name}, args...); return nil }
	defer func() { webStart = old }()
	if !handleSys(lay, sysRequest{Cmd: "status"})["caps"].(map[string]bool)["web"] {
		t.Fatal("the Internet window is missing from caps")
	}
	if r := handleSys(lay, sysRequest{Cmd: "web.open"}); r["ok"] != true {
		t.Fatal(r)
	}
	// orOS's own profile, so the window joins orOS's browser and the
	// orOS add-on there reaches Passwords.
	a := strings.Join(got, " ")
	if got[0] != "/stick/browser/chrome" || !strings.Contains(a, "--user-data-dir="+filepath.Join(lay.data, "profile")+" ") ||
		strings.Contains(a, "--app") || strings.Contains(a, "load-extension") || !strings.HasSuffix(a, webHome) {
		t.Fatalf("Internet window: %s", a)
	}
	if _, err := os.Stat(filepath.Join(lay.data, "web")); err == nil {
		t.Fatal("the old web profile is not made any more")
	}
	sys, addon, ubol := filepath.Join(lay.root, sysExtFolder), filepath.Join(lay.root, addonDir), filepath.Join(lay.root, ubolDir)
	for _, d := range []string{addon, ubol} {
		must(t, os.MkdirAll(d, 0o755))
		must(t, os.WriteFile(filepath.Join(d, "manifest.json"), []byte("{}"), 0o644))
	}
	handleSys(lay, sysRequest{Cmd: "web.open"})
	if a := strings.Join(got, " "); !strings.Contains(a, " --load-extension="+addon+","+ubol+" ") {
		t.Fatalf("the add-on and uBlock Origin Lite not loaded: %s", a)
	}
	// The same list as orOS's window, with what OROS_PORTABLE_BROWSER_ARGS
	// brings merged in (Chromium reads one flag only).
	must(t, os.MkdirAll(sys, 0o755))
	must(t, os.WriteFile(filepath.Join(sys, "manifest.json"), []byte("{}"), 0o644))
	t.Setenv("OROS_PORTABLE_BROWSER_ARGS", "--headless=new --load-extension=/opt/x,"+ubol)
	handleSys(lay, sysRequest{Cmd: "web.open"})
	a = strings.Join(got, " ")
	if strings.Count(a, "--load-extension") != 1 || !strings.Contains(a, " --load-extension="+sys+","+addon+","+ubol+",/opt/x --headless=new ") {
		t.Fatalf("merged extensions: %s", a)
	}
}

func TestJoinExtensions(t *testing.T) {
	for _, c := range []struct{ in, out string }{
		{"--a --b", "--a --b"},
		{"--a --load-extension=/x --b", "--a --load-extension=/x --b"},
		{"--a --load-extension=/x --b --load-extension=/y,/x --c", "--a --load-extension=/x,/y --b --c"},
		{"--load-extension= --a", "--a"},
	} {
		if got := strings.Join(joinExtensions(strings.Fields(c.in)), " "); got != c.out {
			t.Errorf("%q: %q, want %q", c.in, got, c.out)
		}
	}
}

func TestReportZipWithOrosPart(t *testing.T) {
	f := withFake(t)
	lay := portableLayout(t)
	must(t, os.MkdirAll(lay.data, 0o755))
	must(t, os.WriteFile(filepath.Join(lay.data, "launcher.log"), []byte("started"), 0o644))
	r := handleSys(lay, sysRequest{Cmd: "report", Web: "orOS problem report\nversion: 4.67.00\n"})
	if r["ok"] != true {
		t.Fatal(r)
	}
	z, err := zip.OpenReader(filepath.Join(f.dl, r["file"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var names []string
	var web string
	for _, e := range z.File {
		names = append(names, e.Name)
		if e.Name == "oros.txt" {
			rc, err := e.Open()
			must(t, err)
			b, _ := io.ReadAll(rc)
			rc.Close()
			web = string(b)
		}
	}
	if got := strings.Join(names, ","); got != "about.txt,oros.txt,launcher.log" {
		t.Fatalf("report holds %s", got)
	}
	if !strings.Contains(web, "version: 4.67.00") {
		t.Fatalf("oros.txt: %q", web)
	}
}
