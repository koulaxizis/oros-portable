package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// When port 47800 is taken. The port cannot move (it is part of the
// origin, which owns the data), so the launcher finds out who holds it:
//
//   - another orOS that answers (a window, the tray): open a window only;
//   - an orOS that is still closing, or a setup that restarts orOS: wait
//     a little, the port comes free;
//   - an orOS launcher that holds the port but no longer answers (stuck
//     after an update): offer to close it and start;
//   - orOS Drive waiting for the Dropbox sign-in: say so;
//   - Windows' reserved port ranges (Hyper-V, WSL, Docker): say how to
//     free them;
//   - any other program: name it.

// portWait is how long a start waits for the port to come free.
var portWait = 10 * time.Second

// errPortBusy: the port stays taken; fail() has told the user.
var errPortBusy = errors.New("port busy")

// portOwner is the process that listens on a port of this computer.
type portOwner struct {
	pid int
	exe string // full path or name; "" when it cannot be read
}

func (o portOwner) name() string {
	n := filepath.Base(strings.ReplaceAll(o.exe, `\`, "/"))
	if o.exe == "" || n == "." || n == "/" {
		return ""
	}
	return n
}

// ownLauncher: the owner is an orOS launcher, this one's program or
// another copy (installed and portable both run orOS.exe).
func (o portOwner) ownLauncher() bool {
	n := strings.TrimSuffix(strings.ToLower(o.name()), ".exe")
	if n == "" {
		return false
	}
	if self, err := os.Executable(); err == nil {
		if strings.TrimSuffix(strings.ToLower(filepath.Base(self)), ".exe") == n {
			return true
		}
	}
	return n == "oros" || n == "oros-portable"
}

func (o portOwner) drive() bool {
	n := strings.TrimSuffix(strings.ToLower(o.name()), ".exe")
	return n == "orosdrive" || n == "oros-drive"
}

// claimPort starts the server. It returns nil servers and no error when
// another orOS serves already, so the caller only opens a window.
func claimPort(appDir string) (servers, error) {
	srv, err := waitForPort(appDir)
	if err == nil || srv != nil {
		return srv, err
	}
	if alreadyRunning() {
		return nil, nil
	}
	log.Printf("port %d: %v", port, err)
	owner, found := findPortOwner(port)
	if found {
		log.Printf("port %d is held by pid %d (%s)", port, owner.pid, owner.exe)
	}
	if found && owner.ownLauncher() && owner.pid != os.Getpid() {
		if ask(word(
			"An earlier orOS is still running but no longer answers, so the new one cannot start. Close the earlier orOS and start? Your data is safe: it stays in the browser profile.",
			"Ένα προηγούμενο orOS τρέχει ακόμα αλλά δεν αποκρίνεται, οπότε το νέο δεν μπορεί να ξεκινήσει. Να κλείσει το προηγούμενο orOS και να ξεκινήσει; Τα δεδομένα σου είναι ασφαλή: μένουν στο προφίλ του browser.",
			"Ein früheres orOS läuft noch, antwortet aber nicht mehr, deshalb kann das neue nicht starten. Das frühere orOS schließen und starten? Deine Daten sind sicher: Sie bleiben im Browserprofil.")) {
			if err := killProcess(owner.pid); err != nil {
				log.Printf("closing pid %d: %v", owner.pid, err)
			}
			if srv, err := waitForPort(appDir); err == nil || srv != nil {
				return srv, err
			}
			owner, found = findPortOwner(port)
		}
	}
	fail(portBusyMessage(owner, found, accessDenied(err)))
	return nil, errPortBusy
}

// waitForPort tries to listen until portWait runs out. A launcher or tray
// that is closing, or a setup that starts orOS again, frees the port.
func waitForPort(appDir string) (servers, error) {
	deadline := time.Now().Add(portWait)
	for {
		srv, err := startServer(appDir)
		if err == nil {
			return srv, nil
		}
		if alreadyRunning() || !time.Now().Before(deadline) {
			return nil, err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// portBusyMessage says who holds the port and what to do, in the user's
// language. reserved: Windows refused the port itself (access denied).
func portBusyMessage(o portOwner, found, reserved bool) string {
	switch {
	case found && o.drive():
		return word(
			fmt.Sprintf("orOS Drive is waiting for the Dropbox sign-in on port %d, which orOS needs. Finish or cancel the sign-in in orOS Drive (it stops by itself after 10 minutes), then start orOS again.", port),
			fmt.Sprintf("Το orOS Drive περιμένει τη σύνδεση στο Dropbox στη θύρα %d, που τη χρειάζεται το orOS. Ολοκλήρωσε ή ακύρωσε τη σύνδεση στο orOS Drive (σταματά μόνη της σε 10 λεπτά) και άνοιξε ξανά το orOS.", port),
			fmt.Sprintf("orOS Drive wartet auf Port %d auf die Dropbox-Anmeldung, und orOS braucht diesen Port. Schließe die Anmeldung in orOS Drive ab oder brich sie ab (sie endet nach 10 Minuten von selbst) und starte orOS erneut.", port))
	case found && o.name() != "":
		return word(
			fmt.Sprintf("Port %d is used by %s (process %d), so orOS cannot start. Close that program (Task Manager) and try again.", port, o.name(), o.pid),
			fmt.Sprintf("Τη θύρα %d τη χρησιμοποιεί το %s (διεργασία %d), οπότε το orOS δεν μπορεί να ξεκινήσει. Κλείσε αυτό το πρόγραμμα (Διαχείριση εργασιών) και δοκίμασε ξανά.", port, o.name(), o.pid),
			fmt.Sprintf("Port %d wird von %s (Prozess %d) verwendet, deshalb kann orOS nicht starten. Schließe dieses Programm (Task-Manager) und versuche es erneut.", port, o.name(), o.pid))
	case reserved:
		return word(
			fmt.Sprintf("Windows has reserved port %d for itself (Hyper-V, WSL or Docker do this), so orOS cannot start. Restart the computer; if that does not help, run \"net stop winnat\" and then \"net start winnat\" in a Command Prompt opened as administrator.", port),
			fmt.Sprintf("Τα Windows έχουν δεσμεύσει τη θύρα %d για τον εαυτό τους (το κάνουν τα Hyper-V, WSL ή Docker), οπότε το orOS δεν μπορεί να ξεκινήσει. Κάνε επανεκκίνηση του υπολογιστή· αν δεν βοηθήσει, τρέξε \"net stop winnat\" και μετά \"net start winnat\" σε Γραμμή εντολών ως διαχειριστής.", port),
			fmt.Sprintf("Windows hat Port %d für sich reserviert (das tun Hyper-V, WSL oder Docker), deshalb kann orOS nicht starten. Starte den Computer neu; hilft das nicht, führe \"net stop winnat\" und dann \"net start winnat\" in einer Eingabeaufforderung als Administrator aus.", port))
	}
	return word(
		fmt.Sprintf("Port %d is used by another program, so orOS cannot start. Close that program and try again, or restart the computer.", port),
		fmt.Sprintf("Τη θύρα %d τη χρησιμοποιεί άλλο πρόγραμμα, οπότε το orOS δεν μπορεί να ξεκινήσει. Κλείσε εκείνο το πρόγραμμα και δοκίμασε ξανά, ή κάνε επανεκκίνηση του υπολογιστή.", port),
		fmt.Sprintf("Port %d wird von einem anderen Programm verwendet, deshalb kann orOS nicht starten. Schließe dieses Programm und versuche es erneut, oder starte den Computer neu.", port))
}

func killProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
