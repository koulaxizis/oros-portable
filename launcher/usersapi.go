package main

// The users API under /__oros/users/ and the sign-in window at
// /__oros/login (users.go has the model).
//
// Who can call it: only pages of this server. Every request needs the
// Host check that the whole server does (no DNS rebinding),
// Sec-Fetch-Site: same-origin and, on the same Origin, the header
// X-Oros-Users: 1 on everything but GET (another origin cannot send it
// without a CORS preflight that this server never answers). The Internet
// window has another origin for every site, so it cannot reach it.
//
// Which user is calling: the one signed in (current.json). Only one
// browser profile is open at a time, so the open orOS page belongs to
// that user. What changes another user, the guest or the owner role also
// needs the owner's password in "auth".
//
//	GET  state     {enabled, me, users[], guest, max, lang, canAdd}
//	POST login     {id, password}          the sign-in window
//	POST recover   {code, password}        the owner, with the recovery code
//	POST signout   {}                      back to the sign-in window
//	POST add       {name, password, auth | ownerName + ownerPassword}
//	POST remove    {id, auth}
//	POST password  {id, password, old | auth}
//	POST rename    {id, name}
//	POST owner     {id, auth}
//	POST guest     {on, auth}
//	GET  sizes     {id: bytes}
//
// Errors are {"error": code}: denied, bad, name, taken, password, wrong
// (with "wait" in ms when tries must wait), wait, full, busy, self, first.

import (
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	usersPrefix = "/__oros/users/"
	loginPath   = "/__oros/login"
	loginAssets = "/__oros/login/"
	usersHeader = "X-Oros-Users"
)

//go:embed login
var loginFS embed.FS

type usersServer struct {
	mu   sync.Mutex
	data string
	// inUse tells whether a user's folder has a browser open; tests set it.
	inUse func(dir string) bool
	now   func() time.Time
	// home: orOS USB, where users have homes of their own (users.go
	// userHome); "" elsewhere.
	home string
}

var usersAPI = newUsersAPIFromEnv()

func newUsersAPIFromEnv() *usersServer {
	root, err := rootDir()
	if err != nil {
		return nil
	}
	s := newUsersServer(newLayout(root).data)
	if os.Getenv("OROS_COMPUTER_FILES") == "1" {
		s.home, _ = os.UserHomeDir()
	}
	return s
}

func newUsersServer(data string) *usersServer {
	return &usersServer{data: data, inUse: profilesInUse, now: time.Now}
}

func (s *usersServer) allowed(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
		return false
	}
	if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
		return false
	}
	return r.Method == http.MethodGet || r.Header.Get(usersHeader) == "1"
}

// serveLogin sends the sign-in window and its files, with a strict CSP:
// nothing runs but its own script, and the page cannot be framed.
func serveLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := "login/index.html"
	if r.URL.Path != loginPath {
		name = "login/" + strings.TrimPrefix(r.URL.Path, loginAssets)
	}
	b, err := fs.ReadFile(loginFS, name)
	if err != nil || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	if t, ok := mimeTypes[strings.ToLower(filepath.Ext(name))]; ok {
		w.Header().Set("Content-Type", t)
	}
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = w.Write(b)
}

type publicUser struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Owner    bool   `json:"owner,omitempty"`
	Guest    bool   `json:"guest,omitempty"`
	First    bool   `json:"first,omitempty"` // keeps the data folder: cannot be removed
	Password bool   `json:"password"`
	Wait     int64  `json:"wait,omitempty"` // ms until the next try
	Active   bool   `json:"active,omitempty"`
}

type usersReq struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Password      string `json:"password"`
	Old           string `json:"old"`
	Auth          string `json:"auth"`
	Code          string `json:"code"`
	On            bool   `json:"on"`
	OwnerName     string `json:"ownerName"`
	OwnerPassword string `json:"ownerPassword"`
}

