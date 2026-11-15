//go:build !windows

package main

// Linux: Anthropic's Claude desktop for Debian and Ubuntu, read from
// Anthropic's apt repository the way apt does, with the same code as orOS
// USB (oros-usb system/claude.go): InRelease checked against the
// repository key pinned by fingerprint (fetched from downloads.claude.ai,
// as Anthropic's install guide does), then the Packages index and the
// newest .deb by the SHA-256 each is listed with. The .deb is unpacked
// with dpkg-deb into ~/.local/share/oros-claude, so no root is needed and
// nothing outside that folder changes; ldd must find every library it
// needs on this computer. Offered only where dpkg-deb and ldd exist (the
// Debian family Anthropic supports) on amd64 or arm64.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

const claudeRepo = "https://downloads.claude.ai/claude-desktop/apt/stable"

var (
	// claudeFingerprint: Anthropic's Claude desktop repository key, as its
	// install guide publishes it (oros-usb scripts/claude-key.sh pins the
	// same); tests replace it.
	claudeFingerprint = "31DDDE24DDFAB679F42D7BD2BAA929FF1A7ECACE"
	claudeKeyURL      = "https://downloads.claude.ai/claude-desktop/key.asc"
	claudeHTTP        = &http.Client{Timeout: 30 * time.Minute}
	claudeArch        = map[string]string{"amd64": "amd64", "arm64": "arm64"}[runtime.GOARCH]
	// claudeKeyring: no key ships with orOS Portable; the published one is
	// fetched and taken only with the pinned fingerprint.
	claudeKeyring = ""
	// claudeHome is where the app goes; tests replace it.
	claudeHome = func() string {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, ".local", "share", "oros-claude")
	}
)

const (
	claudeMaxIndex = 1 << 20
	claudeMaxPkgs  = 32 << 20
	claudeMaxDeb   = 900 << 20
)

type claudeApp struct {
	Version string `json:"version"`
	Exec    string `json:"exec"` // relative to <dir>/root
}

func claudePlatformOK() bool {
	if claudeArch == "" || runtime.GOOS != "linux" {
		return false
	}
	for _, p := range []string{"dpkg-deb", "ldd"} {
		if _, err := exec.LookPath(p); err != nil {
			return false
		}
	}
	return true
}

func claudeRelOK(p string) bool {
	return p != "" && !filepath.IsAbs(p) && !strings.HasPrefix(filepath.Clean(p), "..")
}

func claudeInstalled(dir string) (claudeApp, bool) {
	var a claudeApp
	b, err := os.ReadFile(filepath.Join(dir, "app.json"))
	if err != nil || json.Unmarshal(b, &a) != nil || !claudeRelOK(a.Exec) {
		return a, false
	}
	if fi, err := os.Stat(filepath.Join(dir, "root", a.Exec)); err != nil || !fi.Mode().IsRegular() {
		return a, false
	}
	return a, true
}

func claudeVersion() string {
	a, ok := claudeInstalled(claudeHome())
	if !ok {
		return ""
	}
	return a.Version
}

