package main

// orOS's screen lock with Windows Hello or a fingerprint (LK-BIO, wave 2;
// koulaxizis/oros lock.js). The page turns it on in Settings › Screen
// lock and the lock screen asks here through the System extension:
//
//   {cmd:"bio.state"}            → {can, kind, kids}
//   {cmd:"bio.seal", kid, text}  → the person verifies, then a new 32-byte
//                                  secret D for kid, kept here, comes back
//   {cmd:"bio.open", kid, text}  → the person verifies, then D for kid
//   {cmd:"bio.clear", kid}       → D for kid removed
//
// The page keeps a token wrapped with D, so the lock opens only with a D
// that this computer gave after Windows Hello (fingerprint, face or the
// Windows Hello PIN; lockbio_windows.go) or a fingerprint through fprintd
// (lockbio_linux.go). D lies in the user's folder (lock-bio.json): on
// Windows sealed with DPAPI for this Windows user, on Linux readable only
// by this user (0600). As the lock itself, it protects the screen, not the
// disk. Errors: cancel, lockout, none (nothing enrolled, or no D for kid),
// no-hw, busy, timeout, failed, full, bad.

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	bioFileName = "lock-bio.json"
	bioMaxKids  = 16
)

var bioKidRe = regexp.MustCompile(`^[0-9a-f]{24}$`)

// bioError is an answer code the page shows a message for.
type bioError string

func (e bioError) Error() string { return string(e) }

// bioOps is the part that differs between Windows and Linux.
type bioOps interface {
	// kind: "hello", "finger", or "" when this system has no way at all.
	kind() string
	// can: ok | none (nothing enrolled) | no-hw.
	can() string
	// verify asks the person; nil when they passed.
	verify(text string) error
	seal(d []byte) ([]byte, error)
	unseal(b []byte) ([]byte, error)
}

// bioPlatform is what this build can do; tests replace it.
var bioPlatform bioOps = hostBio()

type bioStoreFile struct {
	V    int               `json:"v"`
	Kids map[string]string `json:"kids"`
}

func bioPath(lay layout) string { return filepath.Join(lay.userFolder(), bioFileName) }

func bioLoad(lay layout) bioStoreFile {
	s := bioStoreFile{V: 1, Kids: map[string]string{}}
	b, err := os.ReadFile(bioPath(lay))
	if err != nil {
		return s
	}
	var in bioStoreFile
	if json.Unmarshal(b, &in) != nil || in.V != 1 {
		return s
	}
	for k, v := range in.Kids {
		if bioKidRe.MatchString(k) && v != "" && len(s.Kids) < bioMaxKids {
			s.Kids[k] = v
		}
	}
	return s
}

func bioSave(lay layout, s bioStoreFile) error {
	p := bioPath(lay)
	if len(s.Kids) == 0 {
		err := os.Remove(p)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func bioKids(s bioStoreFile) []string {
	out := make([]string, 0, len(s.Kids))
	for k := range s.Kids {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// bioText is the line the prompt shows: the page's own words ("Unlock
// orOS" in the person's language), one short line of plain text.
func bioText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 80 {
		s = string(r[:80])
	}
	if s == "" {
		s = "orOS"
	}
	return s
}

// hostParentWindow is the browser window Chromium names when it starts
// the host on Windows (--parent-window=<HWND>); Windows Hello's prompt
// opens over it. 0 elsewhere.
var hostParentWindow uintptr

func parentWindowArg(args []string) uintptr {
	for _, a := range args {
		if v, found := strings.CutPrefix(a, "--parent-window="); found {
			var n uint64
			for _, c := range v {
				if c < '0' || c > '9' || n > 1<<52 {
					return 0
				}
				n = n*10 + uint64(c-'0')
			}
			return uintptr(n)
		}
	}
	return 0
}

func bioFailCode(err error) sysReply {
	var be bioError
	if errors.As(err, &be) {
		return sysReply{"ok": false, "error": string(be)}
	}
	return sysReply{"ok": false, "error": "failed"}
}

func bioHost(lay layout, req sysRequest) sysReply {
	p := bioPlatform
	if req.Cmd == "bio.state" {
		c := "no-hw"
		if p.kind() != "" {
			c = p.can()
		}
		return ok(sysReply{"can": c, "kind": p.kind(), "kids": bioKids(bioLoad(lay))})
	}
	if !bioKidRe.MatchString(req.Kid) {
		return sysReply{"ok": false, "error": "bad"}
	}
	s := bioLoad(lay)
	switch req.Cmd {
	case "bio.clear":
		delete(s.Kids, req.Kid)
		if err := bioSave(lay, s); err != nil {
			return sysFail(err)
		}
		return ok(nil)
	case "bio.seal":
		if _, has := s.Kids[req.Kid]; !has && len(s.Kids) >= bioMaxKids {
			return sysReply{"ok": false, "error": "full"}
		}
		if p.kind() == "" {
			return sysReply{"ok": false, "error": "no-hw"}
		}
		if err := p.verify(bioText(req.Text)); err != nil {
			return bioFailCode(err)
		}
		d := make([]byte, 32)
		if _, err := rand.Read(d); err != nil {
			return bioFailCode(err)
		}
		sealed, err := p.seal(d)
		if err != nil {
			return bioFailCode(err)
		}
		s.Kids[req.Kid] = base64.StdEncoding.EncodeToString(sealed)
		if err := bioSave(lay, s); err != nil {
			return bioFailCode(err)
		}
		return ok(sysReply{"d": base64.StdEncoding.EncodeToString(d)})
	case "bio.open":
		enc, has := s.Kids[req.Kid]
		if !has {
			return sysReply{"ok": false, "error": "none"}
		}
		if p.kind() == "" {
			return sysReply{"ok": false, "error": "no-hw"}
		}
		if err := p.verify(bioText(req.Text)); err != nil {
			return bioFailCode(err)
		}
		sealed, err := base64.StdEncoding.DecodeString(enc)
		var d []byte
		if err == nil {
			d, err = p.unseal(sealed)
		}
		if err != nil || len(d) != 32 {
			// Another Windows user's copy, a damaged file: it can never
			// open, so it goes (the page then asks to turn it on again).
			delete(s.Kids, req.Kid)
			_ = bioSave(lay, s)
			return sysReply{"ok": false, "error": "none"}
		}
		return ok(sysReply{"d": base64.StdEncoding.EncodeToString(d)})
	}
	return sysFail(errUnsupported)
}
