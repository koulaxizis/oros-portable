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
	quitting atomic.Bool
	unlock   func()
	gate     *gateHostRunner // the background orOS for orOS Gate (gatehost.go)
	barBusy  atomic.Bool     // openBar runs (deskbar.go)
	barStop  chan struct{}   // closes the running dockBar; under mu
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
	t := &trayApp{lay: lay, unlock: unlock, gate: newGateHostRunner(lay)}
	if gateAPI != nil {
		gateAPI.running = t.gate.running
	}
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
	// orOS in its own window (webwin.go): a separate orOS.exe that shows
	// it in WebView2, with its own data like another device of the owner
	var own *systray.MenuItem
	if webWinSupported {
		own = systray.AddMenuItem(word("orOS in its own window", "Το orOS σε δικό του παράθυρο", "orOS in eigenem Fenster"), "")
	}
	swap := systray.AddMenuItem(word("Switch user", "Αλλαγή χρήστη", "Benutzer wechseln"), "")
	go func() { // shown only while there are users (users.go)
		for {
			if loadUsers(t.lay.data).enabled() {
				swap.Show()
			} else {
				swap.Hide()
			}
			time.Sleep(10 * time.Second)
		}
	}()
	var bar *systray.MenuItem
	if deskbarSupported {
		bar = systray.AddMenuItemCheckbox(word("Smart bar on the desktop", "Έξυπνη γραμμή στην επιφάνεια εργασίας",
			"Smarte Leiste auf dem Desktop"), "", deskbarOn(t.lay.data))
	}
	systray.AddSeparator()
	t.driveMenu()
	t.bridgeMenu()
	systray.AddSeparator()
	check := systray.AddMenuItem(checkUpdatesTitle(false), "")
	auto := systray.AddMenuItemCheckbox(word("Start with the computer", "Εκκίνηση με τον υπολογιστή", "Mit dem Computer starten"), "", autostartOn())
	systray.AddSeparator()
	quit := systray.AddMenuItem(word("Quit", "Έξοδος", "Beenden"), "")
	if own != nil {
		go func() {
			for range own.ClickedCh {
				openOwnWindow()
			}
		}()
	}
	barClicks := make(chan struct{})
	if bar != nil {
		go func() {
			for range bar.ClickedCh {
				barClicks <- struct{}{}
			}
		}()
	}
	openMain = func(app string) { t.open(mainURLFor(app)) }
	if deskbarOn(t.lay.data) {
		go t.openBar()
	}
	go func() {
		for {
			select {
			case <-barClicks:
				on := !deskbarOn(t.lay.data)
				if err := setDeskbar(t.lay.data, on); err != nil {
					logBar("%v", err)
				}
				if on {
					bar.Check()
					go t.openBar()
				} else {
					bar.Uncheck()
					t.closeBar()
				}
			case <-quick.ClickedCh:
				t.openQuick()
			case <-open.ClickedCh:
				t.open(appURL)
			case <-swap.ClickedCh:
				t.switchUser()
			case <-check.ClickedCh:
				go t.checkForUpdates(check)
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
				t.quitting.Store(true)
				if readDriveSettings().ManagedBy == managerPath() {
					driveQuit() // orOS started it; it ends with orOS's icon
				}
				t.closeBar() // the bar would keep the server waiting (exit)
				systray.Quit()
				return
			}
		}
	}()
	go t.maintain()
	go t.gate.loop(t.opening.Load)
}

func (t *trayApp) exit() {
	// Quit with an orOS window open: the icon is gone, but the server it
	// gives that window stays until the window closes (a window opened
	// while the tray served only opens the browser, runWindow).
	for t.quitting.Load() && t.serving() && t.windowOpen() {
		time.Sleep(2 * time.Second)
	}
	close(t.gate.stop)
	t.gate.halt()
	t.mu.Lock()
	if t.srv != nil {
		shutdown(t.srv)
		t.srv = nil
	}
	stopBridge(t.lay)
	stopAI(t.lay)
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
	startBridge(t.lay) // the bridge lives with the server (bridge.go)
	startAI(t.lay)     // and the local AI (ai.go)
	return true
}

func (t *trayApp) serving() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.srv != nil
}

func (t *trayApp) keepServer() {
	for {
		time.Sleep(5 * time.Second)
		t.takeServer()
	}
}

func (t *trayApp) openQuick() { t.open(quickURL, quickArgs()...) }

// openBar opens the desktop bar (deskbar.go) once the server is up and
// docks it; again after it closes while the setting is on and the tray
// runs (a browser that quit, a sign-out), at most every 30 s.
func (t *trayApp) openBar() {
	if !deskbarSupported || !t.barBusy.CompareAndSwap(false, true) {
		return
	}
	defer t.barBusy.Store(false)
	for deskbarOn(t.lay.data) && !t.quitting.Load() {
		if t.serving() {
			if ul, ok := barLayout(t.lay); ok {
				stop := make(chan struct{})
				t.mu.Lock()
				t.barStop = stop
				t.mu.Unlock()
				if err := startBrowser(ul, barURL, barArgs()...); err != nil {
					logBar("%v", err)
				} else {
					dockBar(stop)
				}
				t.mu.Lock()
				if t.barStop == stop {
					t.barStop = nil
				}
				t.mu.Unlock()
			}
		}
		time.Sleep(30 * time.Second)
	}
}

