package main

import (
	_ "embed"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/systray"
)

// The tray icon of an installed orOS (Quick window, QW; orOS Bible
// "Quick window"). "tray" keeps the loopback server up and shows the
// orOS icon in the system tray: a click opens a small window with the
// command bar (orOS "#quick-win": add a task or search your data), the
// menu opens orOS, turns "Start with the computer" on or off, or quits.
// On Windows Ctrl+Alt+Shift+Space opens the small window too. The tray
// never reads orOS data: everything stays in the browser profile.
//
// "quick" opens the small window from a command (a Linux keyboard
// shortcut, the menu entry's action, the Start menu entry).
//
// Updates: the tray applies a downloaded orOS, or hands over to a new
// program version, only while no orOS window is open, like a start.
//
// On a Linux desktop without a tray (plain GNOME needs the AppIndicator
// extension) the icon is not shown; "quick" and the menu still work.

// quickURL: the shell opens the bar filling the window, and Escape with
// nothing run closes the window. quickPath (window.go) gives the window
// a placement of its own; the server sends it on to "/".
const quickURL = "http://localhost:47800" + quickPath + "#quick-win"

//go:embed icons/oros.png
var iconPNG []byte

//go:embed icons/oros.ico
var iconICO []byte

const maintainEvery = 10 * time.Minute

type trayApp struct {
	lay      layout
	dir      atomic.Value // string: the orOS folder being served
	mu       sync.Mutex
	srv      servers
	opening  atomic.Bool
	appImage time.Time // the AppImage file's time at start (self-replaced = newer)
	unlock   func()
}

func runTray(lay layout) {
	if !lay.installed {
		fail(word("The tray icon is for an installed orOS (Setup.exe, AppImage or .deb).",
			"Το εικονίδιο στο tray είναι για εγκατεστημένο orOS (Setup.exe, AppImage ή .deb).",
			"Das Symbol im Infobereich gibt es nur für ein installiertes orOS (Setup.exe, AppImage oder .deb)."))
	}
	unlock, ok := lockTray(lay.data)
	if !ok {
		log.Printf("the tray runs already")
		return
	}
	integrateAppImage(lay)
	if beforeStart(lay, true) {
		unlock()
		return // the setup of a new version takes over and starts the tray again
	}
	t := &trayApp{lay: lay, unlock: unlock}
	t.dir.Store(lay.appDir())
	if img := os.Getenv("APPIMAGE"); img != "" {
		if st, err := os.Stat(img); err == nil {
			t.appImage = st.ModTime()
		}
	}
	t.takeServer()
	go t.keepServer()
	go hotkeyLoop(t.openQuick)
	systray.Run(t.ready, t.exit)
}

