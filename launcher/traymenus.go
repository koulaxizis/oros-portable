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

// bridgeMenu shows whether orOS Bridge is ready for Studio and its
// pairing code (the code Studio asks for once).
func (t *trayApp) bridgeMenu() {
	if bridgeExe(t.lay) == "" {
		return
	}
	m := systray.AddMenuItem("orOS Bridge", "")
	code := m.AddSubMenuItem("", "")
	code.Disable()
	refresh := func() {
		if bridgeAnswers() {
			m.SetTitle("orOS Bridge: " + word("ready for Studio", "έτοιμο για το Studio", "bereit für Studio"))
		} else {
			m.SetTitle("orOS Bridge: " + word("Not running", "Δεν τρέχει", "Läuft nicht"))
		}
		if c := bridgeCode(t.lay); c != "" {
			code.SetTitle(word("Pairing code: ", "Κωδικός σύνδεσης: ", "Kopplungscode: ") + c)
			code.Show()
		} else {
			code.Hide()
		}
	}
	refresh()
	go func() {
		for !t.quitting.Load() {
			time.Sleep(menuEvery)
			refresh()
		}
	}()
}
