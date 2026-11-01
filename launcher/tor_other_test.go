//go:build !windows

package main

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

type tarEntry struct {
	name, body, link string
	dir              bool
	mode             int64
}

func tarXZ(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var b bytes.Buffer
	x, err := xz.NewWriter(&b)
	must(t, err)
	tw := tar.NewWriter(x)
	for _, e := range entries {
		h := &tar.Header{Name: e.name, Mode: e.mode}
		switch {
		case e.dir:
			h.Typeflag, h.Mode = tar.TypeDir, 0o755
		case e.link != "":
			h.Typeflag, h.Linkname, h.Mode = tar.TypeSymlink, e.link, 0o777
		default:
			h.Typeflag, h.Size = tar.TypeReg, int64(len(e.body))
			if h.Mode == 0 {
				h.Mode = 0o644
			}
		}
		must(t, tw.WriteHeader(h))
		if h.Typeflag == tar.TypeReg {
			_, err := tw.Write([]byte(e.body))
			must(t, err)
		}
	}
	must(t, tw.Close())
	must(t, x.Close())
	return b.Bytes()
}

func fakeTorBrowser(t *testing.T, marker string) []byte {
	return tarXZ(t, []tarEntry{
		{name: "tor-browser/", dir: true},
		{name: "tor-browser/Browser/", dir: true},
		{name: "tor-browser/Browser/start-tor-browser", body: "#!/bin/sh\necho started > " + marker + "\n", mode: 0o755},
		{name: "tor-browser/Browser/TorBrowser/Docs/README", body: "readme"},
		{name: "tor-browser/start-tor-browser.desktop", link: "Browser/TorBrowser/Docs/README"},
	})
}

func TestTorFetchInstallsCheckedBrowser(t *testing.T) {
	fake := torServer(t)
	key := testKey(t, true)
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, torKeyFile), pubKey(t, key), 0o644))
	marker := filepath.Join(t.TempDir(), "started")
	tb := fakeTorBrowser(t, marker)
	fake.set("/downloads.json", []byte(`{"version":"99.0","downloads":{"linux-x86_64":{"ALL":{"binary":"https://dist.torproject.org/tb.tar.xz","sig":"https://dist.torproject.org/tb.tar.xz.asc"}}}}`))
	fake.set("/tb.tar.xz", tb)
	fake.set("/tb.tar.xz.asc", sign(t, key, tb))

	lay := newLayout(root)
	dir := torDir(lay)
	o := torOpts{dir: dir, index: "https://aus1.torproject.org/downloads.json", keyring: filepath.Join(root, torKeyFile), progress: torProgressFile(lay)}
	var steps []string
	if err := torFetch(o, func(s string, _ int) { steps = append(steps, s) }); err != nil {
		t.Fatal(err)
	}
	if torInstalled(dir) == "" {
		t.Fatal("not installed")
	}
	if !strings.Contains(strings.Join(steps, " "), "verify") {
		t.Fatalf("steps: %v", steps)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "tor-browser", "start-tor-browser.desktop")); err != nil || string(b) != "readme" {
		t.Fatalf("link: %q %v", b, err)
	}
	for _, n := range []string{"download.tar.xz", "download.tar.xz.asc", "new", "tor-browser.old"} {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			t.Errorf("%s left behind", n)
		}
	}
	if err := runTor(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(marker); string(b) != "started\n" {
		t.Fatal("Tor Browser did not start")
	}

	// A changed download is refused, deleted, and the installed one stays.
	fake.set("/tb.tar.xz", fakeTorBrowser(t, filepath.Join(t.TempDir(), "evil")))
	if err := torFetch(o, func(string, int) {}); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("changed download: %v", err)
	}
	if torInstalled(dir) == "" {
		t.Fatal("the installed Tor Browser was lost")
	}
	if _, err := os.Stat(filepath.Join(dir, "download.tar.xz")); err == nil {
		t.Fatal("the refused download was kept")
	}
	// Through the command too: the progress file says why.
	if code := torMain(lay, []string{"--fetch", "--no-start", "--index", o.index}); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if st := torStatus(lay); !strings.Contains(st["progress"].(torState).Error, "signature") {
		t.Fatalf("progress: %v", st)
	}
}

func TestUntarStaysInside(t *testing.T) {
	for name, entries := range map[string][]tarEntry{
		"dotdot":      {{name: "../evil", body: "x"}},
		"deep dotdot": {{name: "a/../../evil", body: "x"}},
		"link out":    {{name: "a", link: "../../etc/passwd"}},
		"abs link":    {{name: "a", link: "/etc/passwd"}},
	} {
		dir := filepath.Join(t.TempDir(), "x")
		x, _ := xz.NewReader(bytes.NewReader(tarXZ(t, entries)))
		if err := untar(x, dir); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := os.Lstat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
			t.Errorf("%s: wrote outside", name)
		}
	}
}
