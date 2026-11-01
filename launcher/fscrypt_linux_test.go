package main

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
)

// OROS_FSCRYPT_DIR: a folder on an ext4 file system with the encrypt
// feature (CI makes one on a loop device); without it the test skips.
func TestFscryptRoundTrip(t *testing.T) {
	base := os.Getenv("OROS_FSCRYPT_DIR")
	if base == "" {
		t.Skip("OROS_FSCRYPT_DIR not set")
	}
	dir, err := os.MkdirTemp(base, "v")
	if err != nil {
		t.Fatal(err)
	}
	key := make([]byte, fsKeyLen)
	_, _ = rand.Read(key)
	id, err := fsAddKey(dir, key)
	if err != nil {
		t.Fatalf("add key: %v", err)
	}
	if err := fsSetPolicy(dir, id); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	if got, ok := fsPolicyKey(dir); !ok || got != id {
		t.Fatalf("policy key %x %v, want %x", got, ok, id)
	}
	if !fsKeyPresent(base, id) {
		t.Fatal("key not present after add")
	}
	secret := []byte("only for me")
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), secret, 0o600); err != nil {
		t.Fatal(err)
	}
	if busy, err := fsRemoveKey(base, id); err != nil || busy {
		t.Fatalf("remove: busy %v err %v", busy, err)
	}
	if fsKeyPresent(base, id) {
		t.Fatal("key still present")
	}
	names, _ := os.ReadDir(dir)
	if len(names) != 1 || names[0].Name() == "note.txt" {
		t.Fatalf("locked folder shows %v", names)
	}
	if _, err := os.ReadFile(filepath.Join(dir, names[0].Name())); err == nil {
		t.Fatal("locked file was readable")
	}
	if id2, err := fsAddKey(dir, key); err != nil || id2 != id {
		t.Fatalf("re-add: %x %v", id2, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "note.txt"))
	if err != nil || !bytes.Equal(b, secret) {
		t.Fatalf("after unlock: %q %v", b, err)
	}
	_, _ = fsRemoveKey(base, id)
}
