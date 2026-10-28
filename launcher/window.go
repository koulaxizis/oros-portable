package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// Window placement (orOS Bible "Quick window"). Chromium keeps the size,
// place and maximised state of an --app window in the profile under a
// name made from the address without its fragment ("localhost_/"), and
// --start-maximized would maximise every app window of that browser,
// the small one too. So:
//   - the small window has an address of its own (quickPath, which the
//     server sends on to "/"), so its size never becomes the main
//     window's and a maximised main window does not maximise it;
//   - before a browser starts on the profile (for either window), the
//     main window's saved placement is marked maximised (seedMaximized),
//     so it opens maximised every time, also when the small window was
//     open first and the browser hands the main window to itself;
//   - the small window opens centred on the screen: where the launcher
//     knows the work area (Windows) it passes --window-position, and the
//     orOS page centres "#quick-win" itself everywhere else.

const (
	quickPath = "/__oros-quick"
	quickW    = 640
	quickH    = 460
	// mainPlacement and quickPlacement are Chromium's names for the
	// two windows (GenerateApplicationNameFromURL: host + "_" + path).
	mainPlacement  = "localhost_/"
	quickPlacement = "localhost_" + quickPath
)

// quickArgs: the size of the small window (px at 100% scale) and, where
// the work area is known, the place that centres it.
func quickArgs() []string {
	args := []string{fmt.Sprintf("--window-size=%d,%d", quickW, quickH)}
	if x, y, w, h, ok := workArea(); ok {
		args = append(args, fmt.Sprintf("--window-position=%d,%d", x+(w-quickW)/2, y+(h-quickH)/2))
	}
	return args
}

// windowArgs prepares the profile for the window at url: the main
// window is marked maximised while no browser holds the profile (a
// running browser keeps its own copy and writes it back).
func windowArgs(profile, url string, extra []string) []string {
	if !profileInUse(profile) {
		if err := seedMaximized(profile); err != nil {
			log.Printf("window placement: %v", err)
		}
	}
	return browserArgs(profile, url, extra...)
}

// restored: the size the main window takes when it is un-maximised and
// no larger one was saved (Chromium fits it to the screen).
var restored = map[string]any{"left": 40, "top": 30, "right": 1240, "bottom": 830}

// seedMaximized sets maximized=true in the main window's saved placement
// in <profile>/Default/Preferences, keeping every other setting. Chromium
// uses the placement only with all four bounds, so a missing or small one
// (the small window's, saved by orOS Portable 0.3.0 under the same name)
// gets the restored size.
func seedMaximized(profile string) error {
	file := filepath.Join(profile, "Default", "Preferences")
	prefs := map[string]any{}
	b, err := os.ReadFile(file)
	switch {
	case err == nil:
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if err := d.Decode(&prefs); err != nil {
			return fmt.Errorf("%s: %v", file, err) // never overwrite what we cannot read
		}
	case !os.IsNotExist(err):
		return err
	}
	browser := child(prefs, "browser")
	places := child(browser, "app_window_placement")
	place := child(places, mainPlacement)
	if place["maximized"] == true && wideEnough(place) {
		return nil
	}
	if !wideEnough(place) {
		for k, v := range restored {
			place[k] = v
		}
	}
	place["maximized"] = true
	out, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".oros-tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// child returns m[key] as a map, making it (or replacing a non-map).
func child(m map[string]any, key string) map[string]any {
	if c, ok := m[key].(map[string]any); ok {
		return c
	}
	c := map[string]any{}
	m[key] = c
	return c
}

// wideEnough: the saved bounds exist and are larger than the small window.
func wideEnough(place map[string]any) bool {
	n := map[string]int64{}
	for _, k := range []string{"left", "top", "right", "bottom"} {
		v, ok := place[k].(json.Number)
		if !ok {
			if i, isInt := place[k].(int); isInt {
				n[k] = int64(i)
				continue
			}
			return false
		}
		i, err := v.Int64()
		if err != nil {
			return false
		}
		n[k] = i
	}
	return n["right"]-n["left"] > quickW+100 && n["bottom"]-n["top"] > quickH+100
}
