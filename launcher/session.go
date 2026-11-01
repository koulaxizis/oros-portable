package main

// Whose orOS opens (users.go): the session loop of a window launcher and
// of the tray.
//
//   - One user (no users.json, or one user and no guest): the window opens
//     on the data folder's profiles, exactly as before users existed.
//   - Users: a user whose window is open already is joined (a second
//     window, the small window); otherwise the sign-in window opens in
//     data/login and the launcher waits for current.json (written by the
//     users API when the password was right), closes the sign-in window
//     and opens that user's orOS.
//   - When that user's browser closes, the session ends (current.json goes,
//     the guest's folder is emptied). If the user asked to sign out
//     (POST /__oros/users/signout), the sign-in window comes back;
//     otherwise the launcher ends as it always did.

import (
	"errors"
	"log"
	"os/exec"
	"path/filepath"
	"time"
)

const loginURL = "http://localhost:47800" + loginPath

// sessionPoll: how often the launcher looks for a sign-in; tests lower it.
var sessionPoll = 300 * time.Millisecond

// launchFunc opens orOS for one user and returns when that user's
// browser is closed.
type launchFunc func(lay layout, url string, extra ...string) error

// activeUser: the user named in current.json, while a browser has their
// profile open.
func activeUser(data string) (string, bool) {
	l := loadUsers(data)
	if !l.enabled() {
		return "", false
	}
	c, ok := readCurrent(data)
	if !ok {
		return "", false
	}
	dir := currentUserDir(data)
	if c.ID != guestID && l.find(c.ID) == nil {
		return "", false
	}
	if c.ID == guestID && !l.Guest {
		return "", false
	}
	return dir, profilesInUse(dir)
}

// runSession signs a user in and opens orOS for them, again after each
// sign-out, until a browser closes without one.
func runSession(lay layout, url string, extra []string, launch launchFunc) error {
	for {
		ul, joined, ok := signIn(lay)
		if !ok {
			return nil // the sign-in window was closed
		}
		err := launch(ul, url, extra...)
		if joined {
			return err // the session that opened first ends it
		}
		endSession(lay.data)
		if err != nil {
			return err
		}
		if !takeSignOut(lay.data) || !loadUsers(lay.data).enabled() {
			return nil
		}
		// The next user must not find this one's Internet window open.
		closeProfile(filepath.Join(ul.userFolder(), webFolder))
		log.Printf("users: signed out, showing the sign-in window")
		url, extra = appURL, nil
	}
}

// endSession forgets who was signed in and empties the guest's folder.
func endSession(data string) {
	if c, ok := readCurrent(data); ok && c.ID == guestID {
		if err := wipeGuest(data); err != nil {
			log.Printf("users: guest folder: %v", err)
		}
	}
	clearCurrent(data)
}

// signIn returns the layout of the user whose orOS opens. joined: that
// user's window was open already. ok false: nobody signed in.
func signIn(lay layout) (layout, bool, bool) {
	if !loadUsers(lay.data).enabled() {
		return lay.forUser(lay.data), false, true
	}
	if dir, ok := activeUser(lay.data); ok {
		return lay.forUser(dir), true, true
	}
	login := lay.forUser(filepath.Join(lay.data, loginFolder))
	if profilesInUse(login.user) {
		// Another launcher shows the sign-in window: wait for it.
		waitUserClosed(login.user, 0)
		if dir, ok := activeUser(lay.data); ok {
			return lay.forUser(dir), true, true
		}
		return lay, false, false
	}
	clearCurrent(lay.data)
	takeSignOut(lay.data) // a sign-out that never closed its window
	start := time.Now().UnixMilli()
	cmd, profile, err := startLogin(lay, login)
	if err != nil {
		notify(err.Error())
		return lay, false, false
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	ended := false
	tick := time.NewTicker(sessionPoll)
	defer tick.Stop()
	for {
		select {
		case <-exited:
			ended, exited = true, nil
		case <-tick.C:
		}
		if c, ok := readCurrent(lay.data); ok && c.At >= start-1000 {
			closeLogin(cmd, ended, exited, profile)
			log.Printf("users: %s signed in", c.ID)
			return lay.forUser(currentUserDir(lay.data)), false, true
		}
		if ended && !profileInUse(profile) {
			log.Printf("users: the sign-in window was closed")
			return lay, false, false
		}
	}
}

// startLogin opens the sign-in window in its own profile, with the first
// browser that starts.
func startLogin(lay, login layout) (*exec.Cmd, string, error) {
	list := browserCandidates(login)
	if len(list) == 0 {
		return nil, "", errors.New(word("No browser was found.", "Δεν βρέθηκε browser.", "Kein Browser gefunden."))
	}
	extra := []string{"--window-size=560,760"}
	if loadSession(lay).Fullscreen {
		extra = []string{"--start-fullscreen"}
	}
	for _, c := range list {
		args := append(append([]string{}, extra...), sysBrowserArgs(login, c, loginURL)...)
		log.Printf("users: sign-in window with %s", c.path)
		cmd := exec.Command(c.path, browserArgs(c.profile, loginURL, args...)...)
		if err := cmd.Start(); err != nil {
			log.Printf("browser: %v, trying the next one", err)
			continue
		}
		return cmd, c.profile, nil
	}
	return nil, "", errors.New(word("None of the browsers could start.", "Κανένας browser δεν ξεκίνησε.", "Keiner der Browser konnte starten."))
}

// closeLogin: the page closes itself; a window that stays is ended (its
// profile holds nothing but the sign-in page).
func closeLogin(cmd *exec.Cmd, ended bool, exited chan struct{}, profile string) {
	if !ended {
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			killTree(cmd.Process)
			<-exited
		}
	}
	for i := 0; i < 50 && profileInUse(profile); i++ {
		time.Sleep(200 * time.Millisecond)
	}
}

// waitUserClosed waits until no browser holds the user's profiles: first
// up to appear (a browser takes a while to lock a new profile), then for
// it to close.
func waitUserClosed(dir string, appear time.Duration) {
	for end := time.Now().Add(appear); time.Now().Before(end) && !profilesInUse(dir); {
		time.Sleep(time.Second)
	}
	for profilesInUse(dir) {
		time.Sleep(time.Second)
	}
}
