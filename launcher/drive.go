package main

// orOS Drive in the orOS tray icon (bundle plan, Chris 2026-11-17: one
// icon for orOS, orOS Drive and orOS Bridge). orOS Drive stays its own
// program (koulaxizis/oros-drive: its own folder, settings, sign-in,
// Explorer folder and uninstaller). On an installed orOS the tray:
//   - hands Drive over once (`orosdrive manage --by <this program>`), so
//     Drive shows no icon and has no login start of its own;
//   - starts it when it has settings, and again if it stopped (at most
//     3 times in 10 minutes), and stops it on Quit;
//   - shows an "orOS Drive" menu: state, Sync now, Pause, Settings;
//   - passes Drive's Dropbox sign-in on: this server holds
//     localhost:47800, the address Dropbox sends the answer to.
// Drive's own answers: GET /api/summary (v1) and POST /api/sync, /api/pause,
// /api/quit on 127.0.0.1, with the cookie from its instance.json.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// driveSignInPath: Drive asks whether this orOS passes its sign-in on.
const driveSignInPath = "/__oros/drive-signin"

// driveConfigDir is orOS Drive's settings folder (oros-drive config.Dir).
func driveConfigDir() string {
	if d := os.Getenv("OROS_DRIVE_CONFIG"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "orOS Drive")
}

// driveExe is the orOS Drive command line program: the one installed on
// its own first (its setup on Windows, its .deb on Linux), then the one
// this orOS carries (drive/<os>/, the Linux packages). "" = none.
func driveExe(lay layout) string {
	p := driveFind(lay)
	if p != "" && os.Getenv("APPIMAGE") != "" && p == filepath.Join(lay.root, "drive", runtime.GOOS, "orosdrive") {
		return driveOutOfAppImage(lay, p)
	}
	return p
}

