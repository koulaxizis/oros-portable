package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

type fakeBio struct {
	k, c   string
	answer error
	asked  []string
	broken bool
}

func (f *fakeBio) kind() string { return f.k }
func (f *fakeBio) can() string  { return f.c }
func (f *fakeBio) verify(text string) error {
	f.asked = append(f.asked, text)
	return f.answer
}
func (f *fakeBio) seal(d []byte) ([]byte, error) {
	out := append([]byte("S"), d...)
	return out, nil
}
func (f *fakeBio) unseal(b []byte) ([]byte, error) {
	if f.broken || len(b) == 0 || b[0] != 'S' {
		return nil, bioError("failed")
	}
	return b[1:], nil
}

func withBio(t *testing.T, f *fakeBio) layout {
	t.Helper()
	old := bioPlatform
	bioPlatform = f
	t.Cleanup(func() { bioPlatform = old })
	dir := t.TempDir()
	return layout{data: dir, user: filepath.Join(dir, "users", "u1")}
}

const kidA = "0123456789abcdef01234567"

func TestBioSealOpenClear(t *testing.T) {
	f := &fakeBio{k: "hello", c: "ok"}
	lay := withBio(t, f)
	st := bioHost(lay, sysRequest{Cmd: "bio.state"})
	if st["ok"] != true || st["can"] != "ok" || st["kind"] != "hello" || len(st["kids"].([]string)) != 0 {
		t.Fatalf("state %v", st)
	}
	r := bioHost(lay, sysRequest{Cmd: "bio.seal", Kid: kidA, Text: "Unlock orOS\n<b>"})
	if r["ok"] != true {
		t.Fatalf("seal %v", r)
	}
	d, _ := base64.StdEncoding.DecodeString(r["d"].(string))
	if len(d) != 32 {
		t.Fatalf("d %d bytes", len(d))
	}
	if f.asked[0] != "Unlock orOS<b>" {
		t.Errorf("prompt text %q", f.asked[0])
	}
	// kept in the user's folder, only for this user, never the plain D
	b, err := os.ReadFile(filepath.Join(lay.user, bioFileName))
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(filepath.Join(lay.user, bioFileName)); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	if string(b) == "" || containsBytes(b, []byte(r["d"].(string))) {
		t.Errorf("stored D in the clear: %s", b)
	}
	o := bioHost(lay, sysRequest{Cmd: "bio.open", Kid: kidA})
	if o["ok"] != true || o["d"] != r["d"] {
		t.Fatalf("open %v", o)
	}
	if st := bioHost(lay, sysRequest{Cmd: "bio.state"}); len(st["kids"].([]string)) != 1 {
		t.Errorf("kids %v", st)
	}
	if c := bioHost(lay, sysRequest{Cmd: "bio.clear", Kid: kidA}); c["ok"] != true {
		t.Fatal(c)
	}
	if o := bioHost(lay, sysRequest{Cmd: "bio.open", Kid: kidA}); o["error"] != "none" {
		t.Errorf("after clear %v", o)
	}
	if _, err := os.Stat(filepath.Join(lay.user, bioFileName)); !os.IsNotExist(err) {
		t.Errorf("empty store left a file")
	}
}

func containsBytes(h, n []byte) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if string(h[i:i+len(n)]) == string(n) {
			return true
		}
	}
	return false
}

