package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// The white-screen watchdog, as on orOS USB: once orOS has painted, the
// System extension pings every 10 s (syshost.go touches data/ping). A main
// window that gives no sign of life within watchdogWait is ended, and
// launchBrowser starts it once more without the GPU.
//
// It is armed only on a stick (or computer) where the extension has
// pinged before: a browser that cannot load the extension must never
// lose its window to the watchdog.

var watchdogWait = 90 * time.Second

// runWatched runs the browser and waits for it. gaveUp: the watchdog
// ended it.
func runWatched(lay layout, c candidate, url string, cmd *exec.Cmd, watch bool) (gaveUp bool, err error) {
	ping := filepath.Join(lay.data, pingFile)
	_, seen := os.Stat(ping)
	if !watch || url != appURL || sysBrowserArgs(lay, c, url) == nil || seen != nil {
		return false, cmd.Run()
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return false, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	deadline := time.After(watchdogWait)
	for {
		select {
		case err := <-done:
			return false, err // closed, or handed over to a running browser
		case <-tick.C:
			if st, err := os.Stat(ping); err == nil && st.ModTime().After(start.Add(-2*time.Second)) {
				return false, <-done // alive: wait for the window to close
			}
		case <-deadline:
			log.Printf("watchdog: no sign of life from orOS for %v, starting again without the GPU", watchdogWait)
			killTree(cmd.Process)
			<-done
			return true, nil
		}
	}
}
