//go:build !windows

package main

import "errors"

const webWinSupported = false

func launchWebWindow(lay layout, url string, extra ...string) error {
	return errors.New("orOS's own window needs Windows (WebView2)")
}
