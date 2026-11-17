package main

import (
	"log"
	"sync"
	"time"

	"fyne.io/systray"
)

// The orOS Drive and orOS Bridge menus of the orOS tray icon (drive.go,
// bridge.go): one icon for the three programs.

const menuEvery = 10 * time.Second

// driveMenu shows orOS Drive when it is installed (on its own, or
// carried by this orOS), hands it over and keeps it running.
func (t *trayApp) driveMenu() {
	cli := driveExe(t.lay)
	if cli == "" {
		return
	}
	m := systray.AddMenuItem("orOS Drive", "")
	syncNow := m.AddSubMenuItem(word("Sync now", "Συγχρονισμός τώρα", "Jetzt synchronisieren"), "")
	pause := m.AddSubMenuItem(word("Pause", "Παύση", "Pausieren"), "")
	settings := m.AddSubMenuItem(word("Settings", "Ρυθμίσεις", "Einstellungen"), "")
	var mu sync.Mutex
	var cur driveSummary
	var curErr error = errDriveOff
	paused := func() bool { mu.Lock(); defer mu.Unlock(); return curErr == nil && cur.Paused }
	refresh := func() {
		mu.Lock()
		defer mu.Unlock()
		cur, curErr = driveStatus()
		title := "orOS Drive: " + driveText(cur, curErr)
		if curErr == nil && cur.Update != "" {
			title += word(" (new: ", " (νέα: ", " (neu: ") + cur.Update + ")"
		}
		m.SetTitle(title)
		if curErr != nil || !cur.SignedIn || !cur.Unlocked {
			syncNow.Disable()
			pause.Disable()
		} else {
			syncNow.Enable()
			pause.Enable()
		}
		if curErr == nil && cur.Paused {
			pause.SetTitle(word("Resume", "Συνέχεια", "Fortsetzen"))
		} else {
			pause.SetTitle(word("Pause", "Παύση", "Pausieren"))
		}
	}
	refresh()
	go func() {
		driveTakeOver(cli)
		k := &driveKeeper{}
		folders := len(readDriveSettings().Folders)
		for !t.quitting.Load() {
			// The first mirrored folder: orOS now starts with the computer,
			// so Drive goes on syncing after a restart (once; the menu can
			// turn it off again).
			if n := len(readDriveSettings().Folders); n > 0 && folders == 0 && !autostartOn() {
				if err := setAutostart(true, trayCommand()); err != nil {
					log.Printf("autostart: %v", err)
				}
			} else {
				folders = n
			}
			folders = len(readDriveSettings().Folders)
			if _, err := driveStatus(); err == errDriveOff && readDriveSettings().exists && k.mayStart(time.Now()) {
				if err := driveStart(cli, true); err != nil {
					log.Printf("orOS Drive: %v", err)
				} else {
					log.Printf("orOS Drive started")
					time.Sleep(3 * time.Second)
				}
			}
			refresh()
			time.Sleep(menuEvery)
		}
	}()
	go func() {
		for {
			select {
			case <-syncNow.ClickedCh:
				_ = driveCall("POST", "/api/sync", map[string]any{}, nil)
			case <-pause.ClickedCh:
				_ = driveCall("POST", "/api/pause", map[string]bool{"paused": !paused()}, nil)
				refresh()
			case <-settings.ClickedCh:
				if _, err := driveStatus(); err == nil {
					if err := driveRun(cli, "open"); err != nil {
						log.Printf("orOS Drive: %v", err)
					}
				} else if err := driveStart(cli, false); err != nil { // opens its settings page
					log.Printf("orOS Drive: %v", err)
				}
			}
		}
	}()
}

// bridgeMenu shows whether orOS Bridge is ready for Studio. The pairing
// code exists only while Studio asks to pair (the bridge removes its
// code file otherwise), so the submenu always carries the state, a line
// for the code (or where it will show) and Open Studio.
func (t *trayApp) bridgeMenu() {
	if bridgeExe(t.lay) == "" {
		return
	}
	m := systray.AddMenuItem("orOS Bridge", "")
	state := m.AddSubMenuItem("", "")
	state.Disable()
	code := m.AddSubMenuItem("", "")
	code.Disable()
	studio := m.AddSubMenuItem(word("Open Studio", "Άνοιγμα του Studio", "Studio öffnen"), "")
	refresh := func() {
		up := bridgeAnswers()
		title, line := bridgeLines(up, bridgeCode(t.lay))
		m.SetTitle(title)
		state.SetTitle(title)
		code.SetTitle(line)
	}
	refresh()
	go func() {
		for !t.quitting.Load() {
			time.Sleep(menuEvery)
			refresh()
		}
	}()
	go func() {
		for range studio.ClickedCh {
			t.open(appURL + "?open=studio")
		}
	}()
}

// bridgeLines: the menu title and the pairing line for the bridge's state.
func bridgeLines(up bool, code string) (title, line string) {
	if !up {
		return "orOS Bridge: " + word("Not running", "Δεν τρέχει", "Läuft nicht"),
			word("Quit orOS and open it again to start it", "Κλείσε το orOS από εδώ και άνοιξέ το ξανά για να ξεκινήσει", "orOS beenden und neu öffnen, um sie zu starten")
	}
	title = "orOS Bridge: " + word("ready for Studio", "έτοιμο για το Studio", "bereit für Studio")
	if code != "" {
		return title, word("Pairing code: ", "Κωδικός σύνδεσης: ", "Kopplungscode: ") + code
	}
	return title, word("The pairing code shows here when Studio connects",
		"Ο κωδικός σύνδεσης εμφανίζεται εδώ όταν συνδεθεί το Studio",
		"Der Kopplungscode erscheint hier, wenn Studio sich verbindet")
}
