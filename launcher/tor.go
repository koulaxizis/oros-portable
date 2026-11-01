package main

// The Tor button (System extension, next to "Internet"): the official Tor
// Browser in a window of its own, as on orOS USB (oros-usb system/tor.go).
//
// orOS Portable and the desktop apps do not carry Tor Browser. The first
// press downloads the latest release from torproject.org into the user's
// own folder (<user folder>/tor), checks its OpenPGP signature against Tor
// Browser's signing key, and installs it there: on Linux the .tar.xz is
// unpacked (tor_other.go), on Windows the official installer runs silently
// into that folder and whatever it put on the computer outside it
// (shortcuts, the Add or remove programs entry) is taken away again
// (tor_windows.go). From then on the button starts it at once, and Tor
// Browser keeps itself up to date like on any computer.
//
// The signature check is done here, in Go (ProtonMail go-crypto), because
// Windows has no gpgv and many Linux systems lack it. The build puts the
// key next to the launcher (tor-browser-developers.gpg, from oros-usb's
// scripts/tor-key.sh); its fingerprint is pinned below as well. When that
// key cannot check a new release (an older stick, the Tor Project signed
// with a new subkey) the key is fetched again from torproject.org and
// keys.openpgp.org and taken only with the same fingerprint. A download
// that fails the check is deleted and never run.
//
// Each user has their own Tor Browser (bookmarks and settings stay
// theirs); the guest has none, because the guest's folder is emptied
// after every session.

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	pgperrors "github.com/ProtonMail/go-crypto/openpgp/errors"
)

const (
	torIndex       = "https://aus1.torproject.org/torbrowser/update_3/release/downloads.json"
	torKeyFile     = "tor-browser-developers.gpg"
	torFolder      = "tor"
	torProgress    = "tor.json"
	torMaxDownload = 600 << 20
	torMaxSig      = 64 << 10
	torMaxKey      = 1 << 20
	// torMinFree is what the download, the unpacked browser and room for
	// its own updates need on the stick or disk.
	torMinFree = 1 << 30
)

// torFingerprint is Tor Browser Developers' signing key, as torproject.org
// publishes it (oros-usb scripts/tor-key.sh pins the same); tests replace it.
var torFingerprint = "EF6E286DDA85EA2A4BA7DE684E2C6E8793298290"

// torKeyURLs give Tor Browser's signing key when the bundled one is
// missing or cannot check a release.
func torKeyURLs() []string {
	return []string{
		"https://openpgpkey.torproject.org/.well-known/openpgpkey/torproject.org/hu/kounek7zrdx745qydx6p59t9mqjpuhdf",
		"https://keys.openpgp.org/vks/v1/by-fingerprint/" + torFingerprint,
	}
}

var torHTTP = &http.Client{Timeout: 30 * time.Minute}

func torDir(lay layout) string          { return filepath.Join(lay.userFolder(), torFolder) }
func torProgressFile(lay layout) string { return filepath.Join(lay.userFolder(), torProgress) }

// torAvailable: the button is offered where the extension runs (the
// bundled browser), on 64-bit Windows and Linux, to every user but the
// guest.
func torAvailable(lay layout) bool {
	if runtime.GOARCH != "amd64" || (runtime.GOOS != "windows" && runtime.GOOS != "linux") {
		return false
	}
	if os.Getenv("OROS_NO_TOR") != "" {
		return false
	}
	return lay.userFolder() != guestDir(lay.data)
}

// torInstalled is the program that starts Tor Browser, or "".
func torInstalled(dir string) string {
	p := torProgram(filepath.Join(dir, "tor-browser"))
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

type torState struct {
	Step    string `json:"step"`
	Percent int    `json:"percent"`
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
}

func torWriter(file string) func(torState) {
	last := torState{Percent: -1}
	return func(p torState) {
		if p.Step == last.Step && p.Percent == last.Percent && !p.Done {
			return
		}
		last = p
		b, _ := json.Marshal(p)
		_ = os.MkdirAll(filepath.Dir(file), 0o700)
		tmp := file + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			_ = os.Rename(tmp, file)
		}
	}
}

