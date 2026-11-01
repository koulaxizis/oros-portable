package main

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeVault keeps fscrypt's bookkeeping without encrypting: which folder
// has which key, which keys are in.
type fakeVault struct {
	base    string
	free    uint64
	present map[fsKeyID]bool
	policy  map[string]fsKeyID
	failSet bool
}

func newFakeVault(t *testing.T) *fakeVault {
	return &fakeVault{base: t.TempDir(), free: 1 << 40, present: map[fsKeyID]bool{}, policy: map[string]fsKeyID{}}
}

func (f *fakeVault) Base() (string, error)       { return f.base, nil }
func (f *fakeVault) Free(string) (uint64, error) { return f.free, nil }
func (f *fakeVault) Add(dir string, key []byte) (fsKeyID, error) {
	var id fsKeyID
	h := sha256.Sum256(key)
	copy(id[:], h[:])
	f.present[id] = true
	return id, nil
}
func (f *fakeVault) SetPolicy(dir string, id fsKeyID) error {
	if f.failSet {
		return os.ErrPermission
	}
	f.policy[dir] = id
	return nil
}
func (f *fakeVault) Remove(base string, id fsKeyID) (bool, error) {
	delete(f.present, id)
	return false, nil
}

// vaultSetup: owner Chris and Anna on orOS USB, Anna signed in with a
// profile file and a document.
func vaultSetup(t *testing.T) (*apiClient, *fakeVault, string, string) {
	home := t.TempDir()
	data := filepath.Join(home, "oros", "data")
	must(t, os.MkdirAll(data, 0o755))
	c := newAPI(t)
	fv := newFakeVault(t)
	c.s.data, c.s.home, c.s.vfs = data, home, fv
	_, out := c.call("add", map[string]any{"name": "Anna", "password": "anna-pass", "ownerName": "Chris", "ownerPassword": "owner-pass"})
	anna := out["id"].(string)
	writeCurrent(data, anna)
	h := userHome(home, data)
	prof := filepath.Join(data, "users", anna)
	must(t, os.MkdirAll(filepath.Join(prof, "profile", "Default"), 0o700))
	must(t, os.WriteFile(filepath.Join(prof, "profile", "Default", "Preferences"), []byte("{\"x\":1}"), 0o600))
	must(t, os.Symlink("x", filepath.Join(prof, "profile", "SingletonLock")))
	must(t, os.Symlink("Default", filepath.Join(prof, "profile", "Last")))
	must(t, os.WriteFile(filepath.Join(h, "Documents", "diary.txt"), []byte("dear diary"), 0o600))
	return c, fv, data, anna
}

func readOK(t *testing.T, p, want string) {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil || string(b) != want {
		t.Fatalf("%s: %q %v", p, b, err)
	}
}

func TestVaultTurnOnAtNextSignIn(t *testing.T) {
	c, fv, data, anna := vaultSetup(t)
	if st := c.state(); st["vault"] != true {
		t.Fatal("state.vault")
	}
	if code, out := c.call("vault", map[string]any{"on": true, "password": "wrong-pass"}); code != 403 || out["error"] != "wrong" {
		t.Fatalf("wrong password: %d %v", code, out)
	}
	code, out := c.call("vault", map[string]any{"on": true, "password": "anna-pass"})
	rcode, _ := out["code"].(string)
	if code != 200 || len(rcode) < 20 {
		t.Fatalf("turn on: %d %v", code, out)
	}
	if u := loadUsers(data).find(anna); u.Vault == nil || u.Vault.State != vaultPending {
		t.Fatal("pending")
	}
	// Nothing moves while Anna is signed in.
	prof := filepath.Join(data, "users", anna)
	if isLink(prof) {
		t.Fatal("moved too early")
	}
	endSession(data)
	// A wrong password at the sign-in moves nothing.
	if code, _ := c.call("login", map[string]any{"id": anna, "password": "nope-nope"}); code != 403 {
		t.Fatal("wrong login")
	}
	if code, out := c.call("login", map[string]any{"id": anna, "password": "anna-pass"}); code != 200 {
		t.Fatalf("login: %d %v", code, out)
	}
	u := loadUsers(data).find(anna)
	if u.Vault.State != vaultOn || u.Vault.Key == "" {
		t.Fatalf("state %+v", u.Vault)
	}
	vd := filepath.Join(fv.base, anna)
	if tgt, _ := os.Readlink(prof); tgt != filepath.Join(vd, "profile") {
		t.Fatalf("profile link %q", tgt)
	}
	hl := filepath.Join(filepath.Dir(data), "homes", anna)
	if tgt, _ := os.Readlink(hl); tgt != filepath.Join(vd, "home") {
		t.Fatalf("home link %q", tgt)
	}
	if _, ok := fv.policy[vd]; !ok {
		t.Fatal("policy not set on the vault folder")
	}
	readOK(t, filepath.Join(prof, "profile", "Default", "Preferences"), "{\"x\":1}")
	readOK(t, filepath.Join(hl, "Documents", "diary.txt"), "dear diary")
	if tgt, _ := os.Readlink(filepath.Join(prof, "profile", "Last")); tgt != "Default" {
		t.Fatal("links inside the profile kept")
	}
	if exists(filepath.Join(prof, "profile", "SingletonLock")) {
		t.Fatal("browser lock copied")
	}
	if exists(prof+".plain") || exists(hl+".plain") {
		t.Fatal("old plain copy kept")
	}
	if len(fv.present) != 1 {
		t.Fatal("key not in while signed in")
	}
	// The user's own home and sizes go through the link.
	if userHome(c.s.home, data) != hl {
		t.Fatal("home path")
	}
	_, sizes := c.call("sizes", nil)
	if sizes[anna].(float64) != 7 {
		t.Fatalf("sizes %v", sizes)
	}
	// Sign-out locks.
	orig := lockVault
	lockVault = func(d, id string) { c.s.vaultLock(id) }
	defer func() { lockVault = orig }()
	endSession(data)
	if len(fv.present) != 0 {
		t.Fatal("still open after sign-out")
	}
	// Next sign-in opens again, and fixes links to a renamed partition.
	old := fv.base
	fv.base = t.TempDir()
	must(t, os.Rename(filepath.Join(old, anna), filepath.Join(fv.base, anna)))
	if code, _ := c.call("login", map[string]any{"id": anna, "password": "anna-pass"}); code != 200 {
		t.Fatal("second login")
	}
	readOK(t, filepath.Join(prof, "profile", "Default", "Preferences"), "{\"x\":1}")
	readOK(t, filepath.Join(hl, "Documents", "diary.txt"), "dear diary")
}

