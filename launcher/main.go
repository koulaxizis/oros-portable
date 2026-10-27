// orOS Portable launcher.
//
// Serves the orOS files that sit next to it on the USB stick from a tiny
// loopback-only web server, then opens a Chromium-family browser in app mode
// with its profile stored on the stick. Closing the window stops the server.
//
// Usage:
//
//	oros-portable           start orOS (default)
//	oros-portable update    replace app/ with the latest orOS from GitHub
//	oros-portable serve     only run the server (debugging)
//	oros-portable remove-menu  remove the menu entry an AppImage added
//	oros-portable tray      installed copies: the tray icon (tray.go)
//	oros-portable quick     the small "Add or search" window (tray.go)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The port is part of the origin, and the origin owns the data. It must
// never change, or every user would "lose" their data after an upgrade.
const (
	port       = 47800
	appURL     = "http://localhost:47800/"
	markerPath = "/__oros-portable"
	markerBody = "oros-portable"
)

var version = "0.1.0"

// appTitle names the launcher in messages and dialogs.
var appTitle = "orOS Portable"

func main() {
	root, err := rootDir()
	if err != nil {
		fail("Cannot find the orOS Portable folder: " + err.Error())
	}
	lay := newLayout(root)
	if lay.installed {
		appTitle = "orOS"
	}
	setupLog(lay.data)
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "", "run":
		if lay.installed {
			integrateAppImage(lay)
		}
		if beforeStart(lay, false) {
			return // the setup of a new version takes over
		}
		run(lay)
	case "tray":
		runTray(lay)
	case "quick":
		runQuick(lay)
	case "serve":
		serveOnly(lay)
	case "update":
		if err := update(lay.updates); err != nil {
			fail("Update failed: " + err.Error())
		}
		notify("orOS was updated. Start " + appTitle + " again to use the new version.")
	case "remove-menu":
		removeAppImageMenu()
	case "version", "--version":
		fmt.Println("oros-portable", version)
	default:
		fail("Unknown command: " + cmd)
	}
}

// rootDir is the folder that holds app/, browser/ and data/. It is the
// launcher's own folder unless OROS_PORTABLE_ROOT points elsewhere.
func rootDir() (string, error) {
	if r := os.Getenv("OROS_PORTABLE_ROOT"); r != "" {
		return filepath.Abs(r)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func setupLog(dir string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "launcher.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
}

func run(lay layout) {
	appDir := lay.appDir()
	if _, err := os.Stat(filepath.Join(appDir, "index.html")); err != nil {
		fail("The app folder is missing next to the launcher: " + appDir)
	}
	if err := profileDirUsable(lay.data); err != nil {
		log.Printf("profile folder: %v", err)
		fail("This stick's format (exFAT or FAT32) cannot hold the browser's data on Linux. Format the stick as NTFS and copy orOS Portable to it again; on Windows it works as it is.")
	}

	runWindow(lay, appDir, appURL)
}

// runWindow serves orOS, opens one browser window at url and keeps the
// server up until the browser is done with the profile. When another
// launcher (a window, the tray) serves already, it only opens the window.
func runWindow(lay layout, appDir, url string, extra ...string) {
	srv, err := startServer(appDir)
	if err != nil {
		if !alreadyRunning() {
			fail(fmt.Sprintf("Port %d is used by another program, so orOS cannot start. Close that program and try again.", port))
		}
		log.Printf("server already running, opening a window only")
		_ = launchBrowser(lay, url, extra...)
		return
	}
	defer shutdown(srv)

	checkUpdatesLater(lay)
	if err := launchBrowser(lay, url, extra...); err != nil {
		fail(err.Error())
	}
	log.Printf("browser closed; data is saved")
}

func serveOnly(lay layout) {
	srv, err := startServer(lay.appDir())
	if err != nil {
		fail(err.Error())
	}
	defer shutdown(srv)
	log.Printf("serving %s at %s (Ctrl+C to stop)", lay.appDir(), appURL)
	select {}
}

type servers []*http.Server

func shutdown(s servers) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, h := range s {
		_ = h.Shutdown(ctx)
	}
}

// startServer listens on IPv4 and, when available, IPv6 loopback, because
// "localhost" may resolve to either. Nothing is reachable from the network.
func startServer(appDir string) (servers, error) {
	return startServerWith(newHandler(appDir), appDir)
}

func startServerWith(handler http.Handler, appDir string) (servers, error) {
	ln4, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	out := servers{serve(ln4, handler)}
	if ln6, err := net.Listen("tcp", fmt.Sprintf("[::1]:%d", port)); err == nil {
		out = append(out, serve(ln6, handler))
	}
	log.Printf("%s %s serving %s", appTitle, version, appDir)
	return out, nil
}

func serve(ln net.Listener, h http.Handler) *http.Server {
	s := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := s.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server: %v", err)
		}
	}()
	return s
}

func alreadyRunning() bool {
	c := http.Client{Timeout: 2 * time.Second}
	r, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, markerPath))
	if err != nil {
		return false
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 64))
	return r.StatusCode == 200 && string(b) == markerBody
}

