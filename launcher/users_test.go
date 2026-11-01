package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func init() { passIter = 1000 } // the real 600 000 rounds would make the tests slow

func TestPBKDF2Vectors(t *testing.T) {
	// RFC 7914 section 11 (PBKDF2-HMAC-SHA256).
	got := hex.EncodeToString(pbkdf2SHA256([]byte("passwd"), []byte("salt"), 1, 64))
	want := "55ac046e56e3089fec1691c22544b605f94185216dde0465e68b9d57c20dacbc49ca9cccf179b645991664b39d77ef317c71b845b1e30bd509112041d3a19783"
	if got != want {
		t.Fatalf("pbkdf2: %s", got)
	}
	got = hex.EncodeToString(pbkdf2SHA256([]byte("Password"), []byte("NaCl"), 80000, 64))
	want = "4ddcd8f60b98be21830cee5ef22701f9641a4418d04c0414aeff08876b34ab56a1d425a1225833549adb841b51c9b3176a272bdebba1d078478f62b397f33c8d"
	if got != want {
		t.Fatalf("pbkdf2 80000: %s", got)
	}
}

func TestPasswordsAndNames(t *testing.T) {
	for p, ok := range map[string]bool{"1234": true, "123": false, "abcdef": true, "abcde": false, "123456789012": true,
		"1234567890123": true, "κωδικός": true, strings.Repeat("a", 257): false} {
		if validPassword(p) != ok {
			t.Errorf("validPassword(%q) = %v", p, !ok)
		}
	}
	h, err := hashPassword("secret1")
	must(t, err)
	if !checkPassword(h, "secret1") || checkPassword(h, "secret2") {
		t.Fatal("checkPassword")
	}
	bad := *h
	bad.Iter = 999999999
	if checkPassword(&bad, "secret1") {
		t.Fatal("a crafted round count must be refused")
	}
	for in, want := range map[string]string{"  Μαρία ": "Μαρία", "": "", "a\nb": "", strings.Repeat("x", 41): ""} {
		got, err := cleanName(in)
		if (err != nil) != (want == "") || got != want {
			t.Errorf("cleanName(%q) = %q, %v", in, got, err)
		}
	}
	code, err := newRecoveryCode()
	must(t, err)
	if len(code) != 23 || normCode(strings.ToLower(code)) != strings.ReplaceAll(code, "-", "") {
		t.Fatalf("code %q", code)
	}
	if waitFor(4) != 0 || waitFor(5) != 30*time.Second || waitFor(6) != time.Minute || waitFor(50) != 15*time.Minute {
		t.Fatal("waitFor")
	}
}

func TestLoadUsersRefusesForeignFolders(t *testing.T) {
	data := t.TempDir()
	raw := `{"v":1,"users":[{"id":"u00000001","name":"A","dir":""},{"id":"u00000002","name":"B","dir":"../../etc"},
	{"id":"../x","name":"C","dir":"users/../x"},{"id":"u00000003","name":"D","dir":"users/u00000003"}]}`
	must(t, os.WriteFile(filepath.Join(data, usersFile), []byte(raw), 0o600))
	l := loadUsers(data)
	if len(l.Users) != 2 || l.Users[0].ID != "u00000001" || l.Users[1].ID != "u00000003" {
		t.Fatalf("kept %+v", l.Users)
	}
}

// ---------- API ----------

type apiClient struct {
	t    *testing.T
	s    *usersServer
	used map[string]bool // folders with an open browser
}

func newAPI(t *testing.T) *apiClient {
	c := &apiClient{t: t, s: newUsersServer(t.TempDir()), used: map[string]bool{}}
	c.s.inUse = func(dir string) bool { return c.used[dir] }
	return c
}

