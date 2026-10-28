package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readPlacement(t *testing.T, profile string) (map[string]any, map[string]any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(profile, "Default", "Preferences"))
	must(t, err)
	var prefs map[string]any
	must(t, json.Unmarshal(b, &prefs))
	places := prefs["browser"].(map[string]any)["app_window_placement"].(map[string]any)
	return prefs, places
}

// A new profile gets a maximised main window with restored bounds,
// because Chromium ignores a placement without all four bounds.
func TestSeedMaximizedNewProfile(t *testing.T) {
	p := t.TempDir()
	must(t, seedMaximized(p))
	_, places := readPlacement(t, p)
	m := places[mainPlacement].(map[string]any)
	if m["maximized"] != true || m["right"].(float64)-m["left"].(float64) < 1000 {
		t.Fatal(m)
	}
}

// Other settings stay as they were (large numbers too); a saved large size
// is kept; the small window's own placement is never touched.
func TestSeedMaximizedKeepsTheRest(t *testing.T) {
	p := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(p, "Default"), 0o755))
	in := `{"profile":{"name":"x","created":13372036854775807},"browser":{"app_window_placement":{` +
		`"localhost_/":{"left":0,"top":0,"right":1500,"bottom":900,"maximized":false},` +
		`"localhost_/__oros-quick":{"left":10,"top":20,"right":650,"bottom":480,"maximized":false}}}}`
	must(t, os.WriteFile(filepath.Join(p, "Default", "Preferences"), []byte(in), 0o600))
	must(t, seedMaximized(p))
	b, _ := os.ReadFile(filepath.Join(p, "Default", "Preferences"))
	if !json.Valid(b) || !strings.Contains(string(b), `"created":13372036854775807`) {
		t.Fatal(string(b))
	}
	prefs, places := readPlacement(t, p)
	if prefs["profile"].(map[string]any)["name"] != "x" {
		t.Fatal(prefs)
	}
	m := places[mainPlacement].(map[string]any)
	if m["maximized"] != true || m["right"].(float64) != 1500 {
		t.Fatal(m)
	}
	if q := places[quickPlacement].(map[string]any); q["maximized"] != false || q["right"].(float64) != 650 {
		t.Fatal(q)
	}
}

// orOS Portable 0.3.0 saved the small window's size under the main
// window's name: that size is replaced, so un-maximising gives a real window.
func TestSeedMaximizedReplacesTheSmallSize(t *testing.T) {
	p := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(p, "Default"), 0o755))
	in := `{"browser":{"app_window_placement":{"localhost_/":{"left":10,"top":20,"right":650,"bottom":480,"maximized":false}}}}`
	must(t, os.WriteFile(filepath.Join(p, "Default", "Preferences"), []byte(in), 0o600))
	must(t, seedMaximized(p))
	_, places := readPlacement(t, p)
	m := places[mainPlacement].(map[string]any)
	if m["maximized"] != true || m["right"].(float64)-m["left"].(float64) < 1000 {
		t.Fatal(m)
	}
}

// A file we cannot read is left alone.
func TestSeedMaximizedLeavesABrokenFile(t *testing.T) {
	p := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(p, "Default"), 0o755))
	f := filepath.Join(p, "Default", "Preferences")
	must(t, os.WriteFile(f, []byte("{broken"), 0o600))
	if seedMaximized(p) == nil {
		t.Fatal("want an error")
	}
	if b, _ := os.ReadFile(f); string(b) != "{broken" {
		t.Fatal(string(b))
	}
}

// Before any window while no browser holds the profile: the main window
// may open later from the browser the small window started.
func TestWindowArgsSeedsBeforeEitherWindow(t *testing.T) {
	for _, url := range []string{quickURL, appURL} {
		p := t.TempDir()
		windowArgs(p, url, nil)
		_, places := readPlacement(t, p)
		if places[mainPlacement].(map[string]any)["maximized"] != true {
			t.Fatal(url)
		}
		if _, ok := places[quickPlacement]; ok {
			t.Fatal("the small window's placement is Chromium's own")
		}
	}
}
