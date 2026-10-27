//go:build !windows

package main

import (
	"os"
	"strings"
	"testing"
)

func TestOneTrayPerUser(t *testing.T) {
	d := t.TempDir()
	unlock, ok := lockTray(d)
	if !ok {
		t.Fatal("first lock")
	}
	// flock is per open file description: a second open in this process conflicts too.
	if _, ok := lockTray(d); ok {
		t.Fatal("second tray got the lock")
	}
	unlock()
	u2, ok := lockTray(d)
	if !ok {
		t.Fatal("lock after unlock")
	}
	u2()
}

func TestAutostartEntry(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if autostartOn() {
		t.Fatal("off by default")
	}
	must(t, setAutostart(true, `/home/u/My "Apps"/$orOS.AppImage`))
	if !autostartOn() {
		t.Fatal("not on")
	}
	p, _ := autostartFile()
	b, err := os.ReadFile(p)
	must(t, err)
	if !strings.Contains(string(b), `Exec="/home/u/My \\"Apps\\"/\\$orOS.AppImage" tray`) {
		t.Fatalf("Exec line:\n%s", b)
	}
	must(t, setAutostart(false, ""))
	if autostartOn() {
		t.Fatal("still on")
	}
	must(t, setAutostart(false, "")) // nothing to remove is fine
}