func (c *apiClient) call(name string, body any) (int, map[string]any) {
	c.t.Helper()
	method := "GET"
	var rd *bytes.Reader
	if body != nil {
		method = "POST"
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, usersPrefix+name, rd)
	r.Host = "localhost:47800"
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Origin", "http://localhost:47800")
	r.Header.Set(usersHeader, "1")
	w := httptest.NewRecorder()
	c.s.ServeHTTP(w, r)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (c *apiClient) state() map[string]any {
	_, st := c.call("state", nil)
	return st
}

func TestUsersAPIRefusesOtherSites(t *testing.T) {
	c := newAPI(t)
	for _, h := range []map[string]string{
		{"Sec-Fetch-Site": "cross-site", usersHeader: "1"},
		{"Sec-Fetch-Site": "same-origin"}, // no header on a POST
		{"Sec-Fetch-Site": "same-origin", usersHeader: "1", "Origin": "http://localhost:3000"},
	} {
		r := httptest.NewRequest("POST", usersPrefix+"add", strings.NewReader(`{}`))
		r.Host = "localhost:47800"
		for k, v := range h {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		c.s.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%v: %d", h, w.Code)
		}
	}
	if _, err := os.Stat(filepath.Join(c.s.data, usersFile)); err == nil {
		t.Fatal("users.json written")
	}
}

func TestUsersFlow(t *testing.T) {
	c := newAPI(t)
	data := c.s.data
	// Today's stick: no users, no sign-in, the data folder is the profile.
	if st := c.state(); st["enabled"] != false || st["canAdd"] != true || currentUserDir(data) != data {
		t.Fatalf("fresh state %v", st)
	}
	// Adding the first other user makes the one person there the owner,
	// keeping the data folder (dir "").
	code, out := c.call("add", map[string]any{"name": "Anna", "password": "1234", "ownerName": "Chris", "ownerPassword": "owner-pass"})
	if code != 200 || out["code"] == nil {
		t.Fatalf("add: %d %v", code, out)
	}
	recovery := out["code"].(string)
	anna := out["id"].(string)
	l := loadUsers(data)
	if len(l.Users) != 2 || !l.Users[0].Owner || l.Users[0].Dir != "" || l.Users[1].Dir != "users/"+anna || !l.enabled() {
		t.Fatalf("users %+v", l.Users)
	}
	owner := l.Users[0].ID
	if st, _ := os.Stat(filepath.Join(data, "users", anna)); st == nil || !st.IsDir() {
		t.Fatal("Anna's folder")
	}
	// The owner who added stays signed in for this session.
	if cur, ok := readCurrent(data); !ok || cur.ID != owner {
		t.Fatalf("current %v", cur)
	}
	if code, out := c.call("add", map[string]any{"name": "anna", "password": "1234", "auth": "owner-pass"}); code != 409 || out["error"] != "taken" {
		t.Fatalf("same name: %d %v", code, out)
	}
	if code, _ := c.call("add", map[string]any{"name": "Bob", "password": "12", "auth": "owner-pass"}); code != 400 {
		t.Fatal("short password")
	}
	if code, out := c.call("add", map[string]any{"name": "Bob", "password": "123456", "auth": "nope"}); code != 403 || out["error"] != "wrong" {
		t.Fatalf("wrong auth: %d %v", code, out)
	}

	// Sign out, then sign in as Anna.
	if code, _ := c.call("signout", map[string]any{}); code != 200 || !takeSignOut(data) {
		t.Fatal("signout")
	}
	clearCurrent(data)
	if code, out := c.call("login", map[string]any{"id": anna, "password": "0000"}); code != 403 || out["error"] != "wrong" {
		t.Fatalf("wrong: %d %v", code, out)
	}
	if code, _ := c.call("login", map[string]any{"id": anna, "password": "1234"}); code != 200 {
		t.Fatal("login")
	}
	if currentUserDir(data) != filepath.Join(data, "users", anna) {
		t.Fatalf("dir %s", currentUserDir(data))
	}
	// While Anna's window is open nobody else signs in.
	c.used[filepath.Join(data, "users", anna)] = true
	if code, out := c.call("login", map[string]any{"id": owner, "password": "owner-pass"}); code != 409 || out["error"] != "busy" {
		t.Fatalf("busy: %d %v", code, out)
	}
	// Anna is no owner: she cannot add or remove, but renames herself and
	// changes her own password with the old one.
	if code, _ := c.call("add", map[string]any{"name": "Bob", "password": "123456", "auth": "1234"}); code != 403 {
		t.Fatal("Anna added a user")
	}
	if code, _ := c.call("remove", map[string]any{"id": owner, "auth": "1234"}); code != 403 {
		t.Fatal("Anna removed the owner")
	}
	if code, _ := c.call("rename", map[string]any{"id": owner, "name": "X"}); code != 403 {
		t.Fatal("Anna renamed the owner")
	}
	if code, _ := c.call("rename", map[string]any{"id": anna, "name": "Άννα"}); code != 200 {
		t.Fatal("rename self")
	}
	if code, _ := c.call("password", map[string]any{"id": anna, "old": "bad", "password": "5678"}); code != 403 {
		t.Fatal("password without the old one")
	}
	if code, _ := c.call("password", map[string]any{"id": anna, "old": "1234", "password": "5678"}); code != 200 {
		t.Fatal("password")
	}
	c.used = map[string]bool{}
	clearCurrent(data)

	// Five wrong tries make the next wait; the right password waits too.
	for i := 0; i < 5; i++ {
		c.call("login", map[string]any{"id": anna, "password": "0000"})
	}
	if code, out := c.call("login", map[string]any{"id": anna, "password": "5678"}); code != 429 || out["error"] != "wait" {
		t.Fatalf("wait: %d %v", code, out)
	}
	c.s.now = func() time.Time { return time.Now().Add(time.Minute) }
	if code, _ := c.call("login", map[string]any{"id": anna, "password": "5678"}); code != 200 {
		t.Fatal("after the wait")
	}
	c.s.now = time.Now
	clearCurrent(data)

	// The owner forgot the password: the recovery code sets a new one and
	// gives a new code; the old code no longer works.
	code, out = c.call("recover", map[string]any{"code": strings.ToLower(recovery), "password": "new-owner"})
	if code != 200 || out["code"] == nil || out["code"] == recovery {
		t.Fatalf("recover: %d %v", code, out)
	}
	if code, _ := c.call("recover", map[string]any{"code": recovery, "password": "other-pass"}); code != 403 {
		t.Fatal("the old code worked twice")
	}
	if code, _ := c.call("login", map[string]any{"id": owner, "password": "new-owner"}); code != 200 {
		t.Fatal("owner login")
	}

	// Guest: on, signs in without a password into an emptied folder.
	if code, _ := c.call("guest", map[string]any{"on": true, "auth": "new-owner"}); code != 200 {
		t.Fatal("guest on")
	}
	clearCurrent(data)
	must(t, os.MkdirAll(filepath.Join(guestDir(data), "profile"), 0o700))
	must(t, os.WriteFile(filepath.Join(guestDir(data), "profile", "old"), []byte("x"), 0o600))
	if code, _ := c.call("login", map[string]any{"id": guestID}); code != 200 {
		t.Fatal("guest login")
	}
	if _, err := os.Stat(filepath.Join(guestDir(data), "profile", "old")); err == nil {
		t.Fatal("the guest found the last guest's data")
	}
	if currentUserDir(data) != guestDir(data) {
		t.Fatal("guest dir")
	}
	if code, _ := c.call("add", map[string]any{"name": "Bob", "password": "123456", "auth": "new-owner"}); code != 403 {
		t.Fatal("the guest added a user")
	}
	endSession(data)
	if _, err := os.Stat(guestDir(data)); err == nil {
		t.Fatal("guest folder kept after the session")
	}

	// The owner removes Anna (not themselves, not the first user's folder).
	writeCurrent(data, owner)
	if code, out := c.call("remove", map[string]any{"id": owner, "auth": "new-owner"}); code != 409 || out["error"] != "self" {
		t.Fatalf("self: %d %v", code, out)
	}
	c.used[filepath.Join(data, "users", anna)] = true
	if code, _ := c.call("remove", map[string]any{"id": anna, "auth": "new-owner"}); code != 409 {
		t.Fatal("removed a user whose window is open")
	}
	c.used = map[string]bool{}
	if code, _ := c.call("remove", map[string]any{"id": anna, "auth": "new-owner"}); code != 200 {
		t.Fatal("remove")
	}
	if _, err := os.Stat(filepath.Join(data, "users", anna)); err == nil {
		t.Fatal("Anna's folder kept")
	}
	// The guest is still on, so the sign-in window stays.
	if !loadUsers(data).enabled() {
		t.Fatal("guest alone keeps the sign-in")
	}
	c.call("guest", map[string]any{"on": false, "auth": "new-owner"})
	if loadUsers(data).enabled() {
		t.Fatal("one user again: no sign-in")
	}
}

func TestOwnerTransfer(t *testing.T) {
	c := newAPI(t)
	_, out := c.call("add", map[string]any{"name": "Anna", "password": "1234", "ownerName": "Chris", "ownerPassword": "owner-pass"})
	anna := out["id"].(string)
	if code, _ := c.call("owner", map[string]any{"id": anna, "auth": "owner-pass"}); code != 200 {
		t.Fatal("owner")
	}
	l := loadUsers(c.s.data)
	if l.Users[0].Owner || !l.Users[1].Owner || l.Recovery != nil {
		t.Fatalf("%+v", l)
	}
	// The first user's folder can never be removed, even by a new owner.
	writeCurrent(c.s.data, anna)
	if code, out := c.call("remove", map[string]any{"id": l.Users[0].ID, "auth": "1234"}); code != 409 || out["error"] != "first" {
		t.Fatalf("first: %d %v", code, out)
	}
	// The new owner gets a recovery code with their next password.
	if _, out := c.call("password", map[string]any{"id": anna, "old": "1234", "password": "anna-new"}); out["code"] == nil {
		t.Fatal("no recovery code")
	}
}

func TestLoginPageServed(t *testing.T) {
	h := newHandler(testApp(t))
	w := get(h, "localhost:47800", loginPath)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "/__oros/login/login.js") ||
		!strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatalf("login: %d %v", w.Code, w.Header())
	}
	if w := get(h, "localhost:47800", loginAssets+"login.js"); w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("js: %d", w.Code)
	}
	if w := get(h, "localhost:47800", loginAssets+"../users.go"); w.Code != 404 {
		t.Fatalf("escape: %d", w.Code)
	}
}

