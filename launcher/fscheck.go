package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// profileDirUsable reports whether Chromium can keep its profile in dir.
// Outside Windows, Chromium locks a profile with symbolic links
// (SingletonLock); exFAT and FAT cannot store them, so the browser would
// quit at once with nothing on screen.
func profileDirUsable(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dir, ".oros-link-test")
	_ = os.Remove(p)
	if err := os.Symlink("oros", p); err != nil {
		return err
	}
	return os.Remove(p)
}