// Go reads MIME types from the Windows registry, which some programs
// corrupt (".js" as text/plain breaks module scripts), so the types that
// orOS needs are pinned here.
var mimeTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".json": "application/json",
	".webmanifest": "application/manifest+json", ".txt": "text/plain; charset=utf-8",
	".md": "text/markdown; charset=utf-8", ".svg": "image/svg+xml",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".ico": "image/x-icon",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
	".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav", ".mp4": "video/mp4",
	".webm": "video/webm", ".wasm": "application/wasm", ".xml": "application/xml",
	".pdf": "application/pdf", ".csv": "text/csv; charset=utf-8", ".zip": "application/zip",
}

func newHandler(appDir string) http.Handler {
	return newDirHandler(func() string { return appDir })
}

// newDirHandler serves the folder that dir names at each request, so the
// tray, which runs for days, can switch to a downloaded orOS.
func newDirHandler(dir func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject other Host names so a web page cannot reach this server
		// through DNS rebinding.
		if !allowedHost(r.Host) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.URL.Path == markerPath {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, markerBody)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if t, ok := mimeTypes[strings.ToLower(filepath.Ext(r.URL.Path))]; ok {
			w.Header().Set("Content-Type", t)
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.FileServer(http.Dir(dir())).ServeHTTP(w, r)
	})
}

func allowedHost(h string) bool {
	switch h {
	case fmt.Sprintf("localhost:%d", port), fmt.Sprintf("127.0.0.1:%d", port), fmt.Sprintf("[::1]:%d", port):
		return true
	}
	return false
}

// browserArgs keeps the browser quiet: no first-run pages, no sync, no
// background calls home, and the passwords stay in the profile on the stick.
func browserArgs(profile, url string, extra ...string) []string {
	args := []string{
		"--user-data-dir=" + profile,
		"--app=" + url,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-sync",
		"--disable-background-networking",
		"--disable-features=Translate,MediaRouter,OptimizationHints",
	}
	if runtime.GOOS == "linux" {
		args = append(args, "--password-store=basic")
	}
	args = append(args, extra...)
	return append(args, extraArgs()...)
}

// extraArgs lets tests add flags such as --headless=new.
func extraArgs() []string {
	return strings.Fields(os.Getenv("OROS_PORTABLE_BROWSER_ARGS"))
}

type candidate struct {
	path    string
	profile string
}

// browserCandidates lists the bundled browser first, then installed
// Chromium-family browsers. A system browser gets its own profile folder,
// because a profile written by a newer Chromium may not open in an older one.
func browserCandidates(lay layout) []candidate {
	data := lay.data
	var out []candidate
	if p := os.Getenv("OROS_PORTABLE_BROWSER"); p != "" {
		out = append(out, candidate{p, filepath.Join(data, "profile")})
	}
	for _, p := range bundledBrowsers(lay.root) {
		out = append(out, candidate{p, filepath.Join(data, "profile")})
	}
	for _, p := range systemBrowsers() {
		out = append(out, candidate{p, filepath.Join(data, "profile-system")})
	}
	return out
}

func bundledBrowsers(root string) []string {
	b := filepath.Join(root, "browser")
	if runtime.GOOS == "windows" {
		return existing(filepath.Join(b, "windows", "chrome.exe"))
	}
	return existing(filepath.Join(b, "linux", "chrome"))
}

func systemBrowsers() []string {
	if runtime.GOOS == "windows" {
		var paths []string
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
			base := os.Getenv(env)
			if base == "" {
				continue
			}
			paths = append(paths,
				filepath.Join(base, "Chromium", "Application", "chrome.exe"),
				filepath.Join(base, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"),
				filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
		}
		return existing(paths...)
	}
	var out []string
	for _, name := range []string{"chromium", "chromium-browser", "brave-browser", "microsoft-edge", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(name); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func existing(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// launchBrowser runs the first browser that starts and waits until its
// window closes. A browser that dies within a few seconds with an error
// (for example a sandbox that the host system blocks) is skipped.
func launchBrowser(lay layout, url string, extra ...string) error {
	list := browserCandidates(lay)
	if len(list) == 0 {
		return errors.New("No browser was found. Put the browser folder next to the launcher, or install Chromium, Edge, Brave or Chrome.")
	}
	for _, c := range list {
		log.Printf("starting %s (profile %s)", c.path, c.profile)
		cmd := exec.Command(c.path, browserArgs(c.profile, url, extra...)...)
		start := time.Now()
		err := cmd.Run()
		if err != nil && time.Since(start) < 5*time.Second {
			log.Printf("browser failed early: %v, trying the next one", err)
			continue
		}
		waitProfileClosed(c.profile)
		return nil
	}
	return errors.New("None of the browsers could start. See " + filepath.Join(lay.data, "launcher.log") + ".")
}

// waitProfileClosed keeps the server up while the browser still uses the
// profile. The process we started can exit while the window stays open:
// a browser may restart itself on its first run or after an update, and
// a second start hands the window to the process that runs already.
// Stopping the server then showed "Connection refused". On the first run
// a browser can take long to set up a new profile on a slow stick before
// it locks it, so we give it a minute to appear.
func waitProfileClosed(profile string) {
	for i := 0; i < 60 && !profileInUse(profile); i++ {
		time.Sleep(time.Second)
	}
	for profileInUse(profile) {
		time.Sleep(time.Second)
	}
}
