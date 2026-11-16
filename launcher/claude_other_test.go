//go:build linux

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

func mustC(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestDebCompare(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0", 0}, {"1.0", "1.1", -1}, {"1.10", "1.9", 1}, {"1.0~beta1", "1.0", -1},
		{"1.0", "1.0-1", -1}, {"1:0.9", "2.0", 1}, {"0.14.10", "0.14.9", 1}, {"1.0a", "1.0+", -1},
		{"1.2.3-2", "1.2.3-10", -1}, {"01.0", "1.0", 0},
	} {
		if got := debCompare(c.a, c.b); got != c.want {
			t.Errorf("debCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := debCompare(c.b, c.a); got != -c.want {
			t.Errorf("debCompare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
}

var claudeTestKey []byte

// claudeKey makes a signing key and writes its public half to a keyring
// file; the pinned fingerprint is set to it for the test.
func claudeKey(t *testing.T) (*openpgp.Entity, string) {
	t.Helper()
	e, err := openpgp.NewEntity("Test repo", "", "repo@example.invalid", nil)
	mustC(t, err)
	var b bytes.Buffer
	w, err := armor.Encode(&b, openpgp.PublicKeyType, nil)
	mustC(t, err)
	mustC(t, e.Serialize(w))
	mustC(t, w.Close())
	f := filepath.Join(t.TempDir(), "key.asc")
	mustC(t, os.WriteFile(f, b.Bytes(), 0o644))
	claudeTestKey = b.Bytes()
	old := claudeFingerprint
	claudeFingerprint = fmt.Sprintf("%X", e.PrimaryKey.Fingerprint)
	t.Cleanup(func() { claudeFingerprint = old })
	return e, f
}

func clearSign(t *testing.T, e *openpgp.Entity, text string) []byte {
	t.Helper()
	var b bytes.Buffer
	w, err := clearsign.Encode(&b, e.PrivateKey, nil)
	mustC(t, err)
	_, err = w.Write([]byte(text))
	mustC(t, err)
	mustC(t, w.Close())
	return b.Bytes()
}

// fakeClaudeDeb builds a .deb shaped like an Electron app: the program in
// /usr/lib/claude-desktop next to resources/app.asar, linked from
// /usr/bin/claude-desktop. The program is a copy of /bin/true.
func fakeClaudeDeb(t *testing.T, version string) []byte {
	t.Helper()
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb missing")
	}
	src := t.TempDir()
	lib := filepath.Join(src, "usr/lib/claude-desktop")
	mustC(t, os.MkdirAll(filepath.Join(lib, "resources"), 0o755))
	mustC(t, os.MkdirAll(filepath.Join(src, "usr/bin"), 0o755))
	mustC(t, os.MkdirAll(filepath.Join(src, "DEBIAN"), 0o755))
	tr, err := os.ReadFile("/bin/true")
	mustC(t, err)
	mustC(t, os.WriteFile(filepath.Join(lib, "claude-desktop"), tr, 0o755))
	mustC(t, os.WriteFile(filepath.Join(lib, "resources/app.asar"), []byte("asar"), 0o644))
	mustC(t, os.Symlink("/usr/lib/claude-desktop/claude-desktop", filepath.Join(src, "usr/bin/claude-desktop")))
	mustC(t, os.WriteFile(filepath.Join(src, "DEBIAN/control"), []byte("Package: claude-desktop\nVersion: "+version+"\nArchitecture: amd64\nMaintainer: test <t@example.invalid>\nDescription: test\n"), 0o644))
	out := filepath.Join(t.TempDir(), "c.deb")
	if b, err := exec.Command("dpkg-deb", "--root-owner-group", "--build", src, out).CombinedOutput(); err != nil {
		t.Fatalf("dpkg-deb: %v %s", err, b)
	}
	b, err := os.ReadFile(out)
	mustC(t, err)
	return b
}

type claudeRepoFake struct {
	inRelease, packagesGz, deb, key []byte
	served                          map[string]int
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func newClaudeRepo(t *testing.T, e *openpgp.Entity, deb []byte, version string) *claudeRepoFake {
	t.Helper()
	pkgs := fmt.Sprintf("Package: claude-desktop\nVersion: %s\nArchitecture: amd64\nFilename: pool/main/c/claude-desktop/claude-desktop_%s_amd64.deb\nSize: %d\nSHA256: %s\n", version, version, len(deb), sha(deb))
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(pkgs))
	mustC(t, zw.Close())
	rel := fmt.Sprintf("Origin: test\nSuite: stable\nArchitectures: amd64 arm64\nComponents: main\nSHA256:\n %s %d main/binary-amd64/Packages.gz\n", sha(gz.Bytes()), gz.Len())
	return &claudeRepoFake{inRelease: clearSign(t, e, rel), packagesGz: gz.Bytes(), deb: deb, key: claudeTestKey, served: map[string]int{}}
}

func (r *claudeRepoFake) serve(t *testing.T) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.served[q.URL.Path]++
		switch {
		case q.URL.Path == "/claude-desktop/apt/stable/dists/stable/InRelease":
			w.Write(r.inRelease)
		case q.URL.Path == "/claude-desktop/apt/stable/dists/stable/main/binary-amd64/Packages.gz":
			w.Write(r.packagesGz)
		case q.URL.Path == "/claude-desktop/key.asc":
			w.Write(r.key)
		case strings.HasPrefix(q.URL.Path, "/claude-desktop/apt/stable/pool/"):
			w.Write(r.deb)
		default:
			http.NotFound(w, q)
		}
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	old := claudeHTTP
	claudeHTTP = &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}}
	t.Cleanup(func() { claudeHTTP = old })
}

