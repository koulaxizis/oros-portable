package main

// Separate users on one computer or stick (orOS Bible "Users on one
// device", plan /mnt/project-files/users/plan.md, waves U1-U4).
//
// A user is a browser profile of their own: the launcher opens Chromium
// with --user-data-dir in that user's folder, so localStorage, IndexedDB,
// the orOS disk (OPFS), cookies and the Dropbox or WebDAV sign-in are all
// theirs. orOS and its sync are unchanged; only the launcher chooses the
// folder.
//
//   - Without users.json, or with one user, nothing changes: no sign-in
//     window, the browser profiles stay where they always were (data/).
//   - The first user keeps those folders as they are (dir ""), so adding
//     a second user moves no data. Every other user gets data/users/<id>/
//     with the same layout (profile, profile-system, web, session.json,
//     computer-files.json).
//   - With two users or more (or the guest on), each start shows the
//     sign-in window (usersapi.go, login/) in its own empty profile
//     (data/login), so signing in loads nobody's data.
//   - One user at a time. data/users/current.json names the user who is
//     signed in; it is valid while that user's browser profile is open.
//     Every launcher process (the native host, the Internet window, the
//     tray) reads it through newLayout.
//   - The guest's folder is emptied before and after each guest session.
//
// Passwords: a PIN of 4-12 digits or at least 6 characters (as orOS's
// screen lock). Only a slow hash is kept: PBKDF2-HMAC-SHA-256, 600 000
// rounds, 16-byte random salt. After 5 wrong tries each try waits (30 s,
// doubling, at most 15 minutes). The owner gets a recovery code when
// their password is first set; it sets a new password from the sign-in
// window. Honest limit (until U4): the password keeps people away from
// the screen, the folders on the stick are not encrypted by it.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	usersFile   = "users.json"
	usersFolder = "users"
	currentFile = "current.json"
	signoutFile = "signout"
	loginFolder = "login"
	guestID     = "guest"
	maxUsers    = 8
	freeTries   = 5
)

var (
	// passIter is the PBKDF2 round count; tests lower it.
	passIter  = 600000
	waitBase  = 30 * time.Second
	waitMax   = 15 * time.Minute
	pinRe     = regexp.MustCompile(`^[0-9]{4,12}$`)
	userIDRe  = regexp.MustCompile(`^u[0-9a-f]{8}$`)
	errDenied = errors.New("denied")
)

// passHash is a stored password (or recovery code).
type passHash struct {
	Alg  string `json:"alg"` // "pbkdf2-sha256"
	Iter int    `json:"iter"`
	Salt string `json:"salt"` // base64
	Key  string `json:"key"`  // base64, 32 bytes
}

type orosUser struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Owner   bool      `json:"owner,omitempty"`
	Dir     string    `json:"dir"` // "" = the data folder itself (the first user)
	Pass    *passHash `json:"pass,omitempty"`
	Fails   int       `json:"fails,omitempty"`
	Until   int64     `json:"until,omitempty"` // unix ms: no try before this
	Created int64     `json:"created,omitempty"`
}

// userList is users.json.
type userList struct {
	V        int         `json:"v"`
	Users    []*orosUser `json:"users"`
	Guest    bool        `json:"guest,omitempty"`
	Recovery *passHash   `json:"recovery,omitempty"` // the owner's recovery code
}

// enabled: the sign-in window is shown.
func (l *userList) enabled() bool {
	return l != nil && (len(l.Users) >= 2 || (l.Guest && len(l.Users) >= 1))
}

func (l *userList) find(id string) *orosUser {
	if l == nil {
		return nil
	}
	for _, u := range l.Users {
		if u.ID == id {
			return u
		}
	}
	return nil
}

func (l *userList) owner() *orosUser {
	if l == nil {
		return nil
	}
	for _, u := range l.Users {
		if u.Owner {
			return u
		}
	}
	return nil
}

// ---------- Names and passwords (pure) ----------

// cleanName trims a display name and refuses an empty, overlong or
// control-character one.
func cleanName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || utf8.RuneCountInString(s) > 40 || !utf8.ValidString(s) {
		return "", errors.New("name")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", errors.New("name")
		}
	}
	return s, nil
}

// nameTaken: another user has this name (case does not count).
func (l *userList) nameTaken(name, except string) bool {
	for _, u := range l.Users {
		if u.ID != except && strings.EqualFold(u.Name, name) {
			return true
		}
	}
	return strings.EqualFold(name, "guest") || strings.EqualFold(name, "επισκέπτης") || strings.EqualFold(name, "gast")
}