// claudeStart runs Claude desktop detached in its own window.
func claudeStart() error {
	dir := claudeHome()
	a, ok := claudeInstalled(dir)
	if !ok {
		return errors.New("Claude desktop is not installed")
	}
	bin := filepath.Join(dir, "root", a.Exec)
	cmd := exec.Command(bin)
	cmd.Dir = filepath.Dir(bin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// claudeUninstall deletes the program; the person's sign-in
// (~/.config/Claude) stays, as after uninstalling any app.
func claudeUninstall() error {
	dir := claudeHome()
	if filepath.Base(dir) != "oros-claude" {
		return errors.New("unexpected Claude desktop folder")
	}
	_ = os.Remove(filepath.Join(dir, "app.json"))
	return os.RemoveAll(filepath.Join(dir, "root"))
}

func claudeFetch(step func(string, int)) error {
	if claudeArch == "" {
		return errors.New("os: this processor has no Claude desktop")
	}
	dir := claudeHome()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	step("index", 0)
	var ir bytes.Buffer
	if err := claudeGet(claudeRepo+"/dists/stable/InRelease", claudeMaxIndex, &ir, nil); err != nil {
		return err
	}
	text, err := claudeRelease(ir.Bytes(), claudeKeyring)
	if err != nil {
		return err
	}
	fields, files := releaseFields(text)
	if vu := fields["Valid-Until"]; vu != "" {
		if t, err := time.Parse(time.RFC1123, strings.Replace(vu, "UTC", "GMT", 1)); err == nil && time.Now().After(t) {
			return errors.New("Anthropic's repository index has expired")
		}
	}
	var pkgs []byte
	for _, name := range []string{"main/binary-" + claudeArch + "/Packages", "main/binary-" + claudeArch + "/Packages.gz"} {
		want, ok := files[name]
		if !ok {
			continue
		}
		var b bytes.Buffer
		if err := claudeGet(claudeRepo+"/dists/stable/"+name, claudeMaxPkgs, &b, nil); err != nil {
			return err
		}
		if err := claudeSum(b.Bytes(), want); err != nil {
			return fmt.Errorf("%s: %v", name, err)
		}
		pkgs = b.Bytes()
		if strings.HasSuffix(name, ".gz") {
			zr, err := gzip.NewReader(bytes.NewReader(pkgs))
			if err != nil {
				return err
			}
			if pkgs, err = io.ReadAll(io.LimitReader(zr, claudeMaxPkgs)); err != nil {
				return err
			}
		}
		break
	}
	if pkgs == nil {
		return fmt.Errorf("os: Anthropic's repository lists no packages for %s", claudeArch)
	}
	pkg, err := claudePick(pkgs, claudeArch)
	if err != nil {
		return err
	}
	if pkg.size > claudeMaxDeb {
		return errors.New("the Claude desktop package is unexpectedly large")
	}
	if a, ok := claudeInstalled(dir); ok && a.Version == pkg.version {
		return nil
	}
	deb := filepath.Join(dir, "download.deb")
	defer os.Remove(deb)
	f, err := os.Create(deb)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = claudeGet(claudeRepo+"/"+pkg.filename, pkg.size, io.MultiWriter(f, h), func(n, _ int64) {
		step("download", int(n*90/pkg.size))
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	step("verify", 92)
	if hex.EncodeToString(h.Sum(nil)) != pkg.sum {
		return errors.New("the Claude desktop download does not match Anthropic's signed index")
	}
	step("unpack", 95)
	tmp := filepath.Join(dir, "new")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if out, err := exec.Command("dpkg-deb", "-x", deb, tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("unpacking Claude desktop: %v %s", err, strings.TrimSpace(string(out)))
	}
	rel, err := claudeFindExec(tmp)
	if err != nil {
		return err
	}
	if missing := claudeMissingLibs(filepath.Join(tmp, rel)); len(missing) > 0 {
		return fmt.Errorf("os: this computer lacks libraries Claude desktop needs: %s", strings.Join(missing, ", "))
	}
	cur, old := filepath.Join(dir, "root"), filepath.Join(dir, "root.old")
	_ = os.Remove(filepath.Join(dir, "app.json"))
	_ = os.RemoveAll(old)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, old); err != nil {
			return err
		}
	}
	if err := os.Rename(tmp, cur); err != nil {
		return err
	}
	_ = os.RemoveAll(old)
	b, _ := json.Marshal(claudeApp{Version: pkg.version, Exec: rel})
	return os.WriteFile(filepath.Join(dir, "app.json"), b, 0o600)
}

type claudeFile struct {
	sum  string
	size int64
}

type claudePkg struct {
	version, filename, sum string
	size                   int64
}

var claudePoolRe = regexp.MustCompile(`^pool/[A-Za-z0-9._+~/-]+\.deb$`)

func claudeGet(u string, max int64, out io.Writer, step func(n, total int64)) error {
	resp, err := claudeHTTP.Get(u)
	if err != nil {
		return fmt.Errorf("net: %v", err)
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
			if step != nil {
				step(n, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("net: %s: %v", u, rerr)
		}
	}
}

// claudeKeys keeps only the key with the pinned fingerprint.
func claudeKeys(b []byte) (openpgp.EntityList, error) {
	l, err := openpgp.ReadKeyRing(bytes.NewReader(b))
	if err != nil {
		l, err = openpgp.ReadArmoredKeyRing(bytes.NewReader(b))
	}
	if err != nil {
		return nil, fmt.Errorf("Claude desktop's signing key: %v", err)
	}
	for _, e := range l {
		if e.PrimaryKey != nil && strings.EqualFold(fmt.Sprintf("%X", e.PrimaryKey.Fingerprint), claudeFingerprint) {
			return openpgp.EntityList{e}, nil
		}
	}
	return nil, errors.New("Claude desktop's signing key has an unexpected fingerprint")
}

// claudeRelease checks InRelease's signature and returns its signed text.
// The image's key comes first; when it cannot check (a renewed subkey),
// Anthropic's published key is fetched and used only with the same
// fingerprint.
func claudeRelease(inRelease []byte, keyring string) ([]byte, error) {
	block, _ := clearsign.Decode(inRelease)
	if block == nil {
		return nil, errors.New("the repository's InRelease is not signed")
	}
	check := func(key openpgp.EntityList) error {
		_, err := openpgp.CheckDetachedSignature(key, bytes.NewReader(block.Bytes), block.ArmoredSignature.Body, nil)
		return err
	}
	var first error
	if b, err := os.ReadFile(keyring); err == nil {
		if key, err := claudeKeys(b); err == nil {
			if first = check(key); first == nil {
				return block.Plaintext, nil
			}
			if !torKeyProblem(first) {
				return nil, fmt.Errorf("signature check failed: %v", first)
			}
		} else {
			first = err
		}
	}
	// the signature was read once: decode again for the second check
	block, _ = clearsign.Decode(inRelease)
	var kb bytes.Buffer
	if err := claudeGet(claudeKeyURL, 1<<20, &kb, nil); err != nil {
		if first != nil {
			return nil, fmt.Errorf("signature check failed: %v", first)
		}
		return nil, err
	}
	key, err := claudeKeys(kb.Bytes())
	if err != nil {
		return nil, err
	}
	if err := check(key); err != nil {
		return nil, fmt.Errorf("signature check failed: %v", err)
	}
	return block.Plaintext, nil
}

// releaseFields reads a Release file: its single-line fields and the
// SHA256 list (path → hash and size).
func releaseFields(text []byte) (map[string]string, map[string]claudeFile) {
	fields := map[string]string{}
	files := map[string]claudeFile{}
	inSHA := false
	sc := bufio.NewScanner(bytes.NewReader(text))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, " ") {
			if inSHA {
				f := strings.Fields(line)
				if len(f) == 3 {
					if n, err := strconv.ParseInt(f[1], 10, 64); err == nil && len(f[0]) == 64 {
						files[f[2]] = claudeFile{strings.ToLower(f[0]), n}
					}
				}
			}
			continue
		}
		inSHA = false
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if k == "SHA256" {
			inSHA = true
			continue
		}
		fields[k] = strings.TrimSpace(v)
	}
	return fields, files
}

// claudePick returns the newest claude-desktop for arch in a Packages
// index.
func claudePick(index []byte, arch string) (claudePkg, error) {
	var best claudePkg
	for _, st := range bytes.Split(index, []byte("\n\n")) {
		f := map[string]string{}
		for _, line := range strings.Split(string(st), "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && !strings.HasPrefix(line, " ") {
				f[k] = strings.TrimSpace(v)
			}
		}
		if f["Package"] != "claude-desktop" || (f["Architecture"] != arch && f["Architecture"] != "all") {
			continue
		}
		size, err := strconv.ParseInt(f["Size"], 10, 64)
		sum := strings.ToLower(f["SHA256"])
		fn := f["Filename"]
		if err != nil || size <= 0 || len(sum) != 64 || !claudePoolRe.MatchString(fn) || strings.Contains(fn, "..") || f["Version"] == "" {
			continue
		}
		if best.version == "" || debCompare(f["Version"], best.version) > 0 {
			best = claudePkg{f["Version"], fn, sum, size}
		}
	}
	if best.version == "" {
		return best, fmt.Errorf("os: no Claude desktop for %s in Anthropic's repository", arch)
	}
	return best, nil
}