// ---------- Session ----------

// fakeBrowser: a "browser" that holds its profile (as Chromium's
// SingletonLock does) until the test signs a user in, then closes, as
// the sign-in page does.
func fakeBrowser(t *testing.T, data string) string {
	if runtime.GOOS == "windows" {
		t.Skip("shell script browser")
	}
	script := filepath.Join(t.TempDir(), "browser.sh")
	body := `#!/bin/sh
for a in "$@"; do case "$a" in --user-data-dir=*) prof="${a#--user-data-dir=}";; esac; done
mkdir -p "$prof"
ln -sf "test-$$" "$prof/SingletonLock"
echo "$@" >> "` + filepath.Join(data, "browser.log") + `"
while [ ! -f "` + filepath.Join(data, "users", currentFile) + `" ]; do sleep 0.05; done
rm -f "$prof/SingletonLock"
`
	must(t, os.WriteFile(script, []byte(body), 0o755))
	return script
}

func TestSessionSignInAndSignOut(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	must(t, os.MkdirAll(data, 0o755))
	t.Setenv("OROS_PORTABLE_BROWSER", fakeBrowser(t, data))
	sessionPoll = 20 * time.Millisecond
	lay := newLayout(root)

	// No users: the data folder's profile, no sign-in window.
	var opened []string
	launch := func(ul layout, url string, extra ...string) error {
		opened = append(opened, ul.userFolder())
		return nil
	}
	must(t, runSession(lay, appURL, nil, launch))
	if len(opened) != 1 || opened[0] != data {
		t.Fatalf("opened %v", opened)
	}
	if _, err := os.Stat(filepath.Join(data, "browser.log")); err == nil {
		t.Fatal("a sign-in window opened without users")
	}

	// Two users: the sign-in window, then Anna's folder; she signs out, the
	// sign-in window comes back, the owner signs in and closes the window.
	s := newUsersServer(data)
	h := &apiClient{t: t, s: s, used: map[string]bool{}}
	_, out := h.call("add", map[string]any{"name": "Anna", "password": "1234", "ownerName": "Chris", "ownerPassword": "owner-pass"})
	anna := out["id"].(string)
	owner := loadUsers(data).Users[0].ID
	clearCurrent(data)
	who := []string{anna, owner}
	opened = nil
	go func() {
		for i := 0; i < len(who); i++ {
			// Wait for a sign-in window with nobody signed in, then sign
			// in as the next user.
			for {
				_, signed := readCurrent(data)
				if !signed && profilesInUse(filepath.Join(data, loginFolder)) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			pw := map[string]string{anna: "1234", owner: "owner-pass"}[who[i]]
			h.call("login", map[string]any{"id": who[i], "password": pw})
		}
	}()
	launch = func(ul layout, url string, extra ...string) error {
		opened = append(opened, ul.userFolder())
		if len(opened) == 1 {
			must(t, markSignOut(data)) // Anna signs out
		} else if url != appURL || len(extra) != 0 {
			t.Errorf("after a sign-out the main window opens, not %s %v", url, extra)
		}
		return nil
	}
	done := make(chan error)
	go func() { done <- runSession(lay, quickURL, quickArgs(), launch) }()
	select {
	case err := <-done:
		must(t, err)
	case <-time.After(20 * time.Second):
		t.Fatal("session hangs")
	}
	if len(opened) != 2 || opened[0] != filepath.Join(data, "users", anna) || opened[1] != data {
		t.Fatalf("opened %v", opened)
	}
	if _, ok := readCurrent(data); ok {
		t.Fatal("current.json left after the session")
	}
	log, _ := os.ReadFile(filepath.Join(data, "browser.log"))
	if !strings.Contains(string(log), "--user-data-dir="+filepath.Join(data, loginFolder, "profile")) ||
		!strings.Contains(string(log), "--app="+loginURL) {
		t.Fatalf("sign-in window: %s", log)
	}
}

func TestSessionJoinsTheOpenUser(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	s := newUsersServer(data)
	h := &apiClient{t: t, s: s, used: map[string]bool{}}
	_, out := h.call("add", map[string]any{"name": "Anna", "password": "1234", "ownerName": "Chris", "ownerPassword": "owner-pass"})
	anna := out["id"].(string)
	writeCurrent(data, anna)
	prof := filepath.Join(data, "users", anna, "profile")
	must(t, os.MkdirAll(prof, 0o700))
	if runtime.GOOS == "windows" {
		t.Skip("lock links")
	}
	must(t, os.Symlink("test-"+itoa(os.Getpid()), filepath.Join(prof, "SingletonLock")))
	lay := newLayout(root)
	if lay.userFolder() != filepath.Join(data, "users", anna) {
		t.Fatalf("newLayout user %s", lay.userFolder())
	}
	var got string
	err := runSession(lay, quickURL, nil, func(ul layout, url string, extra ...string) error {
		got = ul.userFolder()
		return nil
	})
	must(t, err)
	if got != filepath.Join(data, "users", anna) {
		t.Fatalf("joined %s", got)
	}
	if _, ok := readCurrent(data); !ok {
		t.Fatal("a joined window must not end Anna's session")
	}
	// Her Internet window and her "This computer" answer are hers too.
	if !strings.Contains(strings.Join(webArgs(lay), " "), filepath.Join(data, "users", anna, webProfile)+" ") {
		t.Fatal("web profile")
	}
	if c := newFSConsent(lay, false); c.path() != filepath.Join(data, "users", anna, consentFile) {
		t.Fatalf("consent %s", c.path())
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func TestUSBHomes(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, "oros", "data")
	c := newAPI(t)
	c.s.data, c.s.home = data, home
	must(t, os.MkdirAll(data, 0o755))
	if userHome(home, data) != home {
		t.Fatal("one user: the computer's home")
	}
	_, out := c.call("add", map[string]any{"name": "Anna/B", "password": "1234", "ownerName": "Chris", "ownerPassword": "owner-pass"})
	anna := out["id"].(string)
	owner := loadUsers(data).Users[0].ID
	if userHome(home, data) != home {
		t.Fatal("the owner keeps the computer's home")
	}
	writeCurrent(data, anna)
	h := userHome(home, data)
	if h != filepath.Join(home, "oros", "homes", anna) {
		t.Fatalf("Anna's home %s", h)
	}
	if st, err := os.Stat(filepath.Join(h, "Documents")); err != nil || !st.IsDir() {
		t.Fatal("usual folders")
	}
	must(t, os.WriteFile(filepath.Join(h, "Documents", "essay.txt"), []byte("mine"), 0o600))
	// Removed: her files land in the owner's home, nothing is lost.
	writeCurrent(data, owner)
	if code, _ := c.call("remove", map[string]any{"id": anna, "auth": "owner-pass"}); code != 200 {
		t.Fatal("remove")
	}
	b, err := os.ReadFile(filepath.Join(home, "Anna_B (removed user)", "Documents", "essay.txt"))
	if err != nil || string(b) != "mine" {
		t.Fatalf("kept home: %v", err)
	}
	// The guest's home goes with the guest's session.
	c.call("guest", map[string]any{"on": true, "auth": "owner-pass"})
	clearCurrent(data)
	c.call("login", map[string]any{"id": guestID})
	gh := userHome(home, data)
	must(t, os.WriteFile(filepath.Join(gh, "Documents", "x"), []byte("x"), 0o600))
	endSession(data)
	if _, err := os.Stat(gh); err == nil {
		t.Fatal("guest home kept")
	}
}