// torBusy: a download holds <dir>/.lock; Tor Browser started by orOS
// holds <dir>/open.lock while its window is open.
func torBusy(dir string) bool { return held(filepath.Join(dir, ".lock")) }
func torOpen(dir string) bool { return held(filepath.Join(dir, "open.lock")) }

func held(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	unlock, ok := lockFile(path)
	if !ok {
		return true
	}
	unlock()
	return false
}

// torStatus is "tor.status": the same fields as on orOS USB (persistent
// is always true here: the stick or disk keeps the folder), plus open.
func torStatus(lay layout) sysReply {
	if !torAvailable(lay) {
		return sysFail(errUnsupported)
	}
	dir := torDir(lay)
	r := sysReply{"running": torBusy(dir), "installed": torInstalled(dir) != "", "open": torOpen(dir), "persistent": true}
	if b, err := os.ReadFile(torProgressFile(lay)); err == nil {
		var p torState
		if json.Unmarshal(b, &p) == nil {
			r["progress"] = p
		}
	}
	return ok(r)
}

// torSelf runs the launcher detached; tests replace it.
var torSelf = func(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return detach(exe, args...)
}

// openTor is "tor.open": start Tor Browser, or the download that installs
// it first. Both run in a launcher of their own ("orOS tor"), because
// Chromium ends the host as soon as it has answered.
func openTor(lay layout) sysReply {
	if !torAvailable(lay) {
		return sysFail(errUnsupported)
	}
	dir := torDir(lay)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return sysFail(err)
	}
	if torBusy(dir) {
		return ok(sysReply{"downloading": true, "persistent": true})
	}
	args := []string{"tor", "--dir", dir, "--progress", torProgressFile(lay)}
	if torInstalled(dir) != "" {
		if torOpen(dir) {
			return ok(sysReply{"started": true, "open": true})
		}
		if err := torSelf(append(args, "--run")...); err != nil {
			return sysFail(err)
		}
		return ok(sysReply{"started": true})
	}
	if free, _, _ := diskSpace(lay.userFolder()); free > 0 && free < torMinFree {
		return sysFail(fmt.Errorf("Tor Browser needs about 1 GB free space here; %d MB are free", free>>20))
	}
	_ = os.Remove(torProgressFile(lay))
	torWriter(torProgressFile(lay))(torState{Step: "index"})
	if err := torSelf(append(args, "--fetch")...); err != nil {
		return sysFail(err)
	}
	// Let the download take its lock before the next status call.
	for i := 0; i < 20 && !torBusy(dir); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	return ok(sysReply{"downloading": true, "persistent": true})
}

// removeTor is "tor.remove": the user's Tor Browser goes, with its
// bookmarks and settings (the extension asks first).
func removeTor(lay layout) sysReply {
	if !torAvailable(lay) {
		return sysFail(errUnsupported)
	}
	dir := torDir(lay)
	if torBusy(dir) {
		return sysFail(errors.New("Tor Browser is being downloaded"))
	}
	if torOpen(dir) {
		return sysFail(errors.New("close Tor Browser first"))
	}
	if filepath.Base(dir) != torFolder {
		return sysFail(errors.New("unexpected Tor folder"))
	}
	if err := os.RemoveAll(dir); err != nil {
		return sysFail(err)
	}
	_ = os.Remove(torProgressFile(lay))
	return ok(nil)
}