// validPassword: a PIN of 4-12 digits or at least 6 characters, at most 256.
func validPassword(p string) bool {
	if utf8.RuneCountInString(p) > 256 {
		return false
	}
	return pinRe.MatchString(p) || utf8.RuneCountInString(p) >= 6
}

// pbkdf2SHA256 is RFC 8018 PBKDF2 with HMAC-SHA-256 (Go 1.22 has no
// crypto/pbkdf2 yet).
func pbkdf2SHA256(pass, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, pass)
	hLen := prf.Size()
	n := (keyLen + hLen - 1) / hLen
	out := make([]byte, 0, n*hLen)
	buf := make([]byte, 4)
	u := make([]byte, hLen)
	for block := 1; block <= n; block++ {
		prf.Reset()
		prf.Write(salt)
		buf[0], buf[1], buf[2], buf[3] = byte(block>>24), byte(block>>16), byte(block>>8), byte(block)
		prf.Write(buf)
		u = prf.Sum(u[:0])
		t := append([]byte{}, u...)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(u[:0])
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func hashPassword(p string) (*passHash, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := pbkdf2SHA256([]byte(p), salt, passIter, 32)
	return &passHash{Alg: "pbkdf2-sha256", Iter: passIter,
		Salt: base64.StdEncoding.EncodeToString(salt), Key: base64.StdEncoding.EncodeToString(key)}, nil
}

// checkPassword compares in constant time. A crafted round count must not
// hang the launcher.
func checkPassword(h *passHash, p string) bool {
	if h == nil || h.Alg != "pbkdf2-sha256" || h.Iter < 1000 || h.Iter > 5000000 {
		return false
	}
	salt, err1 := base64.StdEncoding.DecodeString(h.Salt)
	key, err2 := base64.StdEncoding.DecodeString(h.Key)
	if err1 != nil || err2 != nil || len(key) != 32 {
		return false
	}
	got := pbkdf2SHA256([]byte(p), salt, h.Iter, 32)
	return subtle.ConstantTimeCompare(got, key) == 1
}

// newRecoveryCode: 20 letters and digits in four groups (base32, no
// padding), easy to write down.
func newRecoveryCode() (string, error) {
	b := make([]byte, 13)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)[:20]
	return s[0:5] + "-" + s[5:10] + "-" + s[10:15] + "-" + s[15:20], nil
}

