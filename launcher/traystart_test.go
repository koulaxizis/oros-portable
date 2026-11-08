package main

import (
	"testing"
)

// An installed copy on Windows starts its tray beside a window, once.
func TestTrayStartsBesideTheWindow(t *testing.T) {
	oldOS, oldSpawn := trayAlongsideOS, spawnTray
	defer func() { trayAlongsideOS, spawnTray = oldOS, oldSpawn }()
	var started []string
	spawnTray = func(exe string) error { started = append(started, exe); return nil }
	t.Setenv("OROS_NO_TRAY", "")
	trayAlongsideOS = true

	lay := layout{data: t.TempDir(), installed: true}
	if !startTrayAlongside(lay) || len(started) != 1 || started[0] == "" {
		t.Fatalf("installed copy: tray not started: %v", started)
	}

	// A tray that runs holds tray.lock: no second one.
	unlock, ok := lockTray(lay.data)
	if !ok {
		t.Fatal("lock")
	}
	if startTrayAlongside(lay) {
		t.Fatal("a second tray was started")
	}
	unlock()

	// Portable copies, other systems and OROS_NO_TRAY start none.
	if startTrayAlongside(layout{data: t.TempDir()}) {
		t.Fatal("portable copy started a tray")
	}
	t.Setenv("OROS_NO_TRAY", "1")
	if startTrayAlongside(lay) {
		t.Fatal("OROS_NO_TRAY started a tray")
	}
	t.Setenv("OROS_NO_TRAY", "")
	trayAlongsideOS = false
	if startTrayAlongside(lay) {
		t.Fatal("Linux started a tray")
	}
	if len(started) != 1 {
		t.Fatal(started)
	}
}

// The tray writes its own log, so a window's launcher.log is kept.
func TestTrayHasItsOwnLog(t *testing.T) {
	if logName("tray") != "tray.log" || logName("") != "launcher.log" || logName("quick") != "launcher.log" {
		t.Fatal(logName("tray"), logName(""))
	}
}
