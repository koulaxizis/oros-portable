package main

import (
	"os"
	"path/filepath"
)

// Chromium keeps "lockfile" open without sharing while a profile is in
// use, so it cannot be removed then. A leftover from a crash can.
func profileInUse(profile string) bool {
	lock := filepath.Join(profile, "lockfile")
	if _, err := os.Stat(lock); err != nil {
		return false
	}
	return os.Remove(lock) != nil
}
