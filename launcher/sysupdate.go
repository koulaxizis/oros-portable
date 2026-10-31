package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// "Update orOS" in the System panel of a portable copy (installed copies
// update themselves, autoupdate.go). The download runs in a process of
// its own ("orOS update-stage"), because Chromium ends the native host as
// soon as it has answered; it stages the new orOS in app.next, which the
// next start swaps in (beforeStart), and writes its progress to a file
// that "update.status" reads.

const updateProgressFile = "update-progress.json"

type updateProgress struct {
	Running bool   `json:"running"`
	Done    bool   `json:"done"`
	Error   string `json:"error,omitempty"`
	MB      int    `json:"mb"`
	Percent int    `json:"percent"`
	At      int64  `json:"at"`
}

func progressPath(lay layout) string { return filepath.Join(lay.data, updateProgressFile) }

func writeProgress(lay layout, p updateProgress) {
	p.At = time.Now().Unix()
	b, _ := json.Marshal(p)
	_ = os.MkdirAll(lay.data, 0o755)
	tmp := progressPath(lay) + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, progressPath(lay))
	}
}

func readProgress(lay layout) (updateProgress, bool) {
	var p updateProgress
	b, err := os.ReadFile(progressPath(lay))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return p, false
	}
	// A download that stopped writing for two minutes is gone (the stick
	// was pulled, the computer slept).
	if p.Running && time.Since(time.Unix(p.At, 0)) > 2*time.Minute {
		p.Running, p.Done, p.Error = false, true, "the download stopped"
	}
	return p, true
}

func updateCheck(lay layout) sysReply {
	if lay.installed {
		return sysFail(errUnsupported)
	}
	latest, err := latestCommit()
	if err != nil {
		return sysFail(err)
	}
	cur := appCommit(lay.appDir())
	staged := appCommit(stagedAppDir(lay))
	return ok(sysReply{
		"kind":      "app",
		"current":   appVersion(lay.appDir()),
		"latest":    shortSHA(latest),
		"available": latest != cur && latest != staged,
		"staged":    latest == staged && staged != "",
		"canUpdate": true,
	})
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func updateStart(lay layout) sysReply {
	if lay.installed {
		return sysFail(errUnsupported)
	}
	if p, found := readProgress(lay); found && p.Running {
		return ok2()
	}
	exe, err := os.Executable()
	if err != nil {
		return sysFail(err)
	}
	writeProgress(lay, updateProgress{Running: true})
	if err := detach(exe, "update-stage"); err != nil {
		writeProgress(lay, updateProgress{Done: true, Error: err.Error()})
		return sysFail(err)
	}
	return ok2()
}

func ok2() sysReply { return ok(nil) }

func updateStatus(lay layout) sysReply {
	p, found := readProgress(lay)
	if !found {
		return ok(sysReply{"running": false})
	}
	return ok(sysReply{"running": p.Running, "progress": p})
}

// stageUpdate is "orOS update-stage": download orOS into app.next.
func stageUpdate(lay layout) {
	var mb int
	downloadProgress = func(n int64) {
		if m := int(n >> 20); m != mb {
			mb = m
			writeProgress(lay, updateProgress{Running: true, MB: mb})
		}
	}
	err := checkAppUpdate(lay)
	p := updateProgress{Done: true, MB: mb, Percent: 100}
	if err != nil {
		p.Error, p.Percent = err.Error(), 0
		log.Printf("update: %v", err)
	}
	writeProgress(lay, p)
}

// countingWriter reports how much was downloaded.
type countingWriter struct {
	n  int64
	fn func(int64)
}

func (c *countingWriter) Write(b []byte) (int, error) {
	c.n += int64(len(b))
	if c.fn != nil {
		c.fn(c.n)
	}
	return len(b), nil
}

// downloadProgress, when set, follows fetchApp's download.
var downloadProgress func(int64)

func progressReader(r io.Reader) io.Reader {
	if downloadProgress == nil {
		return r
	}
	return io.TeeReader(r, &countingWriter{fn: downloadProgress})
}

// maxWebReport bounds orOS's part of a report (it is a few KB).
const maxWebReport = 512 << 10

// writeSysReport saves the launcher's log and a few facts as a zip in the
// Downloads folder, for a message to whoever helps. web is orOS's own
// report from Settings (oros.txt), or "" from the System panel.
func writeSysReport(lay layout, dir, web string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Join(dir, "orOS-report-"+time.Now().Format("20060102-150405")+".zip")
	f, err := os.Create(name)
	if err != nil {
		return "", err
	}
	z := zip.NewWriter(f)
	info := fmt.Sprintf("orOS Portable %s\nsystem %s/%s\norOS %s (%s)\ninstalled %v\nwindow %+v\n",
		version, runtime.GOOS, runtime.GOARCH, appVersion(lay.appDir()), appCommit(lay.appDir()), lay.installed, loadSession(lay))
	if w, err := z.Create("about.txt"); err == nil {
		_, _ = io.WriteString(w, info)
	}
	if web != "" {
		if len(web) > maxWebReport {
			web = web[:maxWebReport] + "\n(cut)\n"
		}
		if w, err := z.Create("oros.txt"); err == nil {
			_, _ = io.WriteString(w, web)
		}
	}
	for _, n := range []string{"launcher.log", "system.log", sessionFile, updateProgressFile} {
		b, err := os.ReadFile(filepath.Join(lay.data, n))
		if err != nil {
			continue
		}
		if w, err := z.Create(n); err == nil {
			_, _ = w.Write(b)
		}
	}
	if err := z.Close(); err != nil {
		f.Close()
		return "", err
	}
	return name, f.Close()
}
