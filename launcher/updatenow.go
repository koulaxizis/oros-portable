package main

import (
	"log"
	"os"
	"sync/atomic"
)

// "Update orOS" at once: the Start-menu entry and the stick's "Update
// orOS.cmd" ("orOS update") and "Check for updates" in the tray icon's
// menu (Chris 2026-11-21) run the same updateNow. It asks GitHub for the
// newest orOS first, so a copy that is already up to date says so instead
// of downloading it again; a newer orOS that the background check already
// downloaded (app.next) is swapped in without a second download. When
// GitHub cannot be asked (its API limit), it downloads as before.

// updateBusy: one update at a time in this process (the tray's menu item
// and the /__oros/update endpoint share it).
var updateBusy atomic.Bool

// updateLatest and updateFetch are variables so tests can replace them.
var (
	updateLatest = latestCommit
	updateFetch  = update
)

// updateNow brings the newest orOS and returns the message for the owner.
func updateNow(lay layout) (string, error) {
	latest, err := updateLatest()
	if err != nil {
		log.Printf("update: asking for the newest orOS: %v (downloading it anyway)", err)
	} else {
		if latest == appCommit(lay.appDir()) {
			return upToDateText(appVersion(lay.appDir())), nil
		}
		if lay.installed && latest == appCommit(stagedAppDir(lay)) {
			applyStagedApp(lay)
			if latest == appCommit(lay.appDir()) {
				return updatedText(appVersion(lay.appDir())), nil
			}
		}
	}
	if err := updateFetch(lay.updates); err != nil {
		updateLog(lay, "update asked by the owner failed: "+err.Error())
		return "", err
	}
	_ = removeStaleStaged(lay)
	ver := appVersion(lay.appDir())
	updateLog(lay, "orOS "+ver+" is now in use (update asked by the owner)")
	return updatedText(ver), nil
}

// removeStaleStaged drops an app.next that is older than what now runs,
// so the next start does not swap an older orOS back in.
func removeStaleStaged(lay layout) error {
	if !lay.installed || !stagedAppWaiting(lay) {
		return nil
	}
	if compareVersions(appVersion(stagedAppDir(lay)), appVersion(lay.appDir())) > 0 {
		return nil
	}
	return os.RemoveAll(stagedAppDir(lay))
}

func upToDateText(ver string) string {
	v := ""
	if ver != "" {
		v = " (" + ver + ")"
	}
	return word("orOS is up to date"+v+".",
		"Το orOS είναι ενημερωμένο"+v+".",
		"orOS ist auf dem neuesten Stand"+v+".")
}

func updatedText(ver string) string {
	v := ""
	if ver != "" {
		v = " " + ver
	}
	return word("orOS"+v+" is installed. Open "+appTitle+" again (or reload its window) to use it.",
		"Το orOS"+v+" εγκαταστάθηκε. Άνοιξε ξανά το "+appTitle+" (ή ανανέωσε το παράθυρό του) για να το χρησιμοποιήσεις.",
		"orOS"+v+" ist installiert. Öffne "+appTitle+" erneut (oder lade sein Fenster neu), um es zu verwenden.")
}

func updateFailedText(err error) string {
	return word("The update did not work: ", "Η ενημέρωση δεν έγινε: ", "Das Update hat nicht geklappt: ") + err.Error()
}

func checkUpdatesTitle(busy bool) string {
	if busy {
		return word("Checking for updates…", "Έλεγχος για ενημερώσεις…", "Suche nach Updates…")
	}
	return word("Check for updates", "Έλεγχος για ενημερώσεις", "Nach Updates suchen")
}

// notifyFn and warnFn show the answer (tests replace them).
var notifyFn, warnFn = notify, warn

// menuTitle is the part of a tray menu item checkForUpdates needs.
type menuTitle interface {
	SetTitle(string)
	Enable()
	Disable()
}

// checkForUpdates is the tray's "Check for updates": the Start menu's
// "Update orOS" without leaving the tray, with the answer in a message.
func (t *trayApp) checkForUpdates(item menuTitle) {
	if !updateBusy.CompareAndSwap(false, true) {
		return // the endpoint or an earlier click is at it
	}
	item.Disable()
	item.SetTitle(checkUpdatesTitle(true))
	msg, err := updateNow(t.lay)
	t.dir.Store(t.lay.appDir())
	updateBusy.Store(false)
	item.SetTitle(checkUpdatesTitle(false))
	item.Enable()
	if err != nil {
		warnFn(updateFailedText(err))
		return
	}
	notifyFn(msg)
}
