package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// updateURL and archiveURL are variables so tests can point them at a
// local server. archiveURL (%s is the commit) is what every update uses
// when it knows the commit GitHub named as newest: the archive of that
// exact commit, whose zip comment must then name the same commit
// (fetchApp). updateURL (main) is left for an update that could not ask.
var (
	updateURL  = "https://github.com/koulaxizis/oros/archive/refs/heads/main.zip"
	archiveURL = "https://github.com/koulaxizis/oros/archive/%s.zip"
)

// maxAppBytes bounds what one archive may unpack to (orOS is about 200 MB).
const maxAppBytes = 1 << 30

// commitFile holds the orOS commit an app folder was built from. GitHub
// puts that commit in the comment of its archives.
const commitFile = ".oros-commit"

// Folders of the orOS repo that are not part of the web app.
var skipDirs = map[string]bool{
	".github": true, "android": true, "relay": true, "extension": true, "tests": true,
}

// update downloads orOS (the given commit, or main when commit is ""),
// unpacks it next to app/ and swaps the folders, keeping the previous
// version as app.old. The service worker notices the new sw.js on the
// next start and refreshes its cache.
func update(root, commit string) error {
	next := filepath.Join(root, "app.new")
	if err := fetchApp(root, next, commit); err != nil {
		return err
	}
	return swapApp(root, next)
}

// fetchApp downloads orOS and unpacks it into dest, replacing whatever was
// there. With a commit it downloads that commit's archive and keeps it
// only when the archive names the same commit; without one, main.
// Nothing in it is ever run by the launcher: it holds the web app the
// browser shows, the same files useoros.online serves.
func fetchApp(root, dest, commit string) error {
	tmp, err := os.CreateTemp(root, "oros-update-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	src := updateURL
	if commit != "" {
		if !isCommit(commit) {
			return errors.New("not a commit: " + commit)
		}
		src = fmt.Sprintf(archiveURL, commit)
	}
	log.Printf("downloading %s", src)
	c := http.Client{Timeout: 10 * time.Minute}
	r, err := c.Get(src)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("download returned %s", r.Status)
	}
	if _, err := io.Copy(tmp, progressReader(r.Body)); err != nil {
		return err
	}

	_ = os.RemoveAll(dest)
	if err := unpackApp(tmp.Name(), dest); err != nil {
		_ = os.RemoveAll(dest)
		return err
	}
	if commit != "" && appCommit(dest) != commit {
		_ = os.RemoveAll(dest)
		return fmt.Errorf("the download is not orOS %s", shortSHA(commit))
	}
	return nil
}

func unpackApp(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	var total uint64
	for _, f := range zr.File {
		// GitHub archives wrap everything in "<repo>-<branch>/".
		parts := strings.SplitN(f.Name, "/", 2)
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		rel := parts[1]
		if skipDirs[strings.SplitN(rel, "/", 2)[0]] {
			continue
		}
		target := filepath.Join(destAbs, filepath.FromSlash(rel))
		if !strings.HasPrefix(target, destAbs+string(os.PathSeparator)) {
			return errors.New("unsafe path in archive: " + f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		// Regular files only, written without execute bits: links and
		// anything else in the archive are left out.
		if !f.Mode().IsRegular() {
			continue
		}
		if total += f.UncompressedSize64; total > maxAppBytes {
			return errors.New("the download is too large for orOS")
		}
		if err := writeFile(f, target); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(destAbs, "index.html")); err != nil || appVersion(destAbs) == "" {
		return errors.New("the download does not look like orOS")
	}
	if isCommit(zr.Comment) {
		_ = os.WriteFile(filepath.Join(destAbs, commitFile), []byte(zr.Comment+"\n"), 0o644)
	}
	return nil
}

func isCommit(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

// appCommit is the orOS commit an app folder came from, or "".
func appCommit(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, commitFile))
	if err != nil {
		return ""
	}
	if s := strings.TrimSpace(string(b)); isCommit(s) {
		return s
	}
	return ""
}

func writeFile(f *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, io.LimitReader(src, int64(f.UncompressedSize64))); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

func swapApp(root, next string) error {
	cur := filepath.Join(root, "app")
	old := filepath.Join(root, "app.old")
	_ = os.RemoveAll(old)
	if _, err := os.Stat(cur); err == nil {
		if err := os.Rename(cur, old); err != nil {
			return err
		}
	}
	if err := os.Rename(next, cur); err != nil {
		_ = os.Rename(old, cur)
		return err
	}
	log.Printf("orOS updated; previous version kept in app.old")
	return nil
}