func (t *trayApp) barOpen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.barStop != nil
}

// stagedAppWaiting: a downloaded orOS (not a program) is ready to swap in.
func stagedAppWaiting(lay layout) bool {
	_, err := os.Stat(filepath.Join(stagedAppDir(lay), "index.html"))
	return err == nil
}

// updateWaiting: a downloaded orOS or a new program waits for no window.
func (t *trayApp) updateWaiting() bool {
	if _, err := os.Stat(stagedAppDir(t.lay)); err == nil {
		return true
	}
	if runtime.GOOS == "windows" {
		if setup, _ := pendingSetup(t.lay); setup != "" {
			return true
		}
	}
	return false
}

// closeBarFor closes the bar window and waits d for the browser to let
// the profile go.
func (t *trayApp) closeBarFor(d time.Duration) {
	t.closeBar()
	time.Sleep(d)
}

// closeBar: the setting went off. The window closes and its space is
// given back.
func (t *trayApp) closeBar() {
	t.mu.Lock()
	if t.barStop != nil {
		close(t.barStop)
		t.barStop = nil
	}
	t.mu.Unlock()
	closeBar()
}

// open starts a browser window without waiting for it. Clicks while one
// is starting are dropped (a slow first start must not open three).
func (t *trayApp) open(url string, extra ...string) {
	if !t.opening.CompareAndSwap(false, true) {
		return
	}
	go func() {
		t.takeServer()
		// With users, the sign-in window comes first and the session
		// waits for the user's browser (session.go); "opening" only covers
		// the start.
		launch := func(ul layout, url string, extra ...string) error {
			err := startBrowser(ul, url, extra...)
			t.opening.Store(false)
			if err != nil {
				return err
			}
			if loadUsers(ul.data).enabled() {
				waitUserClosed(ul.userFolder(), time.Minute)
			}
			return nil
		}
		if err := runSession(t.lay, url, extra, launch); err != nil {
			notify(err.Error())
		}
		t.opening.Store(false)
	}()
}

// switchUser: the signed-in user's orOS signs out (orOS sends what is
// unsynced first, then closes its windows); with nobody signed in it
// opens the sign-in window.
func (t *trayApp) switchUser() {
	if _, ok := activeUser(t.lay.data); ok {
		t.open(appURL + "#oros-signout")
		return
	}
	t.open(appURL)
}

// windowOpen: a browser has an orOS profile in use (any user's, or the
// sign-in window), other than the background orOS of this tray.
func (t *trayApp) windowOpen() bool {
	if t.gate != nil && t.gate.running() {
		return false // the profile is the background orOS's (no window can share it)
	}
	return anyProfileInUse(t.lay.data)
}

// maintain checks for updates (at most every 6 hours, checkUpdatesLater)
// and applies them while no window is open and the tray serves.
func (t *trayApp) maintain() {
	for {
		checkUpdatesLater(t.lay)
		time.Sleep(maintainEvery)
		if updateBusy.Load() {
			continue // "Check for updates" or /__oros/update is swapping orOS
		}
		t.mu.Lock()
		serving := t.srv != nil
		t.mu.Unlock()
		if serving && t.windowOpen() && !t.opening.Load() && autoUpdatesOn(t.lay) && t.barOpen() && t.updateWaiting() {
			// The desktop bar keeps the profile in use: it steps aside
			// (it comes back by itself in 30 s) so a waiting update can
			// apply while no other window is open.
			t.closeBarFor(15 * time.Second)
		}
		if serving && !t.opening.Load() && autoUpdatesOn(t.lay) && t.windowOpen() && stagedAppWaiting(t.lay) {
			// Chris 2026-11-21: a window left open for days kept orOS
			// (and orOS Gate on it) on an old version. The app's files
			// swap under the open window; its service worker sees the
			// new sw.js within the hour and reloads by itself. Only a new
			// program still waits for no window.
			applyStagedApp(t.lay)
			t.dir.Store(t.lay.appDir())
			continue
		}
		if !serving || t.windowOpen() || t.opening.Load() || !autoUpdatesOn(t.lay) {
			continue
		}
		t.gate.halt() // a new orOS: the background one starts again on it
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
	t.gate.halt()
	t.mu.Lock()
	if t.srv != nil {
		shutdown(t.srv)
		t.srv = nil
	}
	stopBridge(t.lay)
	stopAI(t.lay)
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
		args := append(append([]string{}, extra...), sysBrowserArgs(lay, c, url)...)
		cmd := exec.Command(c.path, windowArgs(c.profile, url, args)...)
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
		// With users, only the signed-in user's profile opens, or the
		// sign-in window first (session.go).
		launch := func(ul layout, url string, extra ...string) error {
			if err := startBrowser(ul, url, extra...); err != nil {
				return err
			}
			if loadUsers(ul.data).enabled() {
				waitUserClosed(ul.userFolder(), time.Minute)
			}
			return nil
		}
		if err := runSession(lay, quickURL, quickArgs(), launch); err != nil {
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

// openOwnWindow starts "orOS.exe window" on its own: the tray keeps serving,
// the window's process finds the server running and only shows the window.
func openOwnWindow() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("own window: %v", err)
		return
	}
	if err := exec.Command(exe, "window").Start(); err != nil {
		log.Printf("own window: %v", err)
	}
}
