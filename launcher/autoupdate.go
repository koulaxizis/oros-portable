package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Automatic updates of an installed orOS (the portable edition keeps its
// manual "update" command). Two parts:
//
//   - the orOS web app follows koulaxizis/oros main: a new commit is
//     downloaded in the background into app.next and swapped in at the next
//     start, never under a running window;
//   - the program itself (launcher + browser) follows the releases of this
//     repo: the Windows setup and the AppImage are downloaded in the
//     background and checked against their .sha256 files; the setup runs
//     silently at the next start, the AppImage replaces itself. Other
//     installs (.deb) get a message with the download page.
//
// Checks run at most every checkEvery. A file named "no-auto-update" in the
// data folder, or OROS_NO_AUTO_UPDATE, turns all of it off.

var (
	commitAPI  = "https://api.github.com/repos/koulaxizis/oros/commits/main"
	releaseAPI = "https://api.github.com/repos/koulaxizis/oros-portable/releases/latest"
)

const checkEvery = 6 * time.Hour

var httpClient = &http.Client{Timeout: 10 * time.Minute}

func autoUpdatesOn(lay layout) bool {
	if !lay.installed || os.Getenv("OROS_NO_AUTO_UPDATE") != "" || strings.Contains(version, "-") {
		return false // test builds ("0.0.0-<sha>") never update themselves
	}
	_, err := os.Stat(filepath.Join(lay.data, "no-auto-update"))
	return err != nil
}

// beforeStart applies what a previous run downloaded. It returns true when
// it handed over to the Windows setup and the launcher should exit.
func beforeStart(lay layout) bool {
	// Another orOS window is open and serving: change nothing under it.
	if !autoUpdatesOn(lay) || alreadyRunning() {
		return false
	}
	if runPendingSetup(lay) {
		return true
	}
	applyStagedApp(lay)
	return false
}

// checkUpdatesLater runs the background checks while orOS is open.
func checkUpdatesLater(lay layout) {
	if !autoUpdatesOn(lay) || !dueForCheck(lay) {
		return
	}
	go func() {
		if err := checkAppUpdate(lay); err != nil {
			log.Printf("app update: %v", err)
		}
		if err := checkProgramUpdate(lay); err != nil {
			log.Printf("program update: %v", err)
		}
	}()
}

func dueForCheck(lay layout) bool {
	stamp := filepath.Join(lay.data, "last-update-check")
	if st, err := os.Stat(stamp); err == nil && time.Since(st.ModTime()) < checkEvery {
		return false
	}
	_ = os.WriteFile(stamp, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
	return true
}

// --- the orOS web app ---

func stagedAppDir(lay layout) string { return filepath.Join(lay.updates, "app.next") }

func applyStagedApp(lay layout) {
	next := stagedAppDir(lay)
	if _, err := os.Stat(filepath.Join(next, "index.html")); err != nil {
		_ = os.RemoveAll(next)
		return
	}
	if err := swapApp(lay.updates, next); err != nil {
		log.Printf("applying the downloaded orOS: %v", err)
	}
}

func checkAppUpdate(lay layout) error {
	latest, err := latestCommit()
	if err != nil {
		return err
	}
	if latest == appCommit(lay.appDir()) || latest == appCommit(stagedAppDir(lay)) {
		return nil
	}
	tmp := stagedAppDir(lay) + ".tmp"
	if err := fetchApp(lay.updates, tmp); err != nil {
		return err
	}
	_ = os.RemoveAll(stagedAppDir(lay))
	if err := os.Rename(tmp, stagedAppDir(lay)); err != nil {
		return err
	}
	log.Printf("downloaded orOS %s; it opens at the next start", appCommit(stagedAppDir(lay)))
	return nil
}

func latestCommit() (string, error) {
	req, _ := http.NewRequest("GET", commitAPI, nil)
	req.Header.Set("Accept", "application/vnd.github.sha")
	r, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 128))
	sha := strings.TrimSpace(string(b))
	if r.StatusCode != 200 || !isCommit(sha) {
		return "", fmt.Errorf("latest commit: %s", r.Status)
	}
	return sha, nil
}

// --- the program (launcher + browser) ---

type release struct {
	Tag    string  `json:"tag_name"`
	Page   string  `json:"html_url"`
	Draft  bool    `json:"draft"`
	Pre    bool    `json:"prerelease"`
	Assets []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

func (r release) version() string { return strings.TrimPrefix(r.Tag, "v") }

func (r release) asset(name string) (asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return asset{}, false
}

func latestRelease() (release, error) {
	var rel release
	r, err := httpClient.Get(releaseAPI)
	if err != nil {
		return rel, err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return rel, fmt.Errorf("latest release: %s", r.Status)
	}
	err = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&rel)
	return rel, err
}

