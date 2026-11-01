//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
)

// lockTray keeps one tray per user: an flock on <data>/tray.lock, which
// the system drops when the process ends.
func lockTray(data string) (func(), bool) {
	if err := os.MkdirAll(data, 0o755); err != nil {
		return func() {}, true
	}
	return lockFile(filepath.Join(data, "tray.lock"))
}

// lockFile takes an flock on path without waiting (also Tor's locks,
// tor.go). A file that cannot be opened counts as not locked.
func lockFile(path string) (func(), bool) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}, true
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, false
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, true
}
