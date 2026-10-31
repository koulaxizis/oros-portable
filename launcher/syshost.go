package main

// The orOS System extension (koulaxizis/oros-usb system/extension) in
// orOS Portable and the desktop apps.
//
// On orOS USB the extension puts sound, brightness, battery, Wi-Fi, power
// and the rest into orOS's Quick settings and talks to the computer
// through the oros-system native messaging host. Here the same extension
// is loaded into the bundled browser (system/ next to the launcher, built
// in by scripts/build.sh), and the launcher itself is its native host:
// Chromium starts "orOS chrome-extension://<id>/" for each message, the
// launcher answers one message and exits.
//
// The answers say platform "portable" and which parts this computer has
// (caps); the extension shows only those. What the launcher does to the
// computer is the user's own wish from orOS: the sound and brightness of
// this computer, sleep, restart and shut down (the extension asks first),
// leaving orOS, full screen or a window. Nothing else is reachable: the
// host answers only the extension's fixed ID (allowed_origins), which
// Chromium enforces, and only the commands below.

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// hostName is the native messaging name the extension calls.
	hostName = "online.useoros.system"
	// extID follows from the "key" in the extension's manifest.json.
	extID = "hibkbfiidcfohidigfegiadgedhmdecc"
	// sysExtFolder holds the extension next to the launcher.
	sysExtFolder = "system"
	// pingFile: the extension pings every 10 s once orOS has painted;
	// the launcher's watchdog reads its time (watchdog.go).
	pingFile = "ping"
	// sessionFile keeps "full screen or window" (data folder).
	sessionFile = "session.json"
)

// sysExtDir is the extension folder when it is there (full edition and
// the desktop apps); the lite edition has none.
func sysExtDir(lay layout) string {
	d := filepath.Join(lay.root, sysExtFolder)
	if _, err := os.Stat(filepath.Join(d, "manifest.json")); err != nil {
		return ""
	}
	return d
}

// session is the window preference.
type session struct {
	Fullscreen bool `json:"fullscreen"`
}