func updatesDir(lay layout) string { return filepath.Join(lay.data, "updates") }

func checkProgramUpdate(lay layout) error {
	rel, err := latestRelease()
	if err != nil {
		return err
	}
	if rel.Draft || rel.Pre || compareVersions(rel.version(), version) <= 0 {
		return nil
	}
	switch {
	case runtime.GOOS == "windows":
		_, err = download(rel, setupName(rel.version()), filepath.Join(updatesDir(lay), setupName(rel.version())))
		if err == nil {
			log.Printf("orOS %s downloaded; it installs at the next start", rel.version())
		}
		return err
	case os.Getenv("APPIMAGE") != "":
		return replaceAppImage(rel, os.Getenv("APPIMAGE"))
	default:
		notifyOnce(lay, rel)
		return nil
	}
}

func setupName(ver string) string    { return "orOS-Setup-" + ver + ".exe" }
func appImageName(ver string) string { return "orOS-" + ver + "-x86_64.AppImage" }

// download fetches a release asset to dest and checks it against the
// asset's .sha256 file. A file that does not match is deleted.
func download(rel release, name, dest string) (string, error) {
	file, ok := rel.asset(name)
	sum, ok2 := rel.asset(name + ".sha256")
	if !ok || !ok2 {
		return "", fmt.Errorf("release %s has no %s with a checksum", rel.Tag, name)
	}
	want, err := fetchChecksum(sum.URL)
	if err != nil {
		return "", err
	}
	if got, err := fileSHA256(dest); err == nil && got == want {
		return dest, nil // downloaded already
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	tmp := dest + ".part"
	if err := fetchTo(file.URL, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if got, err := fileSHA256(tmp); err != nil || got != want {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("%s does not match its checksum", name)
	}
	return dest, os.Rename(tmp, dest)
}

func fetchChecksum(url string) (string, error) {
	r, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 1024))
	f := strings.Fields(string(b))
	if r.StatusCode != 200 || len(f) == 0 || len(f[0]) != 64 {
		return "", errors.New("bad checksum file " + url)
	}
	return strings.ToLower(f[0]), nil
}

func fetchTo(url, dest string) error {
	r, err := httpClient.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("download %s: %s", url, r.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// pendingSetup is the newest downloaded setup that is newer than this
// launcher, or "". Older ones are deleted.
func pendingSetup(lay layout) (string, string) {
	entries, _ := os.ReadDir(updatesDir(lay))
	best, bestVer := "", version
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "orOS-Setup-") || !strings.HasSuffix(name, ".exe") {
			continue
		}
		ver := strings.TrimSuffix(strings.TrimPrefix(name, "orOS-Setup-"), ".exe")
		path := filepath.Join(updatesDir(lay), name)
		if compareVersions(ver, bestVer) > 0 {
			if best != "" {
				_ = os.Remove(best)
			}
			best, bestVer = path, ver
		} else {
			_ = os.Remove(path)
		}
	}
	return best, bestVer
}

// runPendingSetup starts a downloaded setup silently. The setup closes
// nothing (orOS has not opened yet), replaces the program and starts orOS
// again (/RELAUNCH=1). A setup that already failed once for this version
// is not tried again; the next release gets a new chance.
func runPendingSetup(lay layout) bool {
	setup, ver := pendingSetup(lay)
	if setup == "" {
		return false
	}
	tried := filepath.Join(updatesDir(lay), "tried")
	if b, _ := os.ReadFile(tried); strings.TrimSpace(string(b)) == ver {
		return false
	}
	_ = os.WriteFile(tried, []byte(ver+"\n"), 0o644)
	log.Printf("installing orOS %s", ver)
	cmd := exec.Command(setup, "/SILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CURRENTUSER", "/RELAUNCH=1")
	if err := cmd.Start(); err != nil {
		log.Printf("setup: %v", err)
		return false
	}
	return true
}

// replaceAppImage swaps the running AppImage file for the new release. The
// running copy stays mounted until it closes, so the new one is used from
// the next start. The user's own file name is kept.
func replaceAppImage(rel release, img string) error {
	tmp := filepath.Join(filepath.Dir(img), ".orOS-update.AppImage")
	if _, err := download(rel, appImageName(rel.version()), tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, img); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	log.Printf("orOS %s downloaded; it starts next time", rel.version())
	return nil
}

// notifyOnce tells about a new release that this install cannot apply by
// itself, once per release.
func notifyOnce(lay layout, rel release) {
	seen := filepath.Join(lay.data, "notified-release")
	if b, _ := os.ReadFile(seen); strings.TrimSpace(string(b)) == rel.Tag {
		return
	}
	_ = os.WriteFile(seen, []byte(rel.Tag+"\n"), 0o644)
	notify(fmt.Sprintf("orOS %s is available. Download it from %s", rel.version(), rel.Page))
}
