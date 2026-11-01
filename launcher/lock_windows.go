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

// closeProfile: Windows' lock file names no process, so a signed-out
// user's Internet window stays until it is closed (README, Users).
func closeProfile(profile string) {}