func TestClaudeLauncherFetch(t *testing.T) {
	e, _ := claudeKey(t)
	repo := newClaudeRepo(t, e, fakeClaudeDeb(t, "1.0.0"), "1.0.0")
	repo.serve(t)
	home := filepath.Join(t.TempDir(), "oros-claude")
	old := claudeHome
	claudeHome = func() string { return home }
	t.Cleanup(func() { claudeHome = old })
	oldArch := claudeArch
	claudeArch = "amd64"
	t.Cleanup(func() { claudeArch = oldArch })

	// the key comes from downloads.claude.ai, taken only with the pinned fingerprint
	mustC(t, claudeFetch(func(string, int) {}))
	if v := claudeVersion(); v != "1.0.0" {
		t.Fatalf("version %q", v)
	}
	*repo = *newClaudeRepo(t, e, fakeClaudeDeb(t, "1.2.0"), "1.2.0")
	mustC(t, claudeFetch(func(string, int) {}))
	if v := claudeVersion(); v != "1.2.0" {
		t.Fatalf("update: %q", v)
	}
	mustC(t, claudeUninstall())
	if v := claudeVersion(); v != "" {
		t.Fatalf("after remove: %q", v)
	}

	// another key's signature, or a tampered package: nothing is installed
	other, err := openpgp.NewEntity("Someone else", "", "x@example.invalid", nil)
	mustC(t, err)
	*repo = *newClaudeRepo(t, other, fakeClaudeDeb(t, "2.0.0"), "2.0.0")
	if err := claudeFetch(func(string, int) {}); err == nil || claudeVersion() != "" {
		t.Fatalf("other key: %v %q", err, claudeVersion())
	}
	*repo = *newClaudeRepo(t, e, fakeClaudeDeb(t, "2.0.0"), "2.0.0")
	repo.deb = append(append([]byte{}, repo.deb[:len(repo.deb)-1]...), repo.deb[len(repo.deb)-1]^1)
	if err := claudeFetch(func(string, int) {}); err == nil || !strings.Contains(err.Error(), "does not match") || claudeVersion() != "" {
		t.Fatalf("tampered: %v", err)
	}
}

func TestClaudeCaps(t *testing.T) {
	withFake(t)
	lay := portableLayout(t)
	t.Setenv("OROS_NO_CLAUDE", "")
	want := claudePlatformOK()
	if got := handleSys(lay, sysRequest{Cmd: "status"})["caps"].(map[string]bool)["claude"]; got != want {
		t.Fatalf("caps claude %v, platform %v", got, want)
	}
	guest := lay.forUser(guestDir(lay.data))
	if handleSys(guest, sysRequest{Cmd: "status"})["caps"].(map[string]bool)["claude"] {
		t.Fatal("the guest is offered Claude desktop")
	}
	for _, c := range []string{"claude.status", "claude.install", "claude.open", "claude.remove"} {
		if r := handleSys(guest, sysRequest{Cmd: c}); r["ok"] != false {
			t.Fatalf("guest %s: %v", c, r)
		}
	}
	t.Setenv("OROS_NO_CLAUDE", "1")
	if handleSys(lay, sysRequest{Cmd: "status"})["caps"].(map[string]bool)["claude"] {
		t.Fatal("OROS_NO_CLAUDE ignored")
	}
}

func TestWmctrlParsing(t *testing.T) {
	w, ok := wmWork("0  * DG: 1920x1080  VP: 0,0  WA: 0,27 1920x1053  Desk 1\n1  - DG: 1920x1080  VP: N/A  WA: 0,0 1920x1080  Desk 2\n")
	if !ok || w != (rect{0, 27, 1920, 1053}) {
		t.Fatalf("wmWork = %v %v", w, ok)
	}
	if _, ok := wmWork("0  - DG: 1x1  VP: 0,0  WA: 0,0 1x1  D\n"); ok {
		t.Fatal("no current desktop must give no work area")
	}
	exe := map[int]string{100: "/opt/claude/claude", 200: "/stick/orOS/browser/chrome"}
	ws := wmWindows("0x03a00003  0 100  10 10 1200 800 host Claude\n0x0280000a  0 200  0 0 1900 1000 host orOS\nbad line\n",
		func(pid int) string { return exe[pid] })
	if len(ws) != 2 || ws[0].id != 0x03a00003 || ws[0].exe != "/opt/claude/claude" || ws[1].w != 1900 {
		t.Fatalf("wmWindows = %+v", ws)
	}
	if o, c := pickWins(ws, "/stick/orOS"); o != 0x0280000a || c != 0x03a00003 {
		t.Fatalf("pick = %x %x", o, c)
	}
}
