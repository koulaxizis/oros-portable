package main

// Claude desktop (CLAUDE-1 wave C3): the System extension's "Claude
// desktop" section and orOS's Settings › Connected services › Claude, as
// on orOS USB (oros-usb system/claude.go). orOS Portable never carries the
// app (Anthropic's proprietary software): it comes from Anthropic only
// when the person asks, and goes onto this computer, not the stick,
// because it is the computer's app and its account:
//   - Windows: winget installs Anthropic's package (Anthropic.Claude),
//     checked against the hash in winget's manifest (claude_windows.go).
//   - Linux (Debian and Ubuntu, as Anthropic supports): Anthropic's apt
//     repository read like apt, signature pinned by fingerprint, the .deb
//     unpacked into ~/.local/share/oros-claude without root
//     (claude_other.go).
// The guest gets none. "orOS claude --fetch" and "orOS claude --open" do
// the work in a launcher of their own, because Chromium ends the host as
// soon as it has answered; both place Claude beside orOS (claude_side.go).

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

const claudeProgress = "claude.json"

func claudeProgressFile(lay layout) string { return filepath.Join(lay.userFolder(), claudeProgress) }
func claudeLock(lay layout) string         { return filepath.Join(lay.userFolder(), "claude.lock") }

// claudeAvailable: offered to every user but the guest, where this
// computer can run it (claudePlatformOK).
func claudeAvailable(lay layout) bool {
	if os.Getenv("OROS_NO_CLAUDE") != "" {
		return false
	}
	return lay.userFolder() != guestDir(lay.data) && claudePlatformOK()
}

// claudeSelf runs the launcher detached; tests replace it.
var claudeSelf = func(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return detach(exe, args...)
}

func claudeHost(lay layout, cmd string) sysReply {
	if !claudeAvailable(lay) {
		return sysFail(errUnsupported)
	}
	switch cmd {
	case "claude.status":
		r := sysReply{"running": held(claudeLock(lay)), "persistent": true, "supported": true}
		v := claudeVersion()
		r["installed"] = v != ""
		if v != "" {
			r["version"] = v
		}
		if b, err := os.ReadFile(claudeProgressFile(lay)); err == nil {
			var p torState
			if json.Unmarshal(b, &p) == nil {
				r["progress"] = p
			}
		}
		return ok(r)
	case "claude.install":
		if held(claudeLock(lay)) {
			return ok(sysReply{"running": true})
		}
		_ = os.Remove(claudeProgressFile(lay))
		torWriter(claudeProgressFile(lay))(torState{Step: "index"})
		if err := claudeSelf("claude", "--fetch", "--progress", claudeProgressFile(lay), "--lock", claudeLock(lay)); err != nil {
			return sysFail(err)
		}
		for i := 0; i < 20 && !held(claudeLock(lay)); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		return ok(sysReply{"running": true})
	case "claude.open":
		if claudeVersion() == "" {
			return sysFail(errors.New("Claude desktop is not installed"))
		}
		// A launcher of its own opens it and places it beside orOS, since
		// this host ends as soon as it has answered.
		if err := claudeSelf("claude", "--open"); err != nil {
			return sysFail(err)
		}
		return ok(sysReply{"started": true})
	case "claude.remove":
		if held(claudeLock(lay)) {
			return sysFail(errors.New("Claude desktop is being downloaded"))
		}
		if err := claudeUninstall(); err != nil {
			return sysFail(err)
		}
		_ = os.Remove(claudeProgressFile(lay))
		return ok(nil)
	}
	return sysFail(errUnsupported)
}

// claudeMain is "orOS claude --fetch --progress F --lock L".
func claudeMain(lay layout, args []string) int {
	fl := flag.NewFlagSet("claude", flag.ContinueOnError)
	var fetch, open, noStart bool
	progress := claudeProgressFile(lay)
	lock := claudeLock(lay)
	fl.BoolVar(&fetch, "fetch", false, "download and install Anthropic's Claude desktop")
	fl.BoolVar(&open, "open", false, "start Claude desktop beside orOS")
	fl.BoolVar(&noStart, "no-start", false, "do not start Claude desktop afterwards")
	fl.StringVar(&progress, "progress", progress, "progress file (JSON)")
	fl.StringVar(&lock, "lock", lock, "lock file held while downloading")
	if err := fl.Parse(args); err != nil || fetch == open {
		fmt.Fprintln(os.Stderr, "usage: orOS claude --fetch [--no-start] | --open")
		return 2
	}
	if open {
		if err := claudeStart(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		claudeBeside(lay)
		return 0
	}
	unlock, okLock := lockFile(lock)
	if !okLock {
		fmt.Fprintln(os.Stderr, "Claude desktop is already being installed")
		return 1
	}
	defer unlock()
	report := torWriter(progress)
	err := claudeFetch(func(step string, pct int) { report(torState{Step: step, Percent: pct}) })
	if err == nil && !noStart {
		err = claudeStart()
	}
	if err != nil {
		report(torState{Step: "error", Done: true, Error: err.Error()})
		log.Printf("claude: %v", err)
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report(torState{Step: "done", Percent: 100, Done: true})
	if !noStart {
		claudeBeside(lay)
	}
	return 0
}
