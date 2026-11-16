package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Gate host (orOS GATE-HOST, PEND-1 wave 2 "b"; Chris 2026-11-16 «Ναι,
// μετά το a»). With the owner's opt-in (orOS › Settings › Connected
// services › orOS Gate), the tray of an installed orOS keeps this user's
// orOS open in the background, with no window, while every orOS window
// is closed: the bundled browser runs headless on the same profile at
// /?gate-host=<nonce>, so orOS Gate answers Claude live and applies its
// waiting changes at once. The launcher never reads orOS data; it only
// starts and stops that browser.
//
//   - Opt-in: <user folder>/gate-host.json {"on": true}. Off by default.
//     Only an installed copy with the bundled browser and without users
//     (users.go) can run it; the guest never.
//   - The page asks GET /__oros/gate-host (can, on, running, tray) and
//     turns it on or off with POST {"on": bool} (same origin, X-Oros-Gate: 1).
//     The headless page proves it is the one the tray started with
//     GET /__oros/gate-host?n=<nonce> ("host": true only for the nonce in
//     gate-host.run). Only then does orOS let the Gate work over its
//     screen lock (gate/gate.js HOST).
//   - One profile, one browser: every window start first stops the
//     headless browser (gateHostYield, from windowArgs) and marks a hold
//     for a minute, so the tray does not start it again under a window
//     that is still starting. The tray starts it again once no profile is
//     in use and the hold is over.
//   - Turning it on also starts the tray and "Start with the computer",
//     so it survives a restart. OROS_NO_GATE_HOST=1 hides it.

const (
	gateHostPath    = "/__oros/gate-host"
	gateHostFile    = "gate-host.json"
	gateHostRun     = "gate-host.run"
	gateHostHold    = "gate-host.hold"
	gateHostBye     = "gate-host.bye"
	gateHostEvery   = 20 * time.Second
	gateHostHoldFor = time.Minute
)

type gateHostPref struct {
	On bool  `json:"on"`
	M  int64 `json:"m"`
}

type gateHostRunRec struct {
	Pid   int    `json:"pid"`
	Nonce string `json:"nonce"`
	At    int64  `json:"at"`
}

func gateHostDisabled() bool { return os.Getenv("OROS_NO_GATE_HOST") != "" }

// gateHostCan: this copy offers the switch: an installed copy with the
// bundled browser (background orOS + the lock rule), or orOS USB, whose
// window never closes (the lock rule only). Not with users.
func gateHostCan(lay layout) bool {
	if gateHostDisabled() || loadUsers(lay.data).enabled() {
		return false
	}
	return gateHostBg(lay) || gateHostUSB()
}

// gateHostBg: the tray can run orOS in the background here.
func gateHostBg(lay layout) bool {
	return lay.installed && !gateHostUSB() && gateHostBrowser(lay) != ""
}

// gateHostUSB: orOS USB's session (oros-session sets it).
func gateHostUSB() bool { return os.Getenv("OROS_COMPUTER_FILES") == "1" }

// gateHostWin: the window's own nonce (gate-host.win), so the window's
// orOS may let the Gate work behind its screen lock. A new one at each
// window start.
const gateHostWin = "gate-host.win"

// gateHostURL adds ?gate-host=<nonce> to the main window's address when
// the owner turned the switch on for the user folder dir.
func gateHostURL(dir, url string) string {
	if gateHostDisabled() || !readGateHostPref(dir).On || url != appURL {
		return url
	}
	n := newGateNonce()
	if n == "" || writeFileAtomic(filepath.Join(dir, gateHostWin), []byte(n)) != nil {
		return url
	}
	return url + "?gate-host=" + n
}

// gateHostBrowser: the bundled browser (or the tests' one).
func gateHostBrowser(lay layout) string {
	if p := os.Getenv("OROS_PORTABLE_BROWSER"); p != "" {
		return p
	}
	if l := bundledBrowsers(lay.root); len(l) > 0 {
		return l[0]
	}
	return ""
}

func readGateHostPref(dir string) gateHostPref {
	var p gateHostPref
	b, err := os.ReadFile(filepath.Join(dir, gateHostFile))
	if err == nil {
		_ = json.Unmarshal(b, &p)
	}
	return p
}

func writeGateHostPref(dir string, on bool) error {
	b, _ := json.Marshal(gateHostPref{On: on, M: time.Now().UnixMilli()})
	return writeFileAtomic(filepath.Join(dir, gateHostFile), b)
}

func readGateHostRun(dir string) (gateHostRunRec, bool) {
	var r gateHostRunRec
	b, err := os.ReadFile(filepath.Join(dir, gateHostRun))
	if err != nil || json.Unmarshal(b, &r) != nil || r.Pid <= 0 || len(r.Nonce) < 16 {
		return gateHostRunRec{}, false
	}
	return r, true
}

func writeFileAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func newGateNonce() string {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// gateHostArgs: the bundled browser without a window, on the user's
// profile, kept awake (no timer throttling, so the Gate's socket stays).
func gateHostArgs(profile, nonce string) []string {
	args := []string{
		"--user-data-dir=" + profile,
		"--headless=new",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-background-networking",
		"--disable-features=Translate,MediaRouter,OptimizationHints",
		"--disable-background-timer-throttling",
		"--disable-renderer-backgrounding",
		"--disable-backgrounding-occluded-windows",
		"--mute-audio",
	}
	if runtime.GOOS == "linux" {
		args = append(args, "--password-store=basic")
	}
	args = append(args, extraArgs()...)
	return append(args, appURL+"?gate-host="+nonce)
}

// gateHostYield stops the background orOS of the user folder dir before
// a window starts on its profile, and holds it off for a minute. Safe to
// call when none runs.
func gateHostYield(dir string) {
	if gateHostDisabled() {
		return
	}
	held := time.Now()
	_ = os.WriteFile(filepath.Join(dir, gateHostHold), []byte(held.UTC().Format(time.RFC3339)), 0o600)
	r, ok := readGateHostRun(dir)
	if !ok {
		return
	}
	// The page stops its Gate work, waits until the browser has written
	// what it changed, and says bye (at most ~12 s); then it ends.
	log.Printf("gate host: handing the profile over to a window (pid %d)", r.Pid)
	for i := 0; i < 60; i++ {
		if st, err := os.Stat(filepath.Join(dir, gateHostBye)); err == nil && !st.ModTime().Before(held.Add(-time.Second)) {
			break
		}
		if !profileInUse(filepath.Join(dir, "profile")) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	killGateHost(r.Pid)
	profile := filepath.Join(dir, "profile")
	for i := 0; i < 40 && profileInUse(profile); i++ {
		time.Sleep(200 * time.Millisecond)
	}
	_ = os.Remove(filepath.Join(dir, gateHostRun))
}

func gateHostHeld(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, gateHostHold))
	return err == nil && time.Since(st.ModTime()) < gateHostHoldFor
}

// gateHostRunner lives in the tray: it keeps the background orOS
// running while it is on and no window holds the profile.
type gateHostRunner struct {
	lay   layout
	mu    sync.Mutex
	cmd   *exec.Cmd
	fails int
	since time.Time
	stop  chan struct{}
}

func newGateHostRunner(lay layout) *gateHostRunner {
	return &gateHostRunner{lay: lay, stop: make(chan struct{})}
}

func (g *gateHostRunner) running() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cmd != nil
}