func (s *usersServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.allowed(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "denied"})
		return
	}
	name := strings.TrimPrefix(r.URL.Path, usersPrefix)
	if r.Method == http.MethodGet {
		switch name {
		case "state":
			s.mu.Lock()
			st := s.state()
			s.mu.Unlock()
			writeJSON(w, 200, st)
		case "sizes":
			writeJSON(w, 200, s.sizes())
		default:
			http.NotFound(w, r)
		}
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var q usersReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&q); err != nil {
		writeJSON(w, 400, map[string]string{"error": "bad"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out map[string]any
	var code int
	switch name {
	case "login":
		out, code = s.login(q)
	case "recover":
		out, code = s.recover(q)
	case "signout":
		out, code = s.signout()
	case "add":
		out, code = s.add(q)
	case "remove":
		out, code = s.remove(q)
	case "password":
		out, code = s.password(q)
	case "rename":
		out, code = s.rename(q)
	case "owner":
		out, code = s.setOwner(q)
	case "guest":
		out, code = s.setGuest(q)
	default:
		http.NotFound(w, r)
		return
	}
	writeJSON(w, code, out)
}

func uFail(code string, status int) (map[string]any, int) {
	return map[string]any{"error": code}, status
}

func uDone(extra map[string]any) (map[string]any, int) {
	out := map[string]any{"ok": true}
	for k, v := range extra {
		out[k] = v
	}
	return out, 200
}

// me is the user who is calling: the one signed in, or (without the
// sign-in window) the owner. nil: nobody (the sign-in window is open, or
// one user without users.json).
func (s *usersServer) me(l *userList) *orosUser {
	if !l.enabled() {
		return l.owner()
	}
	c, ok := readCurrent(s.data)
	if !ok {
		return nil
	}
	return l.find(c.ID)
}

func (s *usersServer) guestSignedIn(l *userList) bool {
	c, ok := readCurrent(s.data)
	return ok && c.ID == guestID && l.Guest && l.enabled()
}

func (s *usersServer) state() map[string]any {
	l := loadUsers(s.data)
	now := s.now().UnixMilli()
	users := []publicUser{}
	for _, u := range l.Users {
		p := publicUser{ID: u.ID, Name: u.Name, Owner: u.Owner, First: u.Dir == "", Password: u.Pass != nil}
		if u.Until > now {
			p.Wait = u.Until - now
		}
		p.Active = s.inUse(userDir(s.data, u))
		users = append(users, p)
	}
	var me any
	if s.guestSignedIn(l) {
		me = publicUser{ID: guestID, Guest: true}
	} else if u := s.me(l); u != nil {
		me = publicUser{ID: u.ID, Name: u.Name, Owner: u.Owner, First: u.Dir == "", Password: u.Pass != nil}
	}
	isOwner := false
	if u, ok := me.(publicUser); ok && u.Owner {
		isOwner = true
	}
	// Without users.json the one person there is the owner-to-be.
	if len(l.Users) == 0 {
		isOwner = true
	}
	return map[string]any{
		"enabled":  l.enabled(),
		"me":       me,
		"users":    users,
		"guest":    l.Guest,
		"max":      maxUsers,
		"lang":     systemLang(),
		"canAdd":   isOwner && len(l.Users) < maxUsers,
		"recovery": l.Recovery != nil,
		"kiosk":    os.Getenv("OROS_COMPUTER_FILES") == "1", // orOS USB: no "Close" on the sign-in window
	}
}

// tryPassword checks a password against a user, counting wrong tries.
// It saves the count. The answer is nil or an error answer.
func (s *usersServer) tryPassword(l *userList, u *orosUser, p string) (map[string]any, int) {
	now := s.now()
	if u.Until > now.UnixMilli() {
		return map[string]any{"error": "wait", "wait": u.Until - now.UnixMilli()}, 429
	}
	if u.Pass == nil {
		return nil, 0 // no password set (the owner before the second user)
	}
	if checkPassword(u.Pass, p) {
		if u.Fails != 0 || u.Until != 0 {
			u.Fails, u.Until = 0, 0
			_ = saveUsers(s.data, l)
		}
		return nil, 0
	}
	u.Fails++
	if d := waitFor(u.Fails); d > 0 {
		u.Until = now.Add(d).UnixMilli()
	}
	_ = saveUsers(s.data, l)
	out := map[string]any{"error": "wrong"}
	if u.Until > now.UnixMilli() {
		out["wait"] = u.Until - now.UnixMilli()
	}
	return out, 403
}

// ownerAuth: the caller is the owner and typed the owner's password.
func (s *usersServer) ownerAuth(l *userList, auth string) (*orosUser, map[string]any, int) {
	me := s.me(l)
	if me == nil || !me.Owner || s.guestSignedIn(l) {
		out, code := uFail("denied", 403)
		return nil, out, code
	}
	if out, code := s.tryPassword(l, me, auth); out != nil {
		return nil, out, code
	}
	return me, nil, 0
}

// someoneActive: a user (not the sign-in window) has a browser open.
func (s *usersServer) someoneActive(l *userList) bool {
	c, ok := readCurrent(s.data)
	if !ok {
		return false
	}
	if c.ID == guestID {
		return s.inUse(guestDir(s.data))
	}
	u := l.find(c.ID)
	return u != nil && s.inUse(userDir(s.data, u))
}

func (s *usersServer) login(q usersReq) (map[string]any, int) {
	l := loadUsers(s.data)
	if !l.enabled() {
		return uFail("denied", 403)
	}
	if s.someoneActive(l) {
		return uFail("busy", 409)
	}
	if q.ID == guestID {
		if !l.Guest {
			return uFail("denied", 403)
		}
		if err := wipeGuest(s.data); err != nil {
			log.Printf("users: guest folder: %v", err)
			return uFail("busy", 500)
		}
		if err := writeCurrent(s.data, guestID); err != nil {
			return uFail("busy", 500)
		}
		log.Printf("users: guest signed in")
		return uDone(nil)
	}
	u := l.find(q.ID)
	if u == nil {
		return uFail("bad", 400)
	}
	if u.Pass == nil {
		return uFail("denied", 403) // every user has a password once the window shows
	}
	if out, code := s.tryPassword(l, u, q.Password); out != nil {
		return out, code
	}
	if err := writeCurrent(s.data, u.ID); err != nil {
		return uFail("busy", 500)
	}
	log.Printf("users: %s signed in", u.ID)
	return uDone(nil)
}

func (s *usersServer) recover(q usersReq) (map[string]any, int) {
	l := loadUsers(s.data)
	o := l.owner()
	if !l.enabled() || o == nil || l.Recovery == nil || s.someoneActive(l) {
		return uFail("denied", 403)
	}
	if !validPassword(q.Password) {
		return uFail("password", 400)
	}
	now := s.now()
	if o.Until > now.UnixMilli() {
		return map[string]any{"error": "wait", "wait": o.Until - now.UnixMilli()}, 429
	}
	if !checkPassword(l.Recovery, normCode(q.Code)) {
		o.Fails++
		if d := waitFor(o.Fails); d > 0 {
			o.Until = now.Add(d).UnixMilli()
		}
		_ = saveUsers(s.data, l)
		return uFail("wrong", 403)
	}
	h, err := hashPassword(q.Password)
	if err != nil {
		return uFail("busy", 500)
	}
	code, rh, err := newRecovery()
	if err != nil {
		return uFail("busy", 500)
	}
	o.Pass, o.Fails, o.Until, l.Recovery = h, 0, 0, rh
	if err := saveUsers(s.data, l); err != nil {
		return uFail("busy", 500)
	}
	log.Printf("users: owner password set with the recovery code")
	return uDone(map[string]any{"code": code})
}

func newRecovery() (string, *passHash, error) {
	code, err := newRecoveryCode()
	if err != nil {
		return "", nil, err
	}
	h, err := hashPassword(normCode(code))
	return code, h, err
}

func (s *usersServer) signout() (map[string]any, int) {
	l := loadUsers(s.data)
	if !l.enabled() {
		return uFail("denied", 403)
	}
	if _, ok := readCurrent(s.data); !ok {
		return uFail("denied", 403)
	}
	if err := markSignOut(s.data); err != nil {
		return uFail("busy", 500)
	}
	log.Printf("users: sign-out asked")
	return uDone(nil)
}

func (s *usersServer) add(q usersReq) (map[string]any, int) {
	l := loadUsers(s.data)
	if s.guestSignedIn(l) {
		return uFail("denied", 403)
	}
	if len(l.Users) >= maxUsers {
		return uFail("full", 409)
	}
	name, err := cleanName(q.Name)
	if err != nil {
		return uFail("name", 400)
	}
	if !validPassword(q.Password) {
		return uFail("password", 400)
	}
	var recovery string
	if len(l.Users) == 0 {
		// The first time: the one person there becomes the owner and keeps
		// the data folder as it is (dir ""), so nothing moves.
		oname, err := cleanName(q.OwnerName)
		if err != nil {
			return uFail("name", 400)
		}
		if !validPassword(q.OwnerPassword) {
			return uFail("password", 400)
		}
		if strings.EqualFold(oname, name) || l.nameTaken(oname, "") {
			return uFail("taken", 409)
		}
		id, err := newUserID()
		if err != nil {
			return uFail("busy", 500)
		}
		h, err := hashPassword(q.OwnerPassword)
		if err != nil {
			return uFail("busy", 500)
		}
		l.Users = append(l.Users, &orosUser{ID: id, Name: oname, Owner: true, Dir: "", Pass: h, Created: s.now().UnixMilli()})
		code, rh, err := newRecovery()
		if err != nil {
			return uFail("busy", 500)
		}
		l.Recovery, recovery = rh, code
	} else {
		me, out, code := s.ownerAuth(l, q.Auth)
		if out != nil {
			return out, code
		}
		if me.Pass == nil {
			// An owner without a password (after the others were removed
			// and none was set): set it now, from ownerPassword.
			if !validPassword(q.OwnerPassword) {
				return uFail("password", 400)
			}
			h, err := hashPassword(q.OwnerPassword)
			if err != nil {
				return uFail("busy", 500)
			}
			me.Pass = h
		}
	}
	if l.nameTaken(name, "") {
		return uFail("taken", 409)
	}
	id, err := newUserID()
	if err != nil {
		return uFail("busy", 500)
	}
	h, err := hashPassword(q.Password)
	if err != nil {
		return uFail("busy", 500)
	}
	u := &orosUser{ID: id, Name: name, Dir: usersFolder + "/" + id, Pass: h, Created: s.now().UnixMilli()}
	if err := os.MkdirAll(userDir(s.data, u), 0o700); err != nil {
		return uFail("busy", 500)
	}
	l.Users = append(l.Users, u)
	if err := saveUsers(s.data, l); err != nil {
		return uFail("busy", 500)
	}
	// The one who added is signed in from now on, so the next start
	// knows whose window is open.
	if me := l.owner(); me != nil {
		if _, ok := readCurrent(s.data); !ok {
			_ = writeCurrent(s.data, me.ID)
		}
	}
	log.Printf("users: added %s", id)
	out := map[string]any{"id": id}
	if recovery != "" {
		out["code"] = recovery
	}
	return uDone(out)
}

func (s *usersServer) remove(q usersReq) (map[string]any, int) {
	l := loadUsers(s.data)
	me, out, code := s.ownerAuth(l, q.Auth)
	if out != nil {
		return out, code
	}
	u := l.find(q.ID)
	switch {
	case u == nil:
		return uFail("bad", 400)
	case u.ID == me.ID:
		return uFail("self", 409)
	case u.Dir == "":
		return uFail("first", 409)
	case s.inUse(userDir(s.data, u)):
		return uFail("busy", 409)
	}
	dir := userDir(s.data, u)
	if filepath.Dir(dir) != filepath.Join(s.data, usersFolder) || filepath.Base(dir) != u.ID {
		return uFail("bad", 400)
	}
	keep := l.Users[:0]
	for _, x := range l.Users {
		if x.ID != u.ID {
			keep = append(keep, x)
		}
	}
	l.Users = keep
	if err := saveUsers(s.data, l); err != nil {
		return uFail("busy", 500)
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Printf("users: removing %s: %v", u.ID, err)
	}
	if s.home != "" {
		keepHome(s.home, s.data, u)
	}
	log.Printf("users: removed %s", u.ID)
	return uDone(nil)
}

func (s *usersServer) password(q usersReq) (map[string]any, int) {
	l := loadUsers(s.data)
	if !validPassword(q.Password) {
		return uFail("password", 400)
	}
	me := s.me(l)
	if me == nil || s.guestSignedIn(l) {
		return uFail("denied", 403)
	}
	u := l.find(q.ID)
	if u == nil {
		return uFail("bad", 400)
	}
	if u.ID == me.ID {
		if out, code := s.tryPassword(l, me, q.Old); out != nil {
			return out, code
		}
	} else {
		if _, out, code := s.ownerAuth(l, q.Auth); out != nil {
			return out, code
		}
		u.Fails, u.Until = 0, 0
	}
	h, err := hashPassword(q.Password)
	if err != nil {
		return uFail("busy", 500)
	}
	u.Pass = h
	extra := map[string]any{}
	if u.Owner && l.Recovery == nil {
		code, rh, err := newRecovery()
		if err == nil {
			l.Recovery = rh
			extra["code"] = code
		}
	}
	if err := saveUsers(s.data, l); err != nil {
		return uFail("busy", 500)
	}
	log.Printf("users: password of %s changed", u.ID)
	return uDone(extra)
}

func (s *usersServer) rename(q usersReq) (map[string]any, int) {
	l := loadUsers(s.data)
	me := s.me(l)
	if me == nil || s.guestSignedIn(l) {
		return uFail("denied", 403)
	}
	u := l.find(q.ID)
	if u == nil {
		return uFail("bad", 400)
	}
	if u.ID != me.ID && !me.Owner {
		return uFail("denied", 403)
	}
	name, err := cleanName(q.Name)
	if err != nil {
		return uFail("name", 400)
	}
	if l.nameTaken(name, u.ID) {
		return uFail("taken", 409)
	}
	u.Name = name
	if err := saveUsers(s.data, l); err != nil {
		return uFail("busy", 500)
	}
	return uDone(nil)
}

func (s *usersServer) setOwner(q usersReq) (map[string]any, int) {
	l := loadUsers(s.data)
	me, out, code := s.ownerAuth(l, q.Auth)
	if out != nil {
		return out, code
	}
	u := l.find(q.ID)
	if u == nil || u.ID == me.ID {
		return uFail("bad", 400)
	}
	me.Owner, u.Owner = false, true
	// The recovery code was the old owner's: the new owner gets one when
	// they next change their password.
	l.Recovery = nil
	if err := saveUsers(s.data, l); err != nil {
		return uFail("busy", 500)
	}
	log.Printf("users: owner is now %s", u.ID)
	return uDone(nil)
}

func (s *usersServer) setGuest(q usersReq) (map[string]any, int) {
	l := loadUsers(s.data)
	if _, out, code := s.ownerAuth(l, q.Auth); out != nil {
		return out, code
	}
	l.Guest = q.On
	if err := saveUsers(s.data, l); err != nil {
		return uFail("busy", 500)
	}
	if !q.On {
		_ = wipeGuest(s.data)
	}
	return uDone(nil)
}

// sizes: how much each user's folder takes (the first user's without the
// other users' folders and the shared files).
func (s *usersServer) sizes() map[string]int64 {
	l := loadUsers(s.data)
	out := map[string]int64{}
	for _, u := range l.Users {
		if u.Dir == "" {
			var n int64
			for _, p := range []string{"profile", "profile-system", webFolder} {
				n += dirSize(filepath.Join(s.data, p))
			}
			out[u.ID] = n
			continue
		}
		out[u.ID] = dirSize(userDir(s.data, u))
	}
	return out
}

func dirSize(dir string) int64 {
	var n int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}