func TestBioRefusals(t *testing.T) {
	f := &fakeBio{k: "finger", c: "ok"}
	lay := withBio(t, f)
	for _, kid := range []string{"", "x", "0123456789ABCDEF01234567", "../../etc/passwd"} {
		if r := bioHost(lay, sysRequest{Cmd: "bio.seal", Kid: kid}); r["error"] != "bad" {
			t.Errorf("kid %q: %v", kid, r)
		}
	}
	// a failed or cancelled check gives no secret and keeps nothing
	f.answer = bioError("cancel")
	if r := bioHost(lay, sysRequest{Cmd: "bio.seal", Kid: kidA}); r["error"] != "cancel" || r["d"] != nil {
		t.Errorf("cancel %v", r)
	}
	if st := bioHost(lay, sysRequest{Cmd: "bio.state"}); len(st["kids"].([]string)) != 0 {
		t.Errorf("kept after cancel %v", st)
	}
	f.answer = nil
	bioHost(lay, sysRequest{Cmd: "bio.seal", Kid: kidA})
	f.answer = bioError("lockout")
	if r := bioHost(lay, sysRequest{Cmd: "bio.open", Kid: kidA}); r["error"] != "lockout" || r["d"] != nil {
		t.Errorf("lockout %v", r)
	}
	// a copy that cannot be opened (another Windows user) is removed
	f.answer = nil
	f.broken = true
	if r := bioHost(lay, sysRequest{Cmd: "bio.open", Kid: kidA}); r["error"] != "none" {
		t.Errorf("broken %v", r)
	}
	f.broken = false
	if r := bioHost(lay, sysRequest{Cmd: "bio.open", Kid: kidA}); r["error"] != "none" {
		t.Errorf("broken copy kept %v", r)
	}
	// at most 16 records
	for i := 0; i < bioMaxKids; i++ {
		kid := kidA[:22] + string("0123456789abcdef"[i]) + "0"
		if r := bioHost(lay, sysRequest{Cmd: "bio.seal", Kid: kid}); r["ok"] != true {
			t.Fatalf("seal %d %v", i, r)
		}
	}
	if r := bioHost(lay, sysRequest{Cmd: "bio.seal", Kid: "ffffffffffffffffffffffff"}); r["error"] != "full" {
		t.Errorf("17th %v", r)
	}
	// no way at all on this system
	f.k = ""
	if st := bioHost(lay, sysRequest{Cmd: "bio.state"}); st["can"] != "no-hw" {
		t.Errorf("no kind %v", st)
	}
	if r := bioHost(lay, sysRequest{Cmd: "bio.open", Kid: kidA[:22] + "00"}); r["error"] != "no-hw" {
		t.Errorf("no kind open %v", r)
	}
}

func TestBioStoreReadDefensively(t *testing.T) {
	lay := withBio(t, &fakeBio{k: "hello", c: "ok"})
	os.MkdirAll(lay.user, 0o700)
	os.WriteFile(filepath.Join(lay.user, bioFileName), []byte(`{"v":1,"kids":{"bad":"x","`+kidA+`":"U0FB"}}`), 0o600)
	if got := bioKids(bioLoad(lay)); len(got) != 1 || got[0] != kidA {
		t.Errorf("kids %v", got)
	}
	os.WriteFile(filepath.Join(lay.user, bioFileName), []byte(`not json`), 0o600)
	if got := bioKids(bioLoad(lay)); len(got) != 0 {
		t.Errorf("junk %v", got)
	}
}

func TestBioTextAndParentWindow(t *testing.T) {
	if bioText("  ") != "orOS" || bioText("a\x00b\tc") != "abc" {
		t.Error("text")
	}
	if n := len([]rune(bioText(string(make([]rune, 200))))); n > 80 {
		t.Errorf("long %d", n)
	}
	if parentWindowArg([]string{"orOS", "chrome-extension://x/", "--parent-window=1234"}) != 1234 {
		t.Error("hwnd")
	}
	if parentWindowArg([]string{"orOS", "--parent-window=12a"}) != 0 || parentWindowArg([]string{"orOS"}) != 0 {
		t.Error("bad hwnd")
	}
}

func TestBioInStatusAndCommands(t *testing.T) {
	f := &fakeBio{k: "hello", c: "ok"}
	lay := withBio(t, f)
	caps := sysStatus(lay, &fakeOps{})["caps"].(map[string]bool)
	if !caps["bio"] {
		t.Error("caps.bio")
	}
	if r := handleSys(lay, sysRequest{Cmd: "bio.state"}); r["ok"] != true {
		t.Errorf("handleSys %v", r)
	}
}
