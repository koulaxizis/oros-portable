package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Desktop bar (orOS smart top bar, wave SB4; orOS Bible "smartbar.js").
// "Smart bar on the desktop" in the tray menu (Windows, off by default)
// opens orOS's own top bar in a thin window of its own ("#bar-win": only
// the bar shows, its title is "orOS bar") and docks it at the top of the
// primary screen as an app bar, so maximised windows stay below it
// (deskbar_windows.go). The tray never reads orOS data: the bar is the
// orOS page itself in the user's browser profile. A chip, the clock or
// the menu button opens that app in the main orOS window (the page's
// "oros-barwin" channel), or, with no main window open, asks the tray
// through POST /__oros-open, which opens one.

const (
	barPath       = "/__oros-bar"
	barURL        = "http://localhost:47800" + barPath + "#bar-win"
	barTitle      = "orOS bar" // document.title of "#bar-win" (shell.js)
	barH          = 40         // the bar's height, px at 100% scale
	barPlacement  = "localhost_" + barPath
	openPath      = "/__oros-open"
	deskbarMarker = "deskbar.on" // in the data folder: the bar opens with the tray
)

var openAppRE = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

// openMain is set by the tray: open the main orOS window, with app ("" =
// the desktop). Without a tray (a plain window launcher) it is nil and
// /__oros-open answers 404.
var openMain func(app string)

func deskbarOn(data string) bool {
	_, err := os.Stat(filepath.Join(data, deskbarMarker))
	return err == nil
}

func setDeskbar(data string, on bool) error {
	p := filepath.Join(data, deskbarMarker)
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(p, []byte("1\n"), 0o600)
}

// barArgs: the bar's size and place: the width of the work area at its
// top (the app bar moves it to the screen's edge on Windows).
func barArgs() []string {
	if x, y, w, _, ok := workArea(); ok {
		return []string{fmt.Sprintf("--window-size=%d,%d", w, barH), fmt.Sprintf("--window-position=%d,%d", x, y)}
	}
	return []string{fmt.Sprintf("--window-size=%d,%d", 1200, barH)}
}

// barLayout: the profile the bar opens in: the only one, or the
// signed-in user's. With users and nobody signed in there is no bar
// (it never shows the sign-in window).
func barLayout(lay layout) (layout, bool) {
	if !loadUsers(lay.data).enabled() {
		return lay.forUser(lay.data), true
	}
	if dir, ok := activeUser(lay.data); ok {
		return lay.forUser(dir), true
	}
	return lay, false
}

// serveOpen answers POST /__oros-open[?app=<id>] from the bar window. A
// web page of another origin cannot send it: the custom header needs a
// CORS preflight this server never allows, and a stated Origin must be
// orOS's own.
func serveOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("X-Oros-Bar") != "1" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if o := r.Header.Get("Origin"); o != "" && o != strings.TrimSuffix(appURL, "/") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	app := r.URL.Query().Get("app")
	if app != "" && !openAppRE.MatchString(app) {
		http.Error(w, "bad app", http.StatusBadRequest)
		return
	}
	if openMain == nil {
		http.NotFound(w, r)
		return
	}
	openMain(app)
	w.WriteHeader(http.StatusNoContent)
}

// mainURLFor: the main window's address that opens app (orOS "?open=").
func mainURLFor(app string) string {
	if app == "" {
		return appURL
	}
	return appURL + "?open=" + url.QueryEscape(app)
}

func logBar(format string, a ...any) { log.Printf("desktop bar: "+format, a...) }