// normCode lets the code be typed in any case, with or without dashes.
func normCode(s string) string {
	s = strings.ToUpper(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// waitFor is how long the next try must wait after n wrong ones.
func waitFor(n int) time.Duration {
	if n < freeTries {
		return 0
	}
	d := waitBase
	for i := freeTries; i < n && d < waitMax; i++ {
		d *= 2
	}
	if d > waitMax {
		d = waitMax
	}
	return d
}

func newUserID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "u" + hex.EncodeToString(b), nil
}

// ---------- Storage ----------

func loadUsers(data string) *userList {
	l := &userList{V: 1}
	b, err := os.ReadFile(filepath.Join(data, usersFile))
	if err != nil {
		return l
	}
	if json.Unmarshal(b, l) != nil {
		return &userList{V: 1}
	}
	// Keep only well-formed entries: a hand-edited file must never point
	// a profile outside the data folder.
	var keep []*orosUser
	for _, u := range l.Users {
		if u == nil || !(userIDRe.MatchString(u.ID)) {
			continue
		}
		if u.Dir != "" && u.Dir != usersFolder+"/"+u.ID {
			continue
		}
		keep = append(keep, u)
	}
	l.Users = keep
	return l
}

// saveUsers writes users.json through a temporary file and a rename, so a
// cut-off write never leaves half a file.
func saveUsers(data string, l *userList) error {
	l.V = 1
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(data, 0o755); err != nil {
		return err
	}
	file := filepath.Join(data, usersFile)
	tmp := file + ".tmp"
	if err := writeSync(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

func writeSync(name string, b []byte, mode os.FileMode) error {
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// userDir is the folder that holds a user's profiles.
func userDir(data string, u *orosUser) string {
	if u == nil || u.Dir == "" {
		return data
	}
	return filepath.Join(data, filepath.FromSlash(u.Dir))
}

func guestDir(data string) string { return filepath.Join(data, usersFolder, guestID) }

// signedIn is current.json.
type signedIn struct {
	ID string `json:"id"`
	At int64  `json:"at"` // unix ms of the sign-in
}

func readCurrent(data string) (signedIn, bool) {
	var c signedIn
	b, err := os.ReadFile(filepath.Join(data, usersFolder, currentFile))
	if err != nil || json.Unmarshal(b, &c) != nil || c.ID == "" {
		return signedIn{}, false
	}
	return c, true
}

func writeCurrent(data, id string) error {
	dir := filepath.Join(data, usersFolder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(signedIn{ID: id, At: time.Now().UnixMilli()})
	file := filepath.Join(dir, currentFile)
	if err := writeSync(file+".tmp", b, 0o644); err != nil {
		return err
	}
	return os.Rename(file+".tmp", file)
}

func clearCurrent(data string) { _ = os.Remove(filepath.Join(data, usersFolder, currentFile)) }

// markSignOut: the user asked to sign out; the session that waits for the
// browser shows the sign-in window again instead of ending.
func markSignOut(data string) error {
	dir := filepath.Join(data, usersFolder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, signoutFile), []byte("1"), 0o644)
}

func takeSignOut(data string) bool {
	return os.Remove(filepath.Join(data, usersFolder, signoutFile)) == nil
}

// currentUserDir is the folder of the user who is signed in, or the data
// folder (one user, nobody signed in yet, or a stale name).
func currentUserDir(data string) string {
	l := loadUsers(data)
	if !l.enabled() {
		return data
	}
	c, ok := readCurrent(data)
	if !ok {
		return data
	}
	if c.ID == guestID && l.Guest {
		return guestDir(data)
	}
	if u := l.find(c.ID); u != nil {
		return userDir(data, u)
	}
	return data
}

// profilesInUse: a browser holds one of the user's orOS profiles.
func profilesInUse(dir string) bool {
	for _, p := range []string{"profile", "profile-system"} {
		if profileInUse(filepath.Join(dir, p)) {
			return true
		}
	}
	return false
}

// anyProfileInUse: some user's window (or the sign-in window) is open.
func anyProfileInUse(data string) bool {
	if profilesInUse(data) || profilesInUse(filepath.Join(data, loginFolder)) || profilesInUse(guestDir(data)) {
		return true
	}
	for _, u := range loadUsers(data).Users {
		if u.Dir != "" && profilesInUse(userDir(data, u)) {
			return true
		}
	}
	return false
}

// wipeGuest empties the guest's folder. Only that folder: its path is
// fixed and checked.
func wipeGuest(data string) error {
	g := guestDir(data)
	if filepath.Base(g) != guestID || filepath.Base(filepath.Dir(g)) != usersFolder {
		return errors.New("guest folder")
	}
	if err := os.RemoveAll(filepath.Join(homesDir(data), guestID)); err != nil {
		return err
	}
	return os.RemoveAll(g)
}

// homesDir holds the own homes of orOS USB's users (next to data, inside
// ~/oros, which "This computer" never shows).
func homesDir(data string) string { return filepath.Join(filepath.Dir(data), "homes") }

// userHome is the folder behind "This computer › Home" on orOS USB: the
// first user keeps the computer's home; everybody else gets their own,
// with the usual folders.
func userHome(home, data string) string {
	dir := currentUserDir(data)
	if dir == data {
		return home
	}
	h := filepath.Join(homesDir(data), filepath.Base(dir))
	for _, d := range []string{"Documents", "Downloads", "Pictures", "Music", "Videos", "Desktop"} {
		_ = os.MkdirAll(filepath.Join(h, d), 0o700)
	}
	return h
}

// keepHome moves a removed user's home into the first user's home as
// "<name> (removed user)", so no file is lost with the user.
func keepHome(home, data string, u *orosUser) {
	h := filepath.Join(homesDir(data), u.ID)
	if _, err := os.Stat(h); err != nil || home == "" {
		return
	}
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, u.Name)
	if strings.HasPrefix(name, ".") {
		name = "_" + name
	}
	to := filepath.Join(home, name+" (removed user)")
	for i := 2; i < 100; i++ {
		if _, err := os.Lstat(to); err != nil {
			break
		}
		to = filepath.Join(home, fmt.Sprintf("%s (removed user %d)", name, i))
	}
	if err := os.Rename(h, to); err != nil {
		log.Printf("users: keeping the home of %s: %v", u.ID, err)
	}
}
