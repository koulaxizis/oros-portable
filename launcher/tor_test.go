package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

func TestTorLinks(t *testing.T) {
	good := `{"version":"15.0.1","downloads":{` +
		`"linux-x86_64":{"ALL":{"binary":"https://dist.torproject.org/torbrowser/15.0.1/tor-browser-linux-x86_64-15.0.1.tar.xz","sig":"https://dist.torproject.org/torbrowser/15.0.1/tor-browser-linux-x86_64-15.0.1.tar.xz.asc"}},` +
		`"windows-x86_64":{"ALL":{"binary":"https://dist.torproject.org/torbrowser/15.0.1/tor-browser-windows-x86_64-portable-15.0.1.exe","sig":"https://dist.torproject.org/torbrowser/15.0.1/tor-browser-windows-x86_64-portable-15.0.1.exe.asc"}}}}`
	for keys, suffix := range map[string]string{"linux-x86_64 linux64": ".tar.xz", "windows-x86_64 win64": ".exe"} {
		v, bin, sig, err := torLinks([]byte(good), strings.Fields(keys))
		if err != nil || v != "15.0.1" || !strings.HasSuffix(bin, suffix) || sig != bin+".asc" {
			t.Fatalf("%s: %q %q %q %v", keys, v, bin, sig, err)
		}
	}
	old := strings.Replace(good, "windows-x86_64", "win64", 1)
	if _, bin, _, err := torLinks([]byte(old), []string{"windows-x86_64", "win64"}); err != nil || !strings.HasSuffix(bin, ".exe") {
		t.Fatalf("win64: %v", err)
	}
	for _, bad := range []string{
		strings.Replace(good, "https://dist.torproject.org", "https://torproject.org.evil.example", 1),
		strings.Replace(good, "https://dist.torproject.org", "http://dist.torproject.org", 1),
		`{"version":"1","downloads":{"macos":{"ALL":{"binary":"https://dist.torproject.org/a","sig":"https://dist.torproject.org/a.asc"}}}}`,
		`not json`,
	} {
		if _, _, _, err := torLinks([]byte(bad), []string{"linux-x86_64", "linux64"}); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// testKey makes a signing key and makes it the pinned one.
func testKey(t *testing.T, pin bool) *openpgp.Entity {
	t.Helper()
	e, err := openpgp.NewEntity("Tor Browser Developers", "test", "test@example.org", &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA})
	must(t, err)
	if pin {
		old := torFingerprint
		torFingerprint = fmt.Sprintf("%X", e.PrimaryKey.Fingerprint)
		t.Cleanup(func() { torFingerprint = old })
	}
	return e
}

func pubKey(t *testing.T, e *openpgp.Entity) []byte {
	var b bytes.Buffer
	must(t, e.Serialize(&b))
	return b.Bytes()
}

func sign(t *testing.T, e *openpgp.Entity, data []byte) []byte {
	var b bytes.Buffer
	must(t, openpgp.ArmoredDetachSign(&b, e, bytes.NewReader(data), nil))
	return b.Bytes()
}

// torServer serves a fake torproject.org (every host) with files.
type fakeTor struct {
	mu    sync.Mutex
	files map[string][]byte
	hits  map[string]int
}

func (f *fakeTor) set(path string, b []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = b
}

func torServer(t *testing.T) *fakeTor {
	t.Helper()
	f := &fakeTor{files: map[string][]byte{}, hits: map[string]int{}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		b, ok := f.files[r.URL.Path]
		f.hits[r.Host+r.URL.Path]++
		f.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	old := torHTTP
	torHTTP = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
	t.Cleanup(func() { torHTTP = old })
	return f
}

func writeFiles(t *testing.T, dir string, files map[string][]byte) {
	for n, b := range files {
		must(t, os.WriteFile(filepath.Join(dir, n), b, 0o644))
	}
}

func TestTorVerify(t *testing.T) {
	fake := torServer(t) // no key on the network unless a case puts one there
	key := testKey(t, true)
	other := testKey(t, false)
	dir := t.TempDir()
	data := []byte("tor browser")
	writeFiles(t, dir, map[string][]byte{
		"key.gpg":   pubKey(t, key),
		"file":      data,
		"file.asc":  sign(t, key, data),
		"bad":       []byte("tor browser, changed"),
		"bad.asc":   sign(t, key, data),
		"other":     data,
		"other.asc": sign(t, other, data),
		"wrong.gpg": pubKey(t, other),
	})
	p := func(n string) string { return filepath.Join(dir, n) }
	if err := torVerify(p("key.gpg"), p("file"), p("file.asc")); err != nil {
		t.Fatalf("good signature: %v", err)
	}
	if err := torVerify(p("key.gpg"), p("bad"), p("bad.asc")); err == nil {
		t.Fatal("a changed file passed")
	}
	if err := torVerify(p("key.gpg"), p("other"), p("other.asc")); err == nil {
		t.Fatal("a file signed by another key passed")
	}
	// A key with another fingerprint is never used, also when it signed.
	if err := torVerify(p("wrong.gpg"), p("other"), p("other.asc")); err == nil {
		t.Fatal("a key with another fingerprint was used")
	}
	// No bundled key: it comes from torproject.org, and only the pinned one.
	if err := torVerify(p("missing.gpg"), p("file"), p("file.asc")); err == nil {
		t.Fatal("passed without any key")
	}
	fake.set("/vks/v1/by-fingerprint/"+torFingerprint, pubKey(t, other))
	if err := torVerify(p("missing.gpg"), p("other"), p("other.asc")); err == nil {
		t.Fatal("a fetched key with another fingerprint was used")
	}
	fake.set("/vks/v1/by-fingerprint/"+torFingerprint, pubKey(t, key))
	if err := torVerify(p("missing.gpg"), p("file"), p("file.asc")); err != nil {
		t.Fatalf("fetched key: %v", err)
	}
}

// An older stick's key cannot check a release signed by a newer subkey;
// the same key fetched again can.
func TestTorVerifyRefreshesKey(t *testing.T) {
	fake := torServer(t)
	key := testKey(t, true)
	dir := t.TempDir()
	stale := pubKey(t, key) // before the new subkey
	must(t, key.AddSigningSubkey(&packet.Config{Algorithm: packet.PubKeyAlgoEdDSA}))
	sub := key.Subkeys[len(key.Subkeys)-1]
	data := []byte("tor browser 2")
	var sig bytes.Buffer
	must(t, openpgp.ArmoredDetachSign(&sig, key, bytes.NewReader(data), &packet.Config{SigningKeyId: sub.PublicKey.KeyId}))
	writeFiles(t, dir, map[string][]byte{"key.gpg": stale, "file": data, "file.asc": sig.Bytes()})
	p := func(n string) string { return filepath.Join(dir, n) }
	if err := torVerify(p("key.gpg"), p("file"), p("file.asc")); err == nil {
		t.Fatal("the stale key checked a signature by a subkey it does not know")
	}
	fake.set("/.well-known/openpgpkey/torproject.org/hu/kounek7zrdx745qydx6p59t9mqjpuhdf", pubKey(t, key))
	if err := torVerify(p("key.gpg"), p("file"), p("file.asc")); err != nil {
		t.Fatalf("refreshed key: %v", err)
	}
}

func TestTorCaps(t *testing.T) {
	withFake(t)
	lay := portableLayout(t)
	t.Setenv("OROS_PORTABLE_BROWSER", "")
	if handleSys(lay, sysRequest{Cmd: "status"})["caps"].(map[string]bool)["tor"] {
		t.Fatal("the lite edition offers Tor")
	}
	t.Setenv("OROS_PORTABLE_BROWSER", "/stick/browser/chrome")
	if !handleSys(lay, sysRequest{Cmd: "status"})["caps"].(map[string]bool)["tor"] {
		t.Fatal("Tor is missing from caps")
	}
	st := handleSys(lay, sysRequest{Cmd: "tor.status"})
	if st["ok"] != true || st["installed"] != false || st["running"] != false || st["persistent"] != true {
		t.Fatalf("tor.status: %v", st)
	}
	// The guest has no Tor: the guest's folder is emptied after each session.
	guest := lay.forUser(guestDir(lay.data))
	if handleSys(guest, sysRequest{Cmd: "status"})["caps"].(map[string]bool)["tor"] {
		t.Fatal("the guest is offered Tor")
	}
	for _, c := range []string{"tor.open", "tor.status", "tor.remove"} {
		if r := handleSys(guest, sysRequest{Cmd: c}); r["ok"] != false {
			t.Fatalf("guest %s: %v", c, r)
		}
	}
}

func TestTorOpenAndRemove(t *testing.T) {
	withFake(t)
	lay := portableLayout(t)
	user := lay.forUser(filepath.Join(lay.data, usersFolder, "u2"))
	var got [][]string
	old := torSelf
	torSelf = func(args ...string) error { got = append(got, args); return nil }
	defer func() { torSelf = old }()

	// Not installed: a detached download into the user's own folder.
	r := handleSys(user, sysRequest{Cmd: "tor.open"})
	if r["ok"] != true || r["downloading"] != true || len(got) != 1 {
		t.Fatalf("tor.open: %v %v", r, got)
	}
	a := strings.Join(got[0], " ")
	if !strings.Contains(a, "--fetch") || !strings.Contains(a, "--dir "+filepath.Join(user.userFolder(), "tor")) {
		t.Fatalf("download: %s", a)
	}
	// Installed: it starts.
	prog := torProgram(filepath.Join(torDir(user), "tor-browser"))
	must(t, os.MkdirAll(filepath.Dir(prog), 0o755))
	must(t, os.WriteFile(prog, []byte("#!/bin/sh\n"), 0o755))
	if r := handleSys(user, sysRequest{Cmd: "tor.open"}); r["ok"] != true || r["started"] != true || !strings.Contains(strings.Join(got[1], " "), "--run") {
		t.Fatalf("start: %v %v", r, got)
	}
	if st := handleSys(user, sysRequest{Cmd: "tor.status"}); st["installed"] != true || st["open"] != false {
		t.Fatalf("status: %v", st)
	}
	// Not while it is open or downloading.
	for _, l := range []string{"open.lock", ".lock"} {
		unlock, ok := lockFile(filepath.Join(torDir(user), l))
		if !ok {
			t.Fatal("lock")
		}
		if r := handleSys(user, sysRequest{Cmd: "tor.remove"}); r["ok"] != false {
			t.Fatalf("removed while %s was held", l)
		}
		unlock()
	}
	if r := handleSys(user, sysRequest{Cmd: "tor.remove"}); r["ok"] != true {
		t.Fatal(r)
	}
	if _, err := os.Stat(torDir(user)); !os.IsNotExist(err) {
		t.Fatal("the Tor folder is still there")
	}
	// The first user's folder is untouched.
	if _, err := os.Stat(filepath.Join(lay.root, "app", "index.html")); err != nil {
		t.Fatal(err)
	}
}

func TestTorFetchLock(t *testing.T) {
	dir := t.TempDir()
	unlock, ok := lockFile(filepath.Join(dir, ".lock"))
	if !ok {
		t.Fatal("lock")
	}
	defer unlock()
	start := time.Now()
	if err := torFetch(torOpts{dir: dir, index: "https://aus1.torproject.org/x"}, func(string, int) {}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("second download: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("waited for the lock")
	}
}