func TestVaultPasswordsAndRecovery(t *testing.T) {
	c, _, data, anna := vaultSetup(t)
	_, out := c.call("vault", map[string]any{"on": true, "password": "anna-pass"})
	rcode := out["code"].(string)
	endSession(data)
	c.call("login", map[string]any{"id": anna, "password": "anna-pass"})
	// Her own change re-seals the key.
	if code, out := c.call("password", map[string]any{"id": anna, "old": "anna-pass", "password": "anna-new"}); code != 200 {
		t.Fatalf("own change: %d %v", code, out)
	}
	u := loadUsers(data).find(anna)
	if _, err := u.Vault.key("anna-new"); err != nil {
		t.Fatal("key not sealed with the new password")
	}
	if _, err := u.Vault.key("anna-pass"); err == nil {
		t.Fatal("old password still opens the key")
	}
	// The owner cannot reset it.
	owner := loadUsers(data).owner().ID
	writeCurrent(data, owner)
	if code, out := c.call("password", map[string]any{"id": anna, "auth": "owner-pass", "password": "taken-over"}); code != 409 || out["error"] != "vault" {
		t.Fatalf("owner reset: %d %v", code, out)
	}
	clearCurrent(data)
	// Her recovery code sets a new password and gives a new code.
	if code, _ := c.call("recover", map[string]any{"id": anna, "code": "AAAAA-AAAAA-AAAAA-AAAAA", "password": "x-new-pass"}); code != 403 {
		t.Fatal("wrong code")
	}
	code, out := c.call("recover", map[string]any{"id": anna, "code": strings.ToLower(rcode), "password": "after-code"})
	if code != 200 || out["code"] == "" || out["code"] == rcode {
		t.Fatalf("recover: %d %v", code, out)
	}
	u = loadUsers(data).find(anna)
	if _, err := u.Vault.key("after-code"); err != nil {
		t.Fatal("key not sealed after recovery")
	}
	if _, err := u.Vault.keyFromCode(rcode); err == nil {
		t.Fatal("old code still opens the key")
	}
	if _, err := u.Vault.keyFromCode(out["code"].(string)); err != nil {
		t.Fatal("new code does not open the key")
	}
	if code, _ := c.call("login", map[string]any{"id": anna, "password": "after-code"}); code != 200 {
		t.Fatal("login after recovery")
	}
}

func TestVaultTurnOffAndRemove(t *testing.T) {
	c, fv, data, anna := vaultSetup(t)
	c.call("vault", map[string]any{"on": true, "password": "anna-pass"})
	endSession(data)
	c.call("login", map[string]any{"id": anna, "password": "anna-pass"})
	if code, _ := c.call("vault", map[string]any{"on": false, "password": "anna-pass"}); code != 200 {
		t.Fatal("turn off")
	}
	if loadUsers(data).find(anna).Vault.State != vaultOffPending {
		t.Fatal("off-pending")
	}
	endSession(data)
	if code, out := c.call("login", map[string]any{"id": anna, "password": "anna-pass"}); code != 200 {
		t.Fatalf("login: %d %v", code, out)
	}
	prof := filepath.Join(data, "users", anna)
	hl := filepath.Join(filepath.Dir(data), "homes", anna)
	if isLink(prof) || isLink(hl) || loadUsers(data).find(anna).Vault != nil {
		t.Fatal("still encrypted")
	}
	readOK(t, filepath.Join(prof, "profile", "Default", "Preferences"), "{\"x\":1}")
	readOK(t, filepath.Join(hl, "Documents", "diary.txt"), "dear diary")
	if exists(filepath.Join(fv.base, anna)) {
		t.Fatal("vault folder kept")
	}
	// On again, then removed by the owner: the vault goes, nothing lands
	// in the owner's home (the owner could not read it).
	c.call("vault", map[string]any{"on": true, "password": "anna-pass"})
	endSession(data)
	c.call("login", map[string]any{"id": anna, "password": "anna-pass"})
	endSession(data)
	writeCurrent(data, loadUsers(data).owner().ID)
	if code, out := c.call("remove", map[string]any{"id": anna, "auth": "owner-pass"}); code != 200 {
		t.Fatalf("remove: %d %v", code, out)
	}
	if exists(filepath.Join(fv.base, anna)) || exists(prof) || exists(hl) {
		t.Fatal("vault or links kept")
	}
	if m, _ := filepath.Glob(filepath.Join(c.s.home, "*removed user*")); len(m) != 0 {
		t.Fatalf("kept in the owner's home: %v", m)
	}
}

