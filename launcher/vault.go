package main

// Encrypted users on orOS USB (U4, optional, off by default).
//
// A user other than the first one may encrypt their orOS data and their
// home (This computer › Home) with their own password. Their files then
// live in an fscrypt folder on the data partition (base/<id>, base from
// "oros-system vault-base"), outside the /home overlay, and are reached
// through two links: data/users/<id> -> base/<id>/profile and
// homes/<id> -> base/<id>/home. The folder is open (the key is in the
// kernel) only while that user is signed in; at sign-out, and at every
// start of the computer, it is locked: names and contents unreadable, also
// for the owner, root and anyone who takes the stick to another computer.
//
// The key: 64 random bytes, kept only sealed (AES-256-GCM) with a key made
// from the user's password (PBKDF2-SHA-256, 600 000 rounds) and once more
// with a key made from the user's own recovery code (shown once when
// encryption is turned on). The owner cannot reset an encrypted user's
// password; the recovery code can. Without both the files are lost.
//
// Turning it on or off only marks the wish (state "pending" or
// "off-pending"); the files move at the user's next sign-in, when no
// browser has them open: copied, every file compared (size and SHA-256),
// then the links swapped and the old copy removed. An interrupted move is
// undone or finished at the next sign-in; the old copy goes only after the
// new one is checked and in place.
//
// The first user (who keeps the data folder) and the guest cannot turn it
// on; the passphrase at start (ENCRYPTION.md, LUKS) protects the whole
// stick. Honest limit: files that were on the stick before encryption may
// stay readable in unused flash blocks until they are overwritten.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	vaultPending    = "pending"     // turn on at the next sign-in
	vaultOn         = "on"          // encrypted
	vaultOffPending = "off-pending" // turn off at the next sign-in
	vaultFailed     = "failed"      // turning on did not finish; the files stayed as they were
	vaultProfile    = "profile"
	vaultHome       = "home"
)

type vaultInfo struct {
	State string `json:"state"`
	Key   string `json:"key,omitempty"` // fscrypt key identifier (hex), once on
	Iter  int    `json:"iter"`
	Salt  string `json:"salt"`  // base64, for the password's key
	Wrap  string `json:"wrap"`  // base64 nonce+sealed key, with the password
	RSalt string `json:"rsalt"` // the same with the recovery code
	RWrap string `json:"rwrap"`
	Error string `json:"error,omitempty"` // why turning on failed: space, copy
}

// vaultFS is the system side: real fscrypt on Linux (vault_linux.go),
// a fake in tests.
type vaultFS interface {
	Base() (string, error)                        // folder on the data partition that holds the vaults
	Free(path string) (uint64, error)             // bytes free there
	Add(dir string, key []byte) (fsKeyID, error)  // open
	SetPolicy(dir string, id fsKeyID) error       // encrypt an empty folder
	Remove(base string, id fsKeyID) (bool, error) // lock; true = files still open
}

var errVaultKey = errors.New("vault: wrong password or code")

// ---------- the key, sealed ----------

func vaultKEK(secret string, salt []byte, iter int) []byte {
	return pbkdf2SHA256([]byte(secret), salt, iter, 32)
}

func vaultSeal(key []byte, secret string, iter int) (salt, wrap string, err error) {
	s := make([]byte, 16)
	if _, err := rand.Read(s); err != nil {
		return "", "", err
	}
	gcm, err := vaultGCM(vaultKEK(secret, s, iter))
	if err != nil {
		return "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", err
	}
	out := gcm.Seal(nonce, nonce, key, []byte("oros-vault-v1"))
	return base64.StdEncoding.EncodeToString(s), base64.StdEncoding.EncodeToString(out), nil
}

func vaultUnseal(salt, wrap, secret string, iter int) ([]byte, error) {
	s, err1 := base64.StdEncoding.DecodeString(salt)
	w, err2 := base64.StdEncoding.DecodeString(wrap)
	if err1 != nil || err2 != nil || iter < 1000 || iter > 5000000 {
		return nil, errVaultKey
	}
	gcm, err := vaultGCM(vaultKEK(secret, s, iter))
	if err != nil || len(w) < gcm.NonceSize() {
		return nil, errVaultKey
	}
	key, err := gcm.Open(nil, w[:gcm.NonceSize()], w[gcm.NonceSize():], []byte("oros-vault-v1"))
	if err != nil || len(key) != fsKeyLen {
		return nil, errVaultKey
	}
	return key, nil
}

