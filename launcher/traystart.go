package main

import (
	"log"
	"os"
	"os/exec"
	"runtime"
)

// An installed orOS on Windows always has its tray icon (Chris
// 2026-11-08: the icon was gone). Until 0.8.2 only "Start with the
// computer" (off by default) or the setup's unticked task started the
// tray, so opening orOS from the Start menu or the desktop never showed
// it, and after a restart, a Quit or an update that opened a window it
// stayed away. Now every window of an installed copy starts the tray
// beside it when none runs. The window keeps serving; the tray takes the
// server over when the window closes (keepServer), so Add or search and
// Ctrl+Alt+Shift+Space keep working. "Start with the computer" still
// decides only whether the tray starts at login. Linux is unchanged
// (plain GNOME shows no tray icons). OROS_NO_TRAY=1 starts none.

// trayLogFile is the tray's own log (setupLog).
const trayLogFile = "tray.log"

var (
	trayAlongsideOS = runtime.GOOS == "windows"
	spawnTray       = func(exe string) error {
		cmd := exec.Command(exe, "tray")
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
)

// startTrayAlongside starts "orOS tray" as a process of its own, so it
// stays when the window's launcher ends. A tray that runs already holds
// tray.lock and is left alone.
func startTrayAlongside(lay layout) bool {
	if !lay.installed || !trayAlongsideOS || os.Getenv("OROS_NO_TRAY") != "" {
		return false
	}
	unlock, free := lockTray(lay.data)
	if !free {
		return false // the tray runs already
	}
	unlock()
	exe := trayCommand()
	if exe == "" {
		return false
	}
	if err := spawnTray(exe); err != nil {
		log.Printf("tray: %v", err)
		return false
	}
	log.Printf("tray started beside the window")
	return true
}