func TestVaultRefusalsAndFailures(t *testing.T) {
	c, fv, data, anna := vaultSetup(t)
	// The first user and the guest cannot.
	writeCurrent(data, loadUsers(data).owner().ID)
	if code, out := c.call("vault", map[string]any{"on": true, "password": "owner-pass"}); code != 409 || out["error"] != "first" {
		t.Fatalf("first user: %d %v", code, out)
	}
	// Without the system side: novault.
	writeCurrent(data, anna)
	c.s.vfs = nil
	if code, out := c.call("vault", map[string]any{"on": true, "password": "anna-pass"}); code != 409 || out["error"] != "novault" {
		t.Fatalf("no vault: %d %v", code, out)
	}
	c.s.vfs = fv
	// Turning on fails (policy refused): the files stay as they were and
	// the state says so.
	c.call("vault", map[string]any{"on": true, "password": "anna-pass"})
	endSession(data)
	fv.failSet = true
	if code, _ := c.call("login", map[string]any{"id": anna, "password": "anna-pass"}); code != 200 {
		t.Fatal("login after a failed turn-on")
	}
	prof := filepath.Join(data, "users", anna)
	if isLink(prof) {
		t.Fatal("moved after failure")
	}
	readOK(t, filepath.Join(prof, "profile", "Default", "Preferences"), "{\"x\":1}")
	if v := loadUsers(data).find(anna).Vault; v == nil || v.State != vaultFailed || v.Error != "copy" {
		t.Fatalf("failed state %+v", v)
	}
	if exists(filepath.Join(fv.base, anna)) {
		t.Fatal("half vault kept")
	}
	// Not enough space: the same, with "space".
	fv.failSet = false
	c.call("vault", map[string]any{"on": true, "password": "anna-pass"})
	endSession(data)
	fv.free = 1
	c.call("login", map[string]any{"id": anna, "password": "anna-pass"})
	if v := loadUsers(data).find(anna).Vault; v == nil || v.Error != "space" {
		t.Fatalf("space %+v", v)
	}
}

func TestVaultResumesAnInterruptedMove(t *testing.T) {
	c, fv, data, anna := vaultSetup(t)
	c.call("vault", map[string]any{"on": true, "password": "anna-pass"})
	endSession(data)
	// As if the computer went off right after swapping the profile link,
	// before users.json said "on".
	prof := filepath.Join(data, "users", anna)
	must(t, os.MkdirAll(filepath.Join(fv.base, anna, "profile"), 0o700))
	must(t, os.Rename(prof, prof+".plain"))
	must(t, os.Symlink(filepath.Join(fv.base, anna, "profile"), prof))
	if code, out := c.call("login", map[string]any{"id": anna, "password": "anna-pass"}); code != 200 {
		t.Fatalf("login: %d %v", code, out)
	}
	if loadUsers(data).find(anna).Vault.State != vaultOn {
		t.Fatal("not on")
	}
	readOK(t, filepath.Join(prof, "profile", "Default", "Preferences"), "{\"x\":1}")
	if exists(prof + ".plain") {
		t.Fatal("plain kept")
	}
}

func TestVaultSealing(t *testing.T) {
	key := make([]byte, fsKeyLen)
	key[0] = 7
	salt, wrap, err := vaultSeal(key, "secret", 1000)
	if err != nil {
		t.Fatal(err)
	}
	got, err := vaultUnseal(salt, wrap, "secret", 1000)
	if err != nil || got[0] != 7 {
		t.Fatal("unseal")
	}
	if _, err := vaultUnseal(salt, wrap, "Secret", 1000); err == nil {
		t.Fatal("wrong secret opened it")
	}
	if _, err := vaultUnseal(salt, wrap, "secret", 10); err == nil {
		t.Fatal("silly round count accepted")
	}
	// The first user never has a vault, whatever users.json says.
	d := t.TempDir()
	must(t, os.WriteFile(filepath.Join(d, usersFile), []byte(`{"v":1,"users":[{"id":"u00000001","name":"A","dir":"","vault":{"state":"on"}}]}`), 0o600))
	if loadUsers(d).Users[0].Vault != nil {
		t.Fatal("first user's vault kept")
	}
}
