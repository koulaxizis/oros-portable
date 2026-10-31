package main

import (
	"errors"
	"os"
	"path/filepath"
)

// The Internet window (System panel, "Internet" button next to it): a
// normal window of the bundled browser, with tabs and an address bar, for
// the web outside orOS, as on orOS USB. It has its own profile (data/web),
// so sites never share storage with orOS, and loads uBlock Origin Lite
// when the build put it next to the launcher (ubol/, from oros-usb's
// scripts/ubol.sh). Closing it leaves orOS as it was.

const (
	webHome   = "https://duckduckgo.com/"
	webFolder = "web"
	ubolDir   = "ubol"
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

func webArgs(lay layout) []string {
	args := []string{
		"--user-data-dir=" + filepath.Join(lay.data, webFolder),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate",
		"--new-window",
	}
	if _, err := os.Stat(filepath.Join(lay.root, ubolDir, "manifest.json")); err == nil {
		args = append(args, "--load-extension="+filepath.Join(lay.root, ubolDir))
	}
	return append(args, webHome)
}

// webStart runs the browser; tests replace it.
var webStart = func(name string, args ...string) error { return detach(name, args...) }

func openWeb(lay layout) sysReply {
	b := webBrowser(lay)
	if b == "" {
		return sysFail(errors.New("this edition has no browser of its own"))
	}
	if err := os.MkdirAll(filepath.Join(lay.data, webFolder), 0o700); err != nil {
		return sysFail(err)
	}
	if err := webStart(b, webArgs(lay)...); err != nil {
		return sysFail(err)
	}
	return ok(nil)
}