func (t *trayApp) ready() {
	if runtime.GOOS == "windows" {
		systray.SetIcon(iconICO)
	} else {
		systray.SetIcon(iconPNG)
	}
	systray.SetTitle("orOS")
	systray.SetTooltip(word("orOS: add or search", "orOS: προσθήκη ή αναζήτηση", "orOS: hinzufügen oder suchen"))
	systray.SetOnTapped(t.openQuick)
	quick := systray.AddMenuItem(word("Add or search", "Προσθήκη ή αναζήτηση", "Hinzufügen oder suchen"), "")
	open := systray.AddMenuItem(word("Open orOS", "Άνοιγμα orOS", "orOS öffnen"), "")
	systray.AddSeparator()
	auto := systray.AddMenuItemCheckbox(word("Start with the computer", "Εκκίνηση με τον υπολογιστή", "Mit dem Computer starten"), "", autostartOn())
	systray.AddSeparator()
	quit := systray.AddMenuItem(word("Quit", "Έξοδος", "Beenden"), "")
	go func() {
		for {
			select {
			case <-quick.ClickedCh:
				t.openQuick()
			case <-open.ClickedCh:
				t.open(appURL)
			case <-auto.ClickedCh:
				on := !autostartOn()
				if err := setAutostart(on, trayCommand()); err != nil {
					log.Printf("autostart: %v", err)
				}
				if autostartOn() {
					auto.Check()
				} else {
					auto.Uncheck()
				}
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
	go t.maintain()
}

func (t *trayApp) exit() {
	t.mu.Lock()
	if t.srv != nil {
		shutdown(t.srv)
		t.srv = nil
	}
	t.mu.Unlock()
	t.unlock()
	log.Printf("tray closed")
}

// takeServer serves orOS unless a launcher with a window serves it
// already; keepServer takes over when that one stops.
func (t *trayApp) takeServer() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.srv != nil {
		return true
	}
	h := newDirHandler(func() string { return t.dir.Load().(string) })
	srv, err := startServerWith(h, t.dir.Load().(string))
	if err != nil {
		return false
	}
	t.srv = srv
	return true
}

func (t *trayApp) keepServer() {
	for {
		time.Sleep(5 * time.Second)
		t.takeServer()
	}
}

func (t *trayApp) openQuick() { t.open(quickURL, quickArgs()...) }

// open starts a browser window without waiting for it. Clicks while one
// is starting are dropped (a slow first start must not open three).
func (t *trayApp) open(url string, extra ...string) {
	if !t.opening.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer t.opening.Store(false)
		t.takeServer()
		if err := startBrowser(t.lay, url, extra...); err != nil {
			notify(err.Error())
		}
	}()
}

// windowOpen: a browser has an orOS profile in use.
func (t *trayApp) windowOpen() bool {
	for _, p := range []string{"profile", "profile-system"} {
		if profileInUse(filepath.Join(t.lay.data, p)) {
			return true
		}
	}
	return false
}

// maintain checks for updates (at most every 6 hours, checkUpdatesLater)
// and applies them while no window is open and the tray serves.
func (t *trayApp) maintain() {
	for {
		checkUpdatesLater(t.lay)
		time.Sleep(maintainEvery)
		t.mu.Lock()
		serving := t.srv != nil
		t.mu.Unlock()
		if !serving || t.windowOpen() || t.opening.Load() || !autoUpdatesOn(t.lay) {
			continue
		}
		applyStagedApp(t.lay)
		t.dir.Store(t.lay.appDir())
		if t.newProgram() {
			return
		}
	}
}

// newProgram hands over to a downloaded program version: the Windows
// setup (it starts the tray again), or the AppImage that replaced itself.
// A .deb waits for the next start, which asks for the password.
func (t *trayApp) newProgram() bool {
	if runtime.GOOS == "windows" {
		if setup, _ := pendingSetup(t.lay); setup == "" {
			return false
		}
		t.release()
		if runPendingSetup(t.lay, true) {
			systray.Quit()
			return true
		}
		t.relock()
		return false
	}
	img := os.Getenv("APPIMAGE")
	if img == "" || t.appImage.IsZero() {
		return false
	}
	st, err := os.Stat(img)
	if err != nil || !st.ModTime().After(t.appImage) {
		return false
	}
	t.release()
	if err := exec.Command(img, "tray").Start(); err != nil {
		log.Printf("restarting the tray: %v", err)
		t.relock()
		return false
	}
	systray.Quit()
	return true
}

// release lets the next tray start: no server, no lock.
func (t *trayApp) release() {
	t.mu.Lock()
	if t.srv != nil {
		shutdown(t.srv)
		t.srv = nil
	}
	t.mu.Unlock()
	t.unlock()
	t.unlock = func() {}
}

func (t *trayApp) relock() {
	if unlock, ok := lockTray(t.lay.data); ok {
		t.unlock = unlock
	}
	t.takeServer()
}

// startBrowser runs the first browser that starts, without waiting for its
// window. A browser that fails within a few seconds is skipped.
func startBrowser(lay layout, url string, extra ...string) error {
	list := browserCandidates(lay)
	if len(list) == 0 {
		return errors.New(word("No browser was found. Reinstall orOS.", "Δεν βρέθηκε browser. Εγκατέστησε ξανά το orOS.",
			"Kein Browser gefunden. Installiere orOS neu."))
	}
	for _, c := range list {
		log.Printf("starting %s (profile %s)", c.path, c.profile)
		cmd := exec.Command(c.path, windowArgs(c.profile, url, extra)...)
		if err := cmd.Start(); err != nil {
			log.Printf("browser: %v, trying the next one", err)
			continue
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				log.Printf("browser failed early: %v, trying the next one", err)
				continue
			}
			return nil // handed the window to a browser that runs already
		case <-time.After(5 * time.Second):
			return nil
		}
	}
	return errors.New("None of the browsers could start. See " + filepath.Join(lay.data, "launcher.log") + ".")
}

// runQuick opens the small window. With a tray or a window serving, it
// only starts the browser; otherwise it serves until the window closes.
func runQuick(lay layout) {
	if alreadyRunning() {
		if err := startBrowser(lay, quickURL, quickArgs()...); err != nil {
			fail(err.Error())
		}
		return
	}
	if beforeStart(lay, false) {
		return
	}
	appDir := lay.appDir()
	if _, err := os.Stat(filepath.Join(appDir, "index.html")); err != nil {
		fail("The app folder is missing next to the launcher: " + appDir)
	}
	runWindow(lay, appDir, quickURL, quickArgs()...)
}

// trayCommand is the program that "Start with the computer" runs: the
// AppImage file itself, else this launcher.
func trayCommand() string {
	if img := os.Getenv("APPIMAGE"); img != "" {
		return img
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}
