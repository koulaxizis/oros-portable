package main

// Windows: Anthropic's Claude desktop through winget (package
// Anthropic.Claude). winget downloads Anthropic's installer from the
// address in its manifest and checks it against the manifest's SHA-256
// before it runs; the app goes onto this computer for this Windows user,
// like any program, and keeps itself up to date. Offered only where
// winget is there (Windows 10 1809 and later with App Installer); without
// it orOS's Settings shows Anthropic's download page instead.

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const claudeWingetID = "Anthropic.Claude"

func claudePlatformOK() bool {
	_, err := exec.LookPath("winget")
	return err == nil
}

var claudeCache struct {
	sync.Mutex
	at      time.Time
	version string
}

func claudeForget() {
	claudeCache.Lock()
	claudeCache.at = time.Time{}
	claudeCache.Unlock()
}

// claudeVersion is the installed version from "winget list", or "";
// winget is slow, so the answer is kept for half a minute.
func claudeVersion() string {
	claudeCache.Lock()
	defer claudeCache.Unlock()
	if time.Since(claudeCache.at) < 30*time.Second {
		return claudeCache.version
	}
	out, _ := hidden("winget", "list", "--id", claudeWingetID, "-e",
		"--accept-source-agreements", "--disable-interactivity").Output()
	claudeCache.version = wingetVersion(string(out), claudeWingetID)
	claudeCache.at = time.Now()
	return claudeCache.version
}

// wingetVersion reads the version column of the row with id in a
// "winget list" table.
func wingetVersion(out, id string) string {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		for i := 0; i+1 < len(f); i++ {
			if strings.EqualFold(f[i], id) {
				return f[i+1]
			}
		}
	}
	return ""
}

func claudeFetch(step func(string, int)) error {
	step("download", 10)
	defer claudeForget()
	out, err := hidden("winget", "install", "--id", claudeWingetID, "-e", "--silent",
		"--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity").CombinedOutput()
	if err != nil {
		// winget says "already installed" with a non-zero code when no
		// newer version exists: that is not a failure.
		if wingetVersion(func() string {
			o, _ := hidden("winget", "list", "--id", claudeWingetID, "-e", "--accept-source-agreements", "--disable-interactivity").Output()
			return string(o)
		}(), claudeWingetID) != "" {
			return nil
		}
		return fmt.Errorf("winget could not install Claude desktop: %v %s", err, lastLine(string(out)))
	}
	step("verify", 95)
	return nil
}

func lastLine(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(l[len(l)-1])
}

var claudeAppID = regexp.MustCompile(`^[A-Za-z0-9._!\\-]{3,200}$`)

// claudeStart opens Claude desktop through its Start menu entry.
func claudeStart() error {
	id, err := powershell(`(Get-StartApps | Where-Object { $_.Name -eq 'Claude' } | Select-Object -First 1).AppID`)
	id = strings.TrimSpace(id)
	if err != nil || !claudeAppID.MatchString(id) {
		return errors.New("Claude desktop is not installed")
	}
	return hidden("explorer.exe", `shell:AppsFolder\`+id).Start()
}

func claudeUninstall() error {
	defer claudeForget()
	out, err := hidden("winget", "uninstall", "--id", claudeWingetID, "-e", "--silent",
		"--accept-source-agreements", "--disable-interactivity").CombinedOutput()
	if err != nil {
		return fmt.Errorf("winget could not remove Claude desktop: %v %s", err, lastLine(string(out)))
	}
	return nil
}