// loadSession: the stick opens in full screen, like a started computer;
// the desktop apps open in a window, like other programs. A saved choice
// wins.
func loadSession(lay layout) session {
	s := session{Fullscreen: !lay.installed}
	if b, err := os.ReadFile(filepath.Join(lay.data, sessionFile)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func saveSession(lay layout, s session) error {
	b, _ := json.Marshal(s)
	if err := os.MkdirAll(lay.data, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(lay.data, sessionFile), b, 0o644)
}

// sysBrowserArgs are the extra browser flags when the extension is there
// and the browser is the bundled one (Edge and Chrome refuse
// --load-extension). The extension loads with every window, because the
// first window started on the profile decides what the browser loads;
// full screen applies to the main orOS window only. It also registers
// the launcher as the extension's native host.
func sysBrowserArgs(lay layout, c candidate, url string) []string {
	dir := sysExtDir(lay)
	if dir == "" || !c.bundled {
		return nil
	}
	registerHostOnce(lay, c.profile)
	args := []string{"--load-extension=" + dir}
	if url == appURL && loadSession(lay).Fullscreen {
		args = append(args, "--start-fullscreen")
	}
	return args
}

var (
	hostMu      sync.Mutex
	hostDone    = map[string]bool{}
	hostCleanup []func()
)

// registerHostOnce registers the host for a profile once per run.
func registerHostOnce(lay layout, profile string) {
	hostMu.Lock()
	defer hostMu.Unlock()
	if hostDone[profile] {
		return
	}
	hostDone[profile] = true
	exe, err := os.Executable()
	if err != nil {
		log.Printf("system host: %v", err)
		return
	}
	if p := os.Getenv("APPIMAGE"); p != "" {
		exe = p // the mount point changes with every start
	}
	undo, err := registerHost(lay, profile, exe)
	if err != nil {
		log.Printf("system host: %v", err)
	}
	hostCleanup = append(hostCleanup, undo)
}

// unregisterHost takes back what registerHost changed outside the stick
// (the Windows registry key of a portable copy).
func unregisterHost() {
	hostMu.Lock()
	defer hostMu.Unlock()
	for _, f := range hostCleanup {
		f()
	}
	hostCleanup = nil
	hostDone = map[string]bool{}
}

// hostManifest is the native messaging manifest that points Chromium at
// this launcher.
func hostManifest(exe string) []byte {
	b, _ := json.MarshalIndent(map[string]any{
		"name":            hostName,
		"description":     "orOS system (sound, power, updates) for orOS Portable",
		"path":            exe,
		"type":            "stdio",
		"allowed_origins": []string{"chrome-extension://" + extID + "/"},
	}, "", "  ")
	return b
}

// ---------- the host ----------

type sysRequest struct {
	Cmd   string `json:"cmd"`
	On    bool   `json:"on,omitempty"`
	Value int    `json:"value,omitempty"`
	Mode  string `json:"mode,omitempty"`
	// Wi-Fi (sysnet.go). The password is used once and never logged.
	SSID     string `json:"ssid,omitempty"`
	Password string `json:"password,omitempty"`
}

type sysReply map[string]any

// isHostCall: Chromium starts a native host with the caller's origin as
// the first argument (and on Windows a --parent-window after it).
func isHostCall(args []string) bool {
	return len(args) > 1 && strings.HasPrefix(args[1], "chrome-extension://")
}

// runSysHost answers native messages until Chromium closes stdin: each
// message is a 32-bit length in native (little-endian) byte order and
// that much JSON.
func runSysHost(lay layout, in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	for {
		var n uint32
		if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if n > 1<<20 {
			return fmt.Errorf("message too large: %d", n)
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return err
		}
		var req sysRequest
		var res sysReply
		if err := json.Unmarshal(buf, &req); err != nil {
			res = sysReply{"ok": false, "error": "bad request"}
		} else {
			res = handleSys(lay, req)
		}
		b, _ := json.Marshal(res)
		if err := binary.Write(out, binary.LittleEndian, uint32(len(b))); err != nil {
			return err
		}
		if _, err := out.Write(b); err != nil {
			return err
		}
	}
}

// sysPlatform is what this build can do; tests replace it.
var sysPlatform platformOps = hostPlatform()

// platformOps is the part that differs between Windows and Linux
// (sys_windows.go, sys_linux.go).
type platformOps interface {
	volume() (level int, muted bool, ok bool)
	setVolume(level int) error
	setMute(on bool) error
	battery() (percent int, charging bool, ok bool)
	brightness() (level int, ok bool)
	setBrightness(level int) error
	power(action string) error // "off", "reboot", "sleep"
	downloads() string
}

func ok(r sysReply) sysReply {
	if r == nil {
		r = sysReply{}
	}
	r["ok"] = true
	return r
}

func sysFail(err error) sysReply {
	return sysReply{"ok": false, "error": err.Error()}
}

var errUnsupported = errors.New("not available in orOS Portable")

func handleSys(lay layout, req sysRequest) sysReply {
	if req.Cmd != "ping" && req.Cmd != "status" {
		log.Printf("system: %s", req.Cmd)
	}
	p := sysPlatform
	if r, done := handleNet(req); done {
		return r
	}
	switch req.Cmd {
	case "ping":
		touch(filepath.Join(lay.data, pingFile))
		// orOS Bridge's open pairing code, shown over orOS (bridge.go)
		if c := bridgeCode(lay); c != "" {
			return ok(sysReply{"bridgeCode": c})
		}
		return ok(nil)
	case "bridge.code":
		return ok(sysReply{"code": bridgeCode(lay)})
	case "status":
		return ok(sysStatus(lay, p))
	case "volume.set":
		if err := p.setVolume(clamp(req.Value, 0, 100)); err != nil {
			return sysFail(err)
		}
		return ok(nil)
	case "volume.mute":
		if err := p.setMute(req.On); err != nil {
			return sysFail(err)
		}
		return ok(nil)
	case "brightness.set":
		if err := p.setBrightness(clamp(req.Value, 5, 100)); err != nil {
			return sysFail(err)
		}
		return ok(nil)
	case "power.off", "power.reboot", "power.sleep":
		if err := p.power(strings.TrimPrefix(req.Cmd, "power.")); err != nil {
			return sysFail(err)
		}
		return ok(nil)
	case "window.mode":
		// bg.js has already changed the window; this keeps the choice.
		if req.Mode != "fullscreen" && req.Mode != "window" {
			return sysFail(errors.New("unknown mode"))
		}
		if err := saveSession(lay, session{Fullscreen: req.Mode == "fullscreen"}); err != nil {
			return sysFail(err)
		}
		return ok(nil)
	case "report":
		f, err := writeSysReport(lay, p.downloads())
		if err != nil {
			return sysFail(err)
		}
		return ok(sysReply{"file": filepath.Base(f)})
	case "update.check":
		return updateCheck(lay)
	case "update.start":
		return updateStart(lay)
	case "update.status":
		return updateStatus(lay)
	case "web.open":
		return openWeb(lay)
	}
	return sysFail(errUnsupported)
}

// sysStatus is the extension's "status": the same fields as on orOS USB
// where this computer has them, plus what this edition offers.
func sysStatus(lay layout, p platformOps) sysReply {
	r := sysReply{
		"platform": "portable",
		"os":       runtime.GOOS,
		"version":  version,
		"window":   map[bool]string{true: "fullscreen", false: "window"}[loadSession(lay).Fullscreen],
		"caps": map[string]bool{
			// Parts the extension shows; the rest stays hidden.
			"sound": true, "screen": true, "power": true, "sleep": true,
			"exit": true, "window": true, "report": true,
			"update": !lay.installed,        // installed copies update themselves
			"web":    webBrowser(lay) != "", // the Internet window (sysweb.go)
		},
	}
	if v, m, ok := p.volume(); ok {
		r["volume"] = v
		r["muted"] = m
	}
	if b, ok := p.brightness(); ok {
		r["brightness"] = b
	}
	if pc, ch, ok := p.battery(); ok {
		r["battery"] = map[string]any{"percent": pc, "charging": ch}
	}
	netStatus(r, r["caps"].(map[string]bool))
	return r
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func touch(path string) {
	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, nil, 0o644)
	}
}
