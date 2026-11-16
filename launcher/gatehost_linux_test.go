package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fake browser that holds the profile like Chromium (SingletonLock ->
// host-pid) until it is ended: the tray starts it while nothing else
// uses the profile, and a window start ends it again.
func TestGateHostRunnerStartsAndYields(t *testing.T) {
	lay := gateHostLay(t)
	fake := os.Getenv("OROS_PORTABLE_BROWSER")
	script := "#!/bin/sh\nfor a; do case \"$a\" in --user-data-dir=*) p=\"${a#--user-data-dir=}\";; esac; done\n" +
		"mkdir -p \"$p\"; echo \"$@\" > \"$p/../args\"; ln -sf \"host-$$\" \"$p/SingletonLock\"\ntrap 'rm -f \"$p/SingletonLock\"; exit 0' TERM\nwhile :; do sleep 0.1; done\n"
	must(t, os.WriteFile(fake, []byte(script), 0o755))
	must(t, writeGateHostPref(lay.data, true))
	g := newGateHostRunner(lay)
	g.tick(func() bool { return false })
	if !g.running() {
		t.Fatal("started")
	}
	profile := filepath.Join(lay.data, "profile")
	for i := 0; i < 50 && !profileInUse(profile); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if !profileInUse(profile) {
		t.Fatal("the fake holds the profile")
	}
	rec, ok := readGateHostRun(lay.data)
	if !ok {
		t.Fatal("run file")
	}
	b, _ := os.ReadFile(filepath.Join(lay.data, "args"))
	if !strings.Contains(string(b), "--headless=new") || !strings.Contains(string(b), "gate-host="+rec.Nonce) {
		t.Fatalf("args: %s", b)
	}
	if !gateHostIsBrowser(rec.Pid) || gateHostIsBrowser(os.Getpid()) {
		t.Fatal("only the background browser is recognised")
	}
	// A second tick does not start a second one.
	g.tick(func() bool { return false })
	// A window starts: the background one says bye (here: never, so the
	// wait ends when it no longer holds the profile) and is ended.
	must(t, os.WriteFile(filepath.Join(lay.data, gateHostBye), []byte("1"), 0o600))
	time.Sleep(1100 * time.Millisecond) // bye must be newer than the hold minus a second
	go func() {
		time.Sleep(200 * time.Millisecond)
		must(t, os.WriteFile(filepath.Join(lay.data, gateHostBye), []byte("1"), 0o600))
	}()
	gateHostYield(lay.data)
	for i := 0; i < 50 && g.running(); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if g.running() || profileInUse(profile) {
		t.Fatal("ended for the window")
	}
	// Held: not started again under the window.
	g.tick(func() bool { return false })
	if g.running() {
		t.Fatal("held for a minute")
	}
}