func claudeSum(b []byte, want claudeFile) error {
	s := sha256.Sum256(b)
	if int64(len(b)) != want.size || hex.EncodeToString(s[:]) != want.sum {
		return errors.New("does not match Anthropic's signed index")
	}
	return nil
}

// claudeFindExec finds the program in the unpacked package: the target of
// /usr/bin/claude-desktop or of the menu entry's Exec when that is a
// program (ELF), otherwise the Electron program next to resources/app.asar.
// Links are followed inside root only. It returns the path relative to
// root.
func claudeFindExec(root string) (string, error) {
	cands := []string{"/usr/bin/claude-desktop"}
	if ds, _ := filepath.Glob(filepath.Join(root, "usr/share/applications/*.desktop")); len(ds) > 0 {
		for _, d := range ds {
			b, err := os.ReadFile(d)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(line, "Exec="); ok {
					if f := strings.Fields(v); len(f) > 0 {
						p := strings.Trim(f[0], `"`)
						if !filepath.IsAbs(p) {
							p = "/usr/bin/" + p
						}
						cands = append(cands, p)
					}
					break
				}
			}
		}
	}
	for _, c := range cands {
		if rel, ok := claudeResolve(root, c); ok && isELF(filepath.Join(root, rel)) {
			return rel, nil
		}
	}
	var found string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || found != "" || !d.IsDir() || filepath.Base(p) != "resources" {
			return nil
		}
		if _, err := os.Stat(filepath.Join(p, "app.asar")); err != nil {
			return nil
		}
		ents, _ := os.ReadDir(filepath.Dir(p))
		for _, e := range ents {
			q := filepath.Join(filepath.Dir(p), e.Name())
			fi, err := os.Lstat(q)
			if err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 && strings.Contains(strings.ToLower(e.Name()), "claude") && isELF(q) {
				found = q
				return filepath.SkipAll
			}
		}
		return nil
	})
	if found == "" {
		return "", errors.New("the Claude desktop package has an unexpected layout")
	}
	rel, _ := filepath.Rel(root, found)
	return rel, nil
}

