package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// installedMarker is a file that the Windows setup and the Linux packages
// put next to the launcher. Without it the launcher runs portable.
const installedMarker = "installed"

// layout says where each part of orOS lives.
//
// Portable: everything sits next to the launcher on the stick.
// Installed: the program folder stays as the setup left it (it may be
// read-only, like /opt), and the browser profile, the log and app updates
// go to the user's own data folder.
type layout struct {
	root      string // launcher folder: browser/ and the app/ it came with
	data      string // browser profiles and launcher.log
	updates   string // folder whose app/ the update command replaces
	installed bool
}

func newLayout(root string) layout {
	if _, err := os.Stat(filepath.Join(root, installedMarker)); err != nil {
		return layout{root: root, data: filepath.Join(root, "data"), updates: root}
	}
	d := userDataDir()
	return layout{root: root, data: d, updates: d, installed: true}
}

// userDataDir is %LOCALAPPDATA%\orOS on Windows and ~/.local/share/oros
// (or $XDG_DATA_HOME/oros) elsewhere. OROS_DATA_DIR overrides it for tests.
func userDataDir() string {
	if d := os.Getenv("OROS_DATA_DIR"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "orOS")
		}
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "oros")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "oros")
}

// appDir is the orOS folder to serve. An installed copy serves whichever
// is newer: the app it was installed with, or one that "update" fetched
// into the data folder. A newer setup then wins over an older update and
// an update wins over an older setup.
func (l layout) appDir() string {
	bundled := filepath.Join(l.root, "app")
	if !l.installed {
		return bundled
	}
	updated := filepath.Join(l.updates, "app")
	if _, err := os.Stat(filepath.Join(updated, "index.html")); err != nil {
		return bundled
	}
	if compareVersions(appVersion(updated), appVersion(bundled)) > 0 {
		return updated
	}
	return bundled
}

var appVersionRe = regexp.MustCompile(`APP_VERSION\s*=\s*"([0-9][0-9.]*)"`)

// appVersion reads APP_VERSION from the shell.js of an orOS folder, or ""
// when it cannot.
func appVersion(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "shell.js"))
	if err != nil {
		return ""
	}
	m := appVersionRe.FindSubmatch(b)
	if m == nil {
		return ""
	}
	return string(m[1])
}

// compareVersions compares dotted numbers ("2.67.00"); "" is the lowest.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	if a == "" {
		pa = nil
	}
	if b == "" {
		pb = nil
	}
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := -1, -1
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