func (g *gateHostRunner) loop(busy func() bool) {
	t := time.NewTicker(gateHostEvery)
	defer t.Stop()
	for {
		g.tick(busy)
		select {
		case <-g.stop:
			g.halt()
			return
		case <-t.C:
		}
	}
}

func (g *gateHostRunner) tick(busy func() bool) {
	dir := g.lay.userFolder()
	on := gateHostCan(g.lay) && gateHostBg(g.lay) && readGateHostPref(dir).On
	if !on {
		g.halt()
		return
	}
	if g.running() {
		return
	}
	profile := filepath.Join(dir, "profile")
	if busy() || gateHostHeld(dir) || anyProfileInUse(g.lay.data) {
		return // a window is open or starting
	}
	g.mu.Lock()
	if g.fails >= 3 && time.Since(g.since) < 30*time.Minute {
		g.mu.Unlock()
		return // it kept failing: try again in a while
	}
	g.mu.Unlock()
	nonce := newGateNonce()
	exe := gateHostBrowser(g.lay)
	if nonce == "" || exe == "" {
		return
	}
	seedNoSaving(profile)
	cmd := exec.Command(exe, gateHostArgs(profile, nonce)...)
	gateHostProcAttr(cmd)
	logf, _ := os.OpenFile(filepath.Join(g.lay.data, "gate-host.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if logf != nil {
		cmd.Stdout, cmd.Stderr = logf, logf
	}
	if err := cmd.Start(); err != nil {
		log.Printf("gate host: %v", err)
		if logf != nil {
			logf.Close()
		}
		g.failed()
		return
	}
	gateHostAfterStart(cmd)
	rec, _ := json.Marshal(gateHostRunRec{Pid: cmd.Process.Pid, Nonce: nonce, At: time.Now().UnixMilli()})
	_ = writeFileAtomic(filepath.Join(dir, gateHostRun), rec)
	log.Printf("gate host: background orOS started (pid %d)", cmd.Process.Pid)
	g.mu.Lock()
	g.cmd = cmd
	started := time.Now()
	g.mu.Unlock()
	go func() {
		err := cmd.Wait()
		if logf != nil {
			logf.Close()
		}
		g.mu.Lock()
		if g.cmd == cmd {
			g.cmd = nil
		}
		g.mu.Unlock()
		if r, ok := readGateHostRun(dir); ok && r.Nonce == nonce {
			_ = os.Remove(filepath.Join(dir, gateHostRun))
		}
		log.Printf("gate host: background orOS ended (%v)", err)
		if time.Since(started) < time.Minute && !gateHostHeld(dir) {
			g.failed()
		} else {
			g.mu.Lock()
			g.fails = 0
			g.mu.Unlock()
		}
	}()
}

func (g *gateHostRunner) failed() {
	g.mu.Lock()
	g.fails++
	g.since = time.Now()
	g.mu.Unlock()
}

// halt stops the background browser (Quit, off, an update).
func (g *gateHostRunner) halt() {
	g.mu.Lock()
	cmd := g.cmd
	g.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return
	}
	gateHostYield(g.lay.userFolder()) // the page says bye first, then it ends
}