// driveFind is driveExe without copying it out of an AppImage.
func driveFind(lay layout) string {
	var c []string
	if v := os.Getenv("OROS_DRIVE_EXE"); v != "" {
		c = append(c, v)
	}
	if runtime.GOOS == "windows" {
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			c = append(c, filepath.Join(la, "Programs", "orOS Drive", "orosdrive.exe"))
		}
	} else {
		c = append(c, "/usr/bin/orosdrive", filepath.Join(lay.root, "drive", runtime.GOOS, "orosdrive"))
	}
	for _, p := range c {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// driveOutOfAppImage copies the Drive an AppImage carries next to the
// user's orOS data and returns that copy: the AppImage's files go away
// when it ends, and Drive must keep running and start again from the
// same path. A newer AppImage brings a new copy.
func driveOutOfAppImage(lay layout, src string) string {
	dst := filepath.Join(lay.data, "drive", "orosdrive")
	a, err := os.ReadFile(src)
	if err != nil {
		return ""
	}
	if b, err := os.ReadFile(dst); err == nil && bytes.Equal(a, b) {
		return dst
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return ""
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, a, 0o755); err != nil {
		return ""
	}
	if err := os.Rename(tmp, dst); err != nil { // a running Drive keeps its old file
		os.Remove(tmp)
		return ""
	}
	return dst
}

// driveAppExe starts the app itself: on Windows the windowless
// "orOS Drive.exe" beside orosdrive.exe (no console window).
func driveAppExe(cli string) string {
	if runtime.GOOS == "windows" {
		g := filepath.Join(filepath.Dir(cli), "orOS Drive.exe")
		if _, err := os.Stat(g); err == nil {
			return g
		}
	}
	return cli
}

// managerPath is what Drive records as the orOS that shows it: this
// program, or the AppImage it runs from.
func managerPath() string {
	if img := os.Getenv("APPIMAGE"); img != "" {
		return img
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe
}

// driveSettings is the part of Drive's app.json the tray reads.
type driveSettings struct {
	exists       bool
	Folders      []json.RawMessage `json:"folders"`
	ManagedBy    string            `json:"managedBy"`
	OwnAutostart bool              `json:"ownAutostart"`
}

func readDriveSettings() driveSettings {
	var s driveSettings
	b, err := os.ReadFile(filepath.Join(driveConfigDir(), "app.json"))
	if err != nil {
		return s
	}
	s.exists = true
	_ = json.Unmarshal(b, &s)
	return s
}

// driveSummary is Drive's GET /api/summary.
type driveSummary struct {
	V        int    `json:"v"`
	Version  string `json:"version"`
	State    string `json:"state"` // warn, paused, sync, bad, ok
	SignedIn bool   `json:"signedIn"`
	Unlocked bool   `json:"unlocked"`
	Paused   bool   `json:"paused"`
	Syncing  bool   `json:"syncing"`
	Folders  int    `json:"folders"`
	Managed  bool   `json:"managed"`
	Update   string `json:"update"`
	// Drive 0.6+: this pass's uploads (only while syncing) and the
	// deletions its Explorer folder waits to be allowed.
	Progress *driveProgress `json:"progress"`
	Held     int            `json:"held"`
}

type driveProgress struct {
	Files     int   `json:"files"`
	FilesDone int   `json:"filesDone"`
	Bytes     int64 `json:"bytes"`
	BytesDone int64 `json:"bytesDone"`
}

type driveInstance struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

var errDriveOff = errors.New("orOS Drive is not running")

// driveCall sends one request to the running Drive app.
func driveCall(method, path string, body any, out any) error {
	b, err := os.ReadFile(filepath.Join(driveConfigDir(), "instance.json"))
	if err != nil {
		return errDriveOff
	}
	var in driveInstance
	if json.Unmarshal(b, &in) != nil || in.Port < 47811 || in.Port > 47820 || in.Token == "" {
		return errDriveOff
	}
	var rd *bytes.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", in.Port, path), rd)
	req.AddCookie(&http.Cookie{Name: "orosdrive", Value: in.Token})
	if method == "POST" {
		req.Header.Set("X-OD", "1")
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return errDriveOff
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("orOS Drive: %s", res.Status)
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func driveStatus() (driveSummary, error) {
	var s driveSummary
	err := driveCall("GET", "/api/summary", nil, &s)
	if err == nil && s.V < 1 {
		err = errors.New("orOS Drive too old for the orOS menu")
	}
	return s, err
}

// driveRun runs one orosdrive command (no window) and waits.
func driveRun(cli string, args ...string) error {
	cmd := exec.Command(cli, args...)
	driveProcAttr(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("orosdrive %v: %v: %s", args, err, bytes.TrimSpace(out))
	}
	return nil
}

// driveStart starts the Drive app in the background, on its own: it
// does not end with the tray (Quit asks it to, driveQuit).
func driveStart(cli string, background bool) error {
	args := []string{"run"}
	if background {
		args = append(args, "--background")
	}
	cmd := exec.Command(driveAppExe(cli), args...)
	cmd.Dir = filepath.Dir(cli)
	driveProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func driveQuit() { _ = driveCall("POST", "/api/quit", map[string]any{}, nil) }

// driveTakeOver hands Drive to this orOS once, and makes orOS start
// with the computer when Drive started by itself before or mirrors
// folders (they must go on syncing after a restart).
func driveTakeOver(cli string) {
	me := managerPath()
	if me == "" {
		return
	}
	s := readDriveSettings()
	if s.ManagedBy == me {
		return
	}
	if err := driveRun(cli, "manage", "--by", me); err != nil {
		log.Printf("orOS Drive: %v", err)
		return
	}
	log.Printf("orOS Drive: shown in the orOS icon from now on")
	s = readDriveSettings()
	if (s.OwnAutostart || len(s.Folders) > 0) && !autostartOn() {
		if err := setAutostart(true, trayCommand()); err != nil {
			log.Printf("autostart: %v", err)
		}
	}
}

// driveWanted: this computer has an orOS Drive set up (its settings
// exist), so the tray, which starts and keeps it, should run.
func driveWanted(lay layout) bool {
	return lay.installed && driveFind(lay) != "" && readDriveSettings().exists
}

// runDrive is "orOS drive" (the menu entry's action on Linux): the tray
// that keeps orOS Drive starts if it does not run, then Drive's settings
// open (starting Drive first when it is not running).
func runDrive(lay layout) {
	cli := driveExe(lay)
	if !lay.installed || cli == "" {
		fail(word("orOS Drive is not installed with this orOS.", "Το orOS Drive δεν είναι εγκατεστημένο με αυτό το orOS.",
			"orOS Drive ist mit diesem orOS nicht installiert."))
	}
	driveTakeOver(cli)
	startTrayFor(lay)
	if _, err := driveStatus(); err == nil {
		if err := driveRun(cli, "open"); err != nil {
			log.Printf("orOS Drive: %v", err)
		}
		return
	}
	if err := driveStart(cli, false); err != nil {
		fail("orOS Drive: " + err.Error())
	}
}

// driveKeeper starts Drive when it should run and restarts it if it
// stops, at most 3 starts in 10 minutes.
type driveKeeper struct {
	starts []time.Time
}

func (k *driveKeeper) mayStart(now time.Time) bool {
	keep := k.starts[:0]
	for _, t := range k.starts {
		if now.Sub(t) < 10*time.Minute {
			keep = append(keep, t)
		}
	}
	k.starts = keep
	if len(k.starts) >= 3 {
		return false
	}
	k.starts = append(k.starts, now)
	return true
}

// driveRedirect passes Drive's Dropbox sign-in on: a state that orOS
// Drive made ("orosdrive-<port>-<random>", port 47811 to 47820, its
// settings page) is sent to that page with the same query. Drive takes
// only its own waiting state.
var driveStateRE = regexp.MustCompile(`^orosdrive-(\d{5})-[A-Za-z0-9_-]{16,64}$`)

func driveRedirect(r *http.Request) (string, bool) {
	if r.URL.Path != "/" {
		return "", false
	}
	q := r.URL.Query()
	m := driveStateRE.FindStringSubmatch(q.Get("state"))
	if m == nil {
		return "", false
	}
	port, _ := strconv.Atoi(m[1])
	if port < 47811 || port > 47820 {
		return "", false
	}
	return fmt.Sprintf("http://127.0.0.1:%d/oauth/done?%s", port, r.URL.RawQuery), true
}

// driveText is the state line of the menu.
func driveText(s driveSummary, err error) string {
	if err != nil {
		return word("Not running", "Δεν τρέχει", "Läuft nicht")
	}
	t := driveState(s)
	if s.Held > 0 {
		t += fmt.Sprintf(word(" · %d deletions wait in its settings", " · %d διαγραφές περιμένουν στις ρυθμίσεις του", " · %d Löschungen warten in den Einstellungen"), s.Held)
	}
	return t
}

func driveState(s driveSummary) string {
	switch s.State {
	case "warn":
		return word("Open the settings to set up", "Άνοιξε τις ρυθμίσεις για να ξεκινήσεις", "Zum Einrichten die Einstellungen öffnen")
	case "paused":
		return word("Paused", "Σε παύση", "Pausiert")
	case "sync":
		if p := s.Progress; p != nil && p.Files > 0 {
			return fmt.Sprintf(word("Uploading %d of %d (%s of %s)", "Ανεβαίνουν %d από %d (%s από %s)", "Lädt hoch: %d von %d (%s von %s)"),
				p.FilesDone, p.Files, driveSize(p.BytesDone), driveSize(p.Bytes))
		}
		return word("Syncing…", "Συγχρονίζει…", "Synchronisiert…")
	case "bad":
		return word("Needs attention", "Θέλει προσοχή", "Braucht Aufmerksamkeit")
	}
	return word("Up to date", "Ενημερωμένο", "Aktuell")
}

// driveSize: 512 B, 3.4 MB, 43 GB (a decimal comma in Greek and German).
func driveSize(n int64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f, i := float64(n), 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	t := fmt.Sprintf("%.0f %s", f, units[i])
	if i > 0 && f < 10 {
		t = fmt.Sprintf("%.1f %s", f, units[i])
		if l := systemLang(); l == "el" || l == "de" {
			t = strings.Replace(t, ".", ",", 1)
		}
	}
	if i == 0 {
		t = fmt.Sprintf("%d B", n)
	}
	return t
}
