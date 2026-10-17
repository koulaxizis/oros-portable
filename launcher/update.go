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

// updateURL is a variable so tests can point it at a local server.
var updateURL = "https://github.com/koulaxizis/oros/archive/refs/heads/main.zip"

// commitFile holds the orOS commit an app folder was built from. GitHub
// puts that commit in the comment of its archives.
const commitFile = ".oros-commit"

// Folders of the orOS repo that are not part of the web app.
var skipDirs = map[string]bool{
	".github": true, "android": true, "relay": true, "extension": true, "tests": true,
}

// update downloads the latest orOS, unpacks it next to app/ and swaps the
// folders, keeping the previous version as app.old. The service worker
// notices the new sw.js on the next start and refreshes its cache.
func update(root string) error {
	next := filepath.Join(root, "app.new")
	if err := fetchApp(root, next); err != nil {
		return err
	}
	return swapApp(root, next)
}

// fetchApp downloads the latest orOS and unpacks it into dest, replacing
// whatever was there.
func fetchApp(root, dest string) error {
	tmp, err := os.CreateTemp(root, "oros-update-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	log.Printf("downloading %s", updateURL)
	c := http.Client{Timeout: 10 * time.Minute}
	r, err := c.Get(updateURL)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return fmt.Errorf("download returned %s", r.Status)
	}
	if _, err := io.Copy(tmp, r.Body); err != nil {
		return err
	}

	_ = os.RemoveAll(dest)
	if err := unpackApp(tmp.Name(), dest); err != nil {
		_ = os.RemoveAll(dest)
		return err
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
		if !f.Mode().IsRegular() {
			continue
		}
		if err := writeFile(f, target); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(destAbs, "index.html")); err != nil {
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
	if _, err := io.Copy(dst, src); err != nil {
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