// claudeResolve follows symbolic links of the absolute path p as if root
// were /, and never leaves root. It returns a path relative to root.
func claudeResolve(root, p string) (string, bool) {
	cur := filepath.Clean("/" + p)
	for i := 0; i < 16; i++ {
		full := filepath.Join(root, cur)
		fi, err := os.Lstat(full)
		if err != nil {
			return "", false
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			if !fi.Mode().IsRegular() || fi.Mode()&0o111 == 0 {
				return "", false
			}
			return strings.TrimPrefix(cur, "/"), true
		}
		t, err := os.Readlink(full)
		if err != nil {
			return "", false
		}
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(cur), t)
		}
		cur = filepath.Clean("/" + t)
	}
	return "", false
}

func isELF(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 4)
	_, err = io.ReadFull(f, b)
	return err == nil && string(b) == "\x7fELF"
}

// claudeMissingLibs lists the shared libraries the program needs that
// this system lacks (ldd's "not found"), with the package's own folder on
// the search path.
func claudeMissingLibs(bin string) []string {
	cmd := exec.Command("ldd", bin)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+filepath.Dir(bin))
	out, _ := cmd.Output()
	var miss []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "not found") {
			if f := strings.Fields(line); len(f) > 0 {
				miss = append(miss, f[0])
			}
		}
	}
	return miss
}

// debCompare compares two Debian version strings like dpkg does
// (epoch:upstream-revision, "~" before everything, letters before other
// characters).
func debCompare(a, b string) int {
	ea, ua, ra := debSplit(a)
	eb, ub, rb := debSplit(b)
	if ea != eb {
		if ea < eb {
			return -1
		}
		return 1
	}
	if c := debPart(ua, ub); c != 0 {
		return c
	}
	return debPart(ra, rb)
}

func debSplit(v string) (int, string, string) {
	e := 0
	if i := strings.IndexByte(v, ':'); i >= 0 {
		e, _ = strconv.Atoi(v[:i])
		v = v[i+1:]
	}
	r := ""
	if i := strings.LastIndexByte(v, '-'); i >= 0 {
		r = v[i+1:]
		v = v[:i]
	}
	return e, v, r
}

func debOrder(c byte) int {
	switch {
	case c == '~':
		return -1
	case c >= '0' && c <= '9':
		return 0
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		return int(c)
	default:
		return int(c) + 256
	}
}

func debPart(a, b string) int {
	for a != "" || b != "" {
		for (a != "" && (a[0] < '0' || a[0] > '9')) || (b != "" && (b[0] < '0' || b[0] > '9')) {
			var ca, cb int
			if a != "" && (a[0] < '0' || a[0] > '9') {
				ca = debOrder(a[0])
			}
			if b != "" && (b[0] < '0' || b[0] > '9') {
				cb = debOrder(b[0])
			}
			if ca != cb {
				if ca < cb {
					return -1
				}
				return 1
			}
			if a != "" {
				a = a[1:]
			}
			if b != "" {
				b = b[1:]
			}
		}
		i, j := 0, 0
		for i < len(a) && a[i] >= '0' && a[i] <= '9' {
			i++
		}
		for j < len(b) && b[j] >= '0' && b[j] <= '9' {
			j++
		}
		na := strings.TrimLeft(a[:i], "0")
		nb := strings.TrimLeft(b[:j], "0")
		if len(na) != len(nb) {
			if len(na) < len(nb) {
				return -1
			}
			return 1
		}
		if na != nb {
			if na < nb {
				return -1
			}
			return 1
		}
		a, b = a[i:], b[j:]
	}
	return 0
}
