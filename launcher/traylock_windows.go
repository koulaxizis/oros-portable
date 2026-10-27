package main

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// lockTray keeps one tray per user: an exclusive lock on
// <data>\tray.lock, which Windows drops when the process ends.
func lockTray(data string) (func(), bool) {
	if err := os.MkdirAll(data, 0o755); err != nil {
		return func() {}, true
	}
	f, err := os.OpenFile(filepath.Join(data, "tray.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return func() {}, true
	}
	ol := new(windows.Overlapped)
	h := windows.Handle(f.Fd())
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol); err != nil {
		f.Close()
		return nil, false
	}
	return func() { _ = windows.UnlockFileEx(h, 0, 1, 0, ol); f.Close() }, true
}