// torMain is "orOS tor --fetch|--run --dir D --progress F".
func torMain(lay layout, args []string) int {
	fl := flag.NewFlagSet("tor", flag.ContinueOnError)
	o := torOpts{index: torIndex, keyring: filepath.Join(lay.root, torKeyFile)}
	var fetch, run, noStart bool
	fl.BoolVar(&fetch, "fetch", false, "download, check and install the latest Tor Browser")
	fl.BoolVar(&run, "run", false, "start Tor Browser and wait until it closes")
	fl.BoolVar(&noStart, "no-start", false, "do not start Tor Browser after --fetch")
	fl.StringVar(&o.dir, "dir", torDir(lay), "folder for Tor Browser")
	fl.StringVar(&o.progress, "progress", torProgressFile(lay), "progress file (JSON)")
	fl.StringVar(&o.index, "index", o.index, "Tor Browser's downloads.json")
	fl.StringVar(&o.keyring, "keyring", o.keyring, "Tor Browser's signing key")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	if fetch == run {
		fmt.Fprintln(os.Stderr, "usage: orOS tor --fetch [--no-start] | --run [--dir D]")
		return 2
	}
	if run {
		if err := runTor(o.dir); err != nil {
			log.Printf("tor: %v", err)
			return 1
		}
		return 0
	}
	report := torWriter(o.progress)
	err := torFetch(o, func(step string, pct int) { report(torState{Step: step, Percent: pct}) })
	if err != nil {
		report(torState{Step: "error", Done: true, Error: err.Error()})
		log.Printf("tor: %v", err)
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report(torState{Step: "done", Percent: 100, Done: true})
	if !noStart {
		if err := runTor(o.dir); err != nil {
			log.Printf("tor: %v", err)
			return 1
		}
	}
	return 0
}

// runTor starts Tor Browser and holds open.lock until its window closes,
// so the extension knows when to bring orOS back.
func runTor(dir string) error {
	p := torInstalled(dir)
	if p == "" {
		return errors.New("Tor Browser is not installed")
	}
	unlock, ok := lockFile(filepath.Join(dir, "open.lock"))
	if !ok {
		return nil // already open
	}
	defer unlock()
	cmd := torCommand(p)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Wait()
}

type torOpts struct {
	dir, index, keyring, progress string
}

type torLink struct {
	Binary string `json:"binary"`
	Sig    string `json:"sig"`
}

// torLinks picks this computer's 64-bit build out of downloads.json.
func torLinks(b []byte, keys []string) (version, binary, sig string, err error) {
	var d struct {
		Version   string                        `json:"version"`
		Downloads map[string]map[string]torLink `json:"downloads"`
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return "", "", "", fmt.Errorf("Tor Browser download list: %v", err)
	}
	for _, key := range keys {
		if l, ok := d.Downloads[key]["ALL"]; ok && l.Binary != "" && l.Sig != "" {
			for _, u := range []string{l.Binary, l.Sig} {
				if !torURL(u) {
					return "", "", "", fmt.Errorf("unexpected download address %q", u)
				}
			}
			return d.Version, l.Binary, l.Sig, nil
		}
	}
	return "", "", "", errors.New("no build for this computer in the Tor Browser download list")
}

// torURL accepts only https addresses on torproject.org.
func torURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := u.Hostname()
	return h == "torproject.org" || strings.HasSuffix(h, ".torproject.org")
}

func torGet(u string, max int64, out io.Writer, progress func(n, total int64)) error {
	resp, err := torHTTP.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", u, resp.Status)
	}
	if resp.ContentLength > max {
		return fmt.Errorf("%s: too large", u)
	}
	buf := make([]byte, 1<<20)
	var n int64
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			n += int64(k)
			if n > max {
				return fmt.Errorf("%s: too large", u)
			}
			if _, err := out.Write(buf[:k]); err != nil {
				return err
			}
			if progress != nil {
				progress(n, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("%s: %v", u, rerr)
		}
	}
}

// torKeyring reads a keyring (binary or armored) and keeps only the key
// with the pinned fingerprint.
func torKeyring(b []byte) (openpgp.EntityList, error) {
	l, err := openpgp.ReadKeyRing(bytes.NewReader(b))
	if err != nil {
		l, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(b))
	}
	if err != nil {
		return nil, fmt.Errorf("Tor Browser's signing key: %v", err)
	}
	for _, e := range l {
		if e.PrimaryKey != nil && strings.EqualFold(fmt.Sprintf("%X", e.PrimaryKey.Fingerprint), torFingerprint) {
			return openpgp.EntityList{e}, nil
		}
	}
	return nil, errors.New("Tor Browser's signing key has an unexpected fingerprint")
}

