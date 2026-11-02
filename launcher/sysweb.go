package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The Internet window (System panel, "Internet" button next to it): a
// normal window of the bundled browser, with tabs and an address bar, for
// the web outside orOS, as on orOS USB. It opens in orOS's own profile
// (<user folder>/profile, browserCandidates), so it joins the browser that
// shows orOS: the orOS add-on there (addon/, from oros-usb's
// scripts/addon.sh) fills and saves Passwords logins and reaches the orOS
// window to do it, and uBlock Origin Lite (ubol/, from oros-usb's
// scripts/ubol.sh) blocks ads. Sites share the browser's storage with
// orOS, as in any browser with orOS in a tab. Up to orOS Portable 0.6.0 it
// had its own profile (data/web), which is no longer used. Closing it
// leaves orOS as it was.

const (
	webHome    = "https://duckduckgo.com/"
	webProfile = "profile" // the orOS window's, browserCandidates
	addonDir   = "addon"
	ubolDir    = "ubol"
)

// webBrowser is the bundled browser, or "" (the lite edition: an installed
// browser has its own windows already).
func webBrowser(lay layout) string {
	if b := os.Getenv("OROS_PORTABLE_BROWSER"); b != "" {
		return b
	}
	if l := bundledBrowsers(lay.root); len(l) > 0 {
		return l[0]
	}
	return ""
}

// webArgs: when orOS's window is open (the button is in it) the browser
// hands the address to it and ignores the other flags; they matter only
// if this window is the first on the profile.
func webArgs(lay layout) []string {
	args := []string{
		"--user-data-dir=" + filepath.Join(lay.userFolder(), webProfile),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate",
		"--new-window",
	}
	if ext := bundledExtensions(lay); len(ext) > 0 {
		args = append(args, "--load-extension="+strings.Join(ext, ","))
	}
	args = joinExtensions(append(args, extraArgs()...)) // OROS_PORTABLE_BROWSER_ARGS, as for orOS's window
	return append(args, webHome)
}

// webStart runs the browser; tests replace it.
var webStart = func(name string, args ...string) error { return detach(name, args...) }

func openWeb(lay layout) sysReply {
	b := webBrowser(lay)
	if b == "" {
		return sysFail(errors.New("this edition has no browser of its own"))
	}
	if err := os.MkdirAll(filepath.Join(lay.userFolder(), webProfile), 0o700); err != nil {
		return sysFail(err)
	}
	if err := webStart(b, webArgs(lay)...); err != nil {
		return sysFail(err)
	}
	return ok(nil)
}
