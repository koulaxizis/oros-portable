package main

import (
	"os"
	"path/filepath"
	"testing"
)

// testVault: the real fscrypt calls, with the vault folder in
// OROS_FSCRYPT_DIR (an ext4 with the encrypt feature) instead of the
// helper's answer.
type testVault struct {
	sysVault
	dir string
}

func (v *testVault) Base() (string, error) { return v.dir, nil }

// The whole round with real encryption: turned on at the sign-in, locked
// at the sign-out (names and contents unreadable), open again at the next.
func TestVaultRealFscrypt(t *testing.T) {
	root := os.Getenv("OROS_FSCRYPT_DIR")
	if root == "" {
		t.Skip("OROS_FSCRYPT_DIR not set")
	}
	c, _, data, anna := vaultSetup(t)
	base, err := os.MkdirTemp(root, "vault")
	if err != nil {
		t.Fatal(err)
	}
	must(t, os.Chmod(base, 0o700))
	c.s.vfs = &testVault{dir: base}
	c.call("vault", map[string]any{"on": true, "password": "anna-pass"})
	endSession(data)
	if code, out := c.call("login", map[string]any{"id": anna, "password": "anna-pass"}); code != 200 {
		t.Fatalf("login: %d %v", code, out)
	}
	vd := filepath.Join(base, anna)
	if _, ok := fsPolicyKey(vd); !ok {
		t.Fatal("vault folder not encrypted")
	}
	hl := filepath.Join(filepath.Dir(data), "homes", anna)
	readOK(t, filepath.Join(hl, "Documents", "diary.txt"), "dear diary")
	orig := lockVault
	lockVault = func(d, id string) { c.s.vaultLock(id) }
	defer func() { lockVault = orig }()
	endSession(data)
	names, _ := os.ReadDir(vd)
	for _, n := range names {
		if n.Name() == "home" || n.Name() == "profile" {
			t.Fatalf("locked vault shows %q", n.Name())
		}
	}
	if _, err := os.ReadFile(filepath.Join(hl, "Documents", "diary.txt")); err == nil {
		t.Fatal("locked file readable")
	}
	if code, _ := c.call("login", map[string]any{"id": anna, "password": "anna-pass"}); code != 200 {
		t.Fatal("second login")
	}
	readOK(t, filepath.Join(hl, "Documents", "diary.txt"), "dear diary")
	readOK(t, filepath.Join(data, "users", anna, "profile", "Default", "Preferences"), "{\"x\":1}")
	endSession(data)
}