// torCheck verifies the detached signature sig of file with key.
func torCheck(key openpgp.EntityList, file, sig string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	s, err := os.Open(sig)
	if err != nil {
		return err
	}
	defer s.Close()
	_, err = openpgp.CheckArmoredDetachedSignature(key, f, s, nil)
	if err != nil && errors.Is(err, io.EOF) {
		// not armored: read it as a binary signature
		if _, err2 := f.Seek(0, io.SeekStart); err2 != nil {
			return err2
		}
		if _, err2 := s.Seek(0, io.SeekStart); err2 != nil {
			return err2
		}
		_, err = openpgp.CheckDetachedSignature(key, f, s, nil)
	}
	return err
}

// torVerify checks the download with the bundled key, and once more with a
// freshly fetched one (same fingerprint) when that key cannot.
func torVerify(keyring, file, sig string) error {
	var first error
	if b, err := os.ReadFile(keyring); err == nil {
		key, err := torKeyring(b)
		if err == nil {
			if err = torCheck(key, file, sig); err == nil {
				return nil
			}
			if !torKeyProblem(err) {
				return fmt.Errorf("signature check failed: %v", err)
			}
		}
		first = err
	}
	for _, u := range torKeyURLs() {
		var b bytes.Buffer
		if err := torGet(u, torMaxKey, &b, nil); err != nil {
			continue
		}
		key, err := torKeyring(b.Bytes())
		if err != nil {
			continue
		}
		if err := torCheck(key, file, sig); err != nil {
			return fmt.Errorf("signature check failed: %v", err)
		}
		return nil
	}
	if first != nil {
		return fmt.Errorf("signature check failed: %v", first)
	}
	return errors.New("Tor Browser's signing key could not be found")
}

// torKeyProblem: the key, not the file, is why the check failed (an
// expired or unknown subkey), so a newer copy of the same key may pass.
func torKeyProblem(err error) bool {
	return errors.Is(err, pgperrors.ErrUnknownIssuer) || errors.Is(err, pgperrors.ErrKeyExpired) ||
		errors.Is(err, pgperrors.ErrSignatureExpired) || errors.Is(err, pgperrors.ErrKeyRevoked) ||
		strings.Contains(err.Error(), "expired") || strings.Contains(err.Error(), "revoked")
}

func torFetch(o torOpts, step func(string, int)) error {
	if err := os.MkdirAll(o.dir, 0o700); err != nil {
		return err
	}
	unlock, ok := lockFile(filepath.Join(o.dir, ".lock"))
	if !ok {
		return errors.New("Tor Browser is already being downloaded")
	}
	defer unlock()

	step("index", 0)
	var idx bytes.Buffer
	if err := torGet(o.index, 1<<20, &idx, nil); err != nil {
		return err
	}
	version, binURL, sigURL, err := torLinks(idx.Bytes(), torPlatformKeys)
	if err != nil {
		return err
	}
	log.Printf("tor: Tor Browser %s: %s", version, binURL)

	file := filepath.Join(o.dir, "download"+torDownloadExt)
	sig := file + ".asc"
	defer os.Remove(file)
	defer os.Remove(sig)
	f, err := os.Create(file)
	if err != nil {
		return err
	}
	err = torGet(binURL, torMaxDownload, f, func(n, total int64) {
		if total > 0 {
			step("download", int(n*90/total))
		}
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	s, err := os.Create(sig)
	if err != nil {
		return err
	}
	err = torGet(sigURL, torMaxSig, s, nil)
	if cerr := s.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}

	step("verify", 92)
	if err := torVerify(o.keyring, file, sig); err != nil {
		return err
	}

	step("unpack", 95)
	tmp := filepath.Join(o.dir, "new")
	_ = os.RemoveAll(tmp)
	defer os.RemoveAll(tmp)
	if err := torInstall(file, tmp); err != nil {
		return err
	}
	if _, err := os.Stat(torProgram(tmp)); err != nil {
		return errors.New("the Tor Browser download has an unexpected layout")
	}
	cur := filepath.Join(o.dir, "tor-browser")
	old := filepath.Join(o.dir, "tor-browser.old")
	_ = os.RemoveAll(old)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, cur); err != nil {
		_ = os.Rename(old, cur)
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}