func vaultGCM(kek []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// newVault makes a key and seals it with the password and a new recovery
// code, which is returned to be shown once.
func newVault(password string) (*vaultInfo, string, error) {
	key := make([]byte, fsKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, "", err
	}
	code, err := newRecoveryCode()
	if err != nil {
		return nil, "", err
	}
	v := &vaultInfo{State: vaultPending, Iter: passIter}
	if v.Salt, v.Wrap, err = vaultSeal(key, password, v.Iter); err != nil {
		return nil, "", err
	}
	if v.RSalt, v.RWrap, err = vaultSeal(key, normCode(code), v.Iter); err != nil {
		return nil, "", err
	}
	return v, code, nil
}

func (v *vaultInfo) key(password string) ([]byte, error) {
	return vaultUnseal(v.Salt, v.Wrap, password, v.Iter)
}

func (v *vaultInfo) keyFromCode(code string) ([]byte, error) {
	return vaultUnseal(v.RSalt, v.RWrap, normCode(code), v.Iter)
}

// reseal: a new password (and, from the recovery code, a new code).
func (v *vaultInfo) reseal(key []byte, password string, newCode bool) (string, error) {
	salt, wrap, err := vaultSeal(key, password, v.Iter)
	if err != nil {
		return "", err
	}
	code := ""
	var rsalt, rwrap string
	if newCode {
		if code, err = newRecoveryCode(); err != nil {
			return "", err
		}
		if rsalt, rwrap, err = vaultSeal(key, normCode(code), v.Iter); err != nil {
			return "", err
		}
		v.RSalt, v.RWrap = rsalt, rwrap
	}
	v.Salt, v.Wrap = salt, wrap
	return code, nil
}

// encrypted: the user's files are (or are being made) encrypted, so only
// their password or recovery code opens them.
func (u *orosUser) encrypted() bool {
	return u.Vault != nil && (u.Vault.State == vaultOn || u.Vault.State == vaultOffPending)
}

// ---------- moving the files ----------

type vaultPaths struct {
	base, dir, prof, home string // the vault side
	link, hlink           string // data/users/<id>, homes/<id>
}

func (s *usersServer) vaultPaths(u *orosUser) (vaultPaths, error) {
	base, err := s.vfs.Base()
	if err != nil {
		return vaultPaths{}, err
	}
	d := filepath.Join(base, u.ID)
	return vaultPaths{base: base, dir: d, prof: filepath.Join(d, vaultProfile), home: filepath.Join(d, vaultHome),
		link: userDir(s.data, u), hlink: filepath.Join(homesDir(s.data), u.ID)}, nil
}

func isLink(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// pointLink makes link point at target (a new place after the stick came
// up under another device name).
func pointLink(link, target string) error {
	if t, err := os.Readlink(link); err == nil && t == target {
		return nil
	}
	if exists(link) && !isLink(link) {
		return fmt.Errorf("vault: %s is a folder", link)
	}
	_ = os.Remove(link)
	return os.Symlink(target, link)
}

// vaultSignIn runs after the password was right: it finishes or undoes a
// move and opens the user's folder. failed: turning on did not work and
// the user goes on unencrypted (saved in users.json).
func (s *usersServer) vaultSignIn(l *userList, u *orosUser, password string) error {
	if u.Vault == nil || u.Vault.State == vaultFailed || s.vfs == nil {
		if u.Vault != nil && u.Vault.State != vaultFailed && s.vfs == nil {
			return errors.New("vault: not on this system")
		}
		return nil
	}
	key, err := u.Vault.key(password)
	if err != nil {
		return err
	}
	defer clear(key)
	switch u.Vault.State {
	case vaultPending:
		if err := s.vaultTurnOn(u, key); err != nil {
			log.Printf("users: encrypting %s: %v", u.ID, err)
			code := "copy"
			if errors.Is(err, errVaultSpace) {
				code = "space"
			}
			u.Vault = &vaultInfo{State: vaultFailed, Error: code}
		}
		return saveUsers(s.data, l)
	case vaultOn:
		return s.vaultOpen(u, key)
	case vaultOffPending:
		if err := s.vaultOpen(u, key); err != nil {
			return err
		}
		if err := s.vaultTurnOff(u); err != nil {
			// Still encrypted and open: the user signs in as before.
			log.Printf("users: decrypting %s: %v", u.ID, err)
			return nil
		}
		u.Vault = nil
		return saveUsers(s.data, l)
	}
	return nil
}

var errVaultSpace = errors.New("vault: not enough free space")

func (s *usersServer) vaultTurnOn(u *orosUser, key []byte) error {
	p, err := s.vaultPaths(u)
	if err != nil {
		return err
	}
	// An earlier try that stopped after swapping the links: put the
	// folders back, then start again.
	for _, x := range [][2]string{{p.link, p.link + ".plain"}, {p.hlink, p.hlink + ".plain"}} {
		if exists(x[1]) {
			if isLink(x[0]) {
				_ = os.Remove(x[0])
			}
			if !exists(x[0]) {
				if err := os.Rename(x[1], x[0]); err != nil {
					return err
				}
			}
		}
	}
	if isLink(p.link) {
		return errors.New("vault: profile is already a link")
	}
	if err := os.MkdirAll(p.link, 0o700); err != nil {
		return err
	}
	need := dirSize(p.link) + dirSize(p.hlink)
	if free, err := s.vfs.Free(p.base); err == nil && uint64(need)+64<<20 > free {
		return errVaultSpace
	}
	if err := os.RemoveAll(p.dir); err != nil {
		return err
	}
	if err := os.Mkdir(p.dir, 0o700); err != nil {
		return err
	}
	id, err := s.vfs.Add(p.dir, key)
	if err != nil {
		return err
	}
	fail := func(e error) error {
		_, _ = s.vfs.Remove(p.base, id)
		_ = os.RemoveAll(p.dir)
		return e
	}
	if err := s.vfs.SetPolicy(p.dir, id); err != nil {
		return fail(err)
	}
	if err := vaultCopy(p.link, p.prof); err != nil {
		return fail(err)
	}
	if exists(p.hlink) {
		if err := vaultCopy(p.hlink, p.home); err != nil {
			return fail(err)
		}
	} else if err := os.Mkdir(p.home, 0o700); err != nil {
		return fail(err)
	}
	if err := sameTree(p.link, p.prof); err != nil {
		return fail(err)
	}
	if exists(p.hlink) {
		if err := sameTree(p.hlink, p.home); err != nil {
			return fail(err)
		}
	}
	// Swap: the plain folders step aside, the links take their place.
	if err := os.Rename(p.link, p.link+".plain"); err != nil {
		return fail(err)
	}
	if err := os.Symlink(p.prof, p.link); err != nil {
		_ = os.Rename(p.link+".plain", p.link)
		return fail(err)
	}
	if exists(p.hlink) {
		if err := os.Rename(p.hlink, p.hlink+".plain"); err != nil {
			_ = os.Remove(p.link)
			_ = os.Rename(p.link+".plain", p.link)
			return fail(err)
		}
	}
	_ = os.MkdirAll(filepath.Dir(p.hlink), 0o755)
	if err := os.Symlink(p.home, p.hlink); err != nil {
		_ = os.Remove(p.link)
		_ = os.Rename(p.link+".plain", p.link)
		if exists(p.hlink + ".plain") {
			_ = os.Rename(p.hlink+".plain", p.hlink)
		}
		return fail(err)
	}
	u.Vault.State, u.Vault.Key = vaultOn, hex.EncodeToString(id[:])
	if err := saveUsers(s.data, loadUsersWith(s.data, u)); err != nil {
		return err
	}
	_ = os.RemoveAll(p.link + ".plain")
	_ = os.RemoveAll(p.hlink + ".plain")
	log.Printf("users: %s is encrypted", u.ID)
	return nil
}

// loadUsersWith: users.json with u as it is now (vaultTurnOn saves its
// state before removing the old copy, whatever the caller saves later).
func loadUsersWith(data string, u *orosUser) *userList {
	l := loadUsers(data)
	for i, x := range l.Users {
		if x.ID == u.ID {
			l.Users[i] = u
		}
	}
	return l
}

// vaultOpen adds the key, after pointing the links at the vault (the
// partition may have another name this time).
func (s *usersServer) vaultOpen(u *orosUser, key []byte) error {
	p, err := s.vaultPaths(u)
	if err != nil {
		return err
	}
	// Turned on, but the old copy was still there (stopped after saving).
	for _, x := range []string{p.link + ".plain", p.hlink + ".plain"} {
		if exists(x) {
			_ = os.RemoveAll(x)
		}
	}
	if !exists(p.dir) {
		return errors.New("vault: folder missing")
	}
	id, err := s.vfs.Add(p.dir, key)
	if err != nil {
		return err
	}
	if hex.EncodeToString(id[:]) != u.Vault.Key {
		_, _ = s.vfs.Remove(p.base, id)
		return errVaultKey
	}
	if err := pointLink(p.link, p.prof); err != nil {
		return err
	}
	_ = os.MkdirAll(filepath.Dir(p.hlink), 0o755)
	return pointLink(p.hlink, p.home)
}

// vaultTurnOff copies the open vault back to plain folders.
func (s *usersServer) vaultTurnOff(u *orosUser) error {
	p, err := s.vaultPaths(u)
	if err != nil {
		return err
	}
	need := dirSize(p.prof) + dirSize(p.home)
	if free, err := s.vfs.Free(p.base); err == nil && uint64(need)+64<<20 > free {
		return errVaultSpace
	}
	for _, x := range [][2]string{{p.prof, p.link}, {p.home, p.hlink}} {
		if !isLink(x[1]) {
			continue // done by an earlier try
		}
		tmp := x[1] + ".new"
		_ = os.RemoveAll(tmp)
		if err := vaultCopy(x[0], tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		if err := sameTree(x[0], tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		if err := os.Remove(x[1]); err != nil {
			return err
		}
		if err := os.Rename(tmp, x[1]); err != nil {
			_ = os.Symlink(x[0], x[1])
			return err
		}
	}
	if id, ok := vaultKeyID(u); ok {
		_, _ = s.vfs.Remove(p.base, id)
	}
	if err := os.RemoveAll(p.dir); err != nil {
		log.Printf("users: removing the vault of %s: %v", u.ID, err)
	}
	log.Printf("users: %s is no longer encrypted", u.ID)
	return nil
}

func vaultKeyID(u *orosUser) (fsKeyID, bool) {
	var id fsKeyID
	if u.Vault == nil {
		return id, false
	}
	b, err := hex.DecodeString(u.Vault.Key)
	if err != nil || len(b) != len(id) {
		return id, false
	}
	copy(id[:], b)
	return id, true
}

// vaultLock locks the user's folder (sign-out, the browser closed). Files
// still open (a browser process that has not ended yet) are retried for a
// while in the background.
func (s *usersServer) vaultLock(id string) {
	if s == nil || s.vfs == nil {
		return
	}
	u := loadUsers(s.data).find(id)
	if u == nil || !u.encrypted() {
		return
	}
	kid, ok := vaultKeyID(u)
	if !ok {
		return
	}
	base, err := s.vfs.Base()
	if err != nil {
		return
	}
	busy, err := s.vfs.Remove(base, kid)
	if err != nil {
		log.Printf("users: locking %s: %v", id, err)
		return
	}
	if !busy {
		log.Printf("users: %s locked", id)
		return
	}
	go func() {
		for i := 0; i < 60; i++ {
			time.Sleep(time.Second)
			if b, err := s.vfs.Remove(base, kid); err == nil && !b {
				log.Printf("users: %s locked", id)
				return
			}
		}
		log.Printf("users: %s: files still open, the folder locks when they close", id)
	}()
}

// vaultRemove deletes an encrypted user's vault (locked or not) and the
// links to it. The owner cannot read those files, so nothing is kept.
func (s *usersServer) vaultRemove(u *orosUser) {
	p, err := s.vaultPaths(u)
	if err != nil {
		return
	}
	if id, ok := vaultKeyID(u); ok {
		_, _ = s.vfs.Remove(p.base, id)
	}
	_ = os.Remove(p.link)
	_ = os.Remove(p.hlink)
	if err := os.RemoveAll(p.dir); err != nil {
		log.Printf("users: removing the vault of %s: %v", u.ID, err)
	}
}

// ---------- copy and compare ----------

// vaultCopy copies a folder: folders, regular files and links. Browser
// lock files (Singleton*) and sockets are left out.
func vaultCopy(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if strings.HasPrefix(d.Name(), "Singleton") {
			return nil
		}
		to := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return os.MkdirAll(to, info.Mode().Perm()|0o700)
		case d.Type()&fs.ModeSymlink != 0:
			t, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(t, to)
		case d.Type().IsRegular():
			return copyFile(p, to, info)
		}
		return nil
	})
}

func copyFile(src, dst string, info fs.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// sameTree checks that dst has every folder, link and file of src, files
// with the same size and SHA-256.
func sameTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), "Singleton") {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		to := filepath.Join(dst, rel)
		fi, err := os.Lstat(to)
		if err != nil {
			return fmt.Errorf("vault: %s missing", rel)
		}
		switch {
		case d.IsDir():
			if !fi.IsDir() {
				return fmt.Errorf("vault: %s differs", rel)
			}
		case d.Type()&fs.ModeSymlink != 0:
			a, _ := os.Readlink(p)
			b, _ := os.Readlink(to)
			if a != b {
				return fmt.Errorf("vault: %s differs", rel)
			}
		case d.Type().IsRegular():
			a, err1 := fileSum(p)
			b, err2 := fileSum(to)
			if err1 != nil || err2 != nil || a != b {
				return fmt.Errorf("vault: %s differs", rel)
			}
		}
		return nil
	})
}

func fileSum(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%x", n, h.Sum(nil)), nil
}