// gateHostAPI answers orOS at /__oros/gate-host.
type gateHostAPI struct {
	lay     layout
	running func() bool
	tray    func() bool
	turnOn  func() // start the tray and "Start with the computer"
}

type gateHostState struct {
	Can     bool   `json:"can"`
	On      bool   `json:"on"`
	Running bool   `json:"running"`
	Tray    bool   `json:"tray"`
	Host    bool   `json:"host"`
	Kind    string `json:"kind,omitempty"`  // to the host: "bg" (no window) or "lock" (the window, behind its lock)
	Bg      bool   `json:"bg"`              // the background orOS can run here (not orOS USB)
	Yield   bool   `json:"yield,omitempty"` // to the host: a window wants the profile
	Win     string `json:"win,omitempty"`   // after turning on: the window\'s nonce
}

func (a *gateHostAPI) state(r *http.Request) gateHostState {
	dir := a.lay.userFolder()
	st := gateHostState{Can: gateHostCan(a.lay)}
	if !st.Can {
		return st
	}
	st.On = readGateHostPref(dir).On
	st.Bg = gateHostBg(a.lay)
	if n := r.URL.Query().Get("n"); n != "" && st.On {
		if b, err := os.ReadFile(filepath.Join(dir, gateHostWin)); err == nil && len(b) >= 16 && constEq(n, string(b)) {
			st.Host, st.Kind = true, "lock"
			return st
		}
	}
	rec, ok := readGateHostRun(dir)
	st.Running = ok && (a.running == nil || a.running() || profileInUse(filepath.Join(dir, "profile")))
	st.Tray = a.tray != nil && a.tray()
	if n := r.URL.Query().Get("n"); n != "" && ok && constEq(n, rec.Nonce) {
		st.Host = st.On && st.Bg
		st.Kind = "bg"
		st.Yield = gateHostHeld(dir) || !st.Host
		if r.URL.Query().Get("bye") == "1" {
			_ = os.WriteFile(filepath.Join(dir, gateHostBye), []byte("1"), 0o600)
		}
	}
	return st
}

func constEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var d byte
	for i := 0; i < len(a); i++ {
		d |= a[i] ^ b[i]
	}
	return d == 0
}

func (a *gateHostAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(a.state(r))
	case http.MethodPost:
		// Only orOS's own pages: same origin, our header, a small JSON body.
		if r.Header.Get("X-Oros-Gate") != "1" || r.Header.Get("Sec-Fetch-Site") != "same-origin" ||
			(r.Header.Get("Origin") != "" && !allowedOrigin(r.Header.Get("Origin"))) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		if !gateHostCan(a.lay) {
			http.Error(w, `{"error":"off"}`, http.StatusNotFound)
			return
		}
		var body struct {
			On *bool `json:"on"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 256)).Decode(&body); err != nil || body.On == nil {
			http.Error(w, `{"error":"bad"}`, http.StatusBadRequest)
			return
		}
		dir := a.lay.userFolder()
		if err := writeGateHostPref(dir, *body.On); err != nil {
			http.Error(w, `{"error":"storage"}`, http.StatusInternalServerError)
			return
		}
		log.Printf("gate host: turned %s", map[bool]string{true: "on", false: "off"}[*body.On])
		if *body.On && a.turnOn != nil && gateHostBg(a.lay) {
			a.turnOn()
		}
		if _, running := readGateHostRun(dir); !*body.On && running {
			go gateHostYield(dir) // the page stops at its next check, then it ends
		}
		st := a.state(r)
		if *body.On {
			// The window that turned it on gets its nonce at once (the lock
			// rule then holds without a restart; orOS USB's window rarely restarts).
			if n := newGateNonce(); n != "" && writeFileAtomic(filepath.Join(dir, gateHostWin), []byte(n)) == nil {
				st.Win = n
			}
		}
		_ = json.NewEncoder(w).Encode(st)
	default:
		http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
	}
}

func allowedOrigin(o string) bool {
	switch o {
	case fmt.Sprintf("http://localhost:%d", port), fmt.Sprintf("http://127.0.0.1:%d", port), fmt.Sprintf("http://[::1]:%d", port):
		return true
	}
	return false
}

// gateAPI serves /__oros/gate-host in every launcher that serves orOS;
// the tray sets running (its own runner).
var gateAPI = newGateHostAPIFromEnv()

func newGateHostAPIFromEnv() *gateHostAPI {
	root, err := rootDir()
	if err != nil {
		return nil
	}
	lay := newLayout(root)
	return &gateHostAPI{lay: lay, tray: func() bool { return trayRunning(lay) }, turnOn: func() { gateHostTrayOn(lay) }}
}

// trayRunning: a tray of this copy holds tray.lock.
func trayRunning(lay layout) bool {
	if !lay.installed {
		return false
	}
	unlock, free := lockTray(lay.data)
	if free {
		unlock()
	}
	return !free
}

// gateHostTrayOn: the background orOS needs the tray, now and after a
// restart: start it when none runs, and turn on "Start with the computer".
func gateHostTrayOn(lay layout) {
	exe := trayCommand()
	if exe == "" || os.Getenv("OROS_NO_TRAY") != "" {
		return
	}
	if !autostartOn() {
		if err := setAutostart(true, exe); err != nil {
			log.Printf("gate host: autostart: %v", err)
		}
	}
	if trayRunning(lay) {
		return
	}
	if err := spawnTray(exe); err != nil {
		log.Printf("gate host: tray: %v", err)
	}
}
