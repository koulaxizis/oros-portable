//go:build windows

package main

import (
	"errors"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"

	webview2 "github.com/jchv/go-webview2"
)

const webWinSupported = true

// WebView2 runs its window on the thread that made it: the main goroutine
// stays on the main thread (as for the tray icon).
func init() { runtime.LockOSThread() }

// launchWebWindow shows url in orOS's own WebView2 window and returns when
// it closes. No WebView2 runtime (old Windows 10): an error, and the
// caller opens the browser window instead.
func launchWebWindow(lay layout, url string, extra ...string) error {
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  filepath.Join(lay.data, webWinDir),
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "orOS",
			Width:  1280,
			Height: 800,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		return errors.New("WebView2 is not installed on this computer")
	}
	defer w.Destroy()
	_ = w.Bind("orosOpenExternal", func(u string) {
		if !externalOK(u) {
			log.Printf("own window: refused to open %q", u)
			return
		}
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	})
	w.Init(webWinJS)
	w.Navigate(url)
	log.Printf("own window: open (data %s)", filepath.Join(lay.data, webWinDir))
	w.Run()
	log.Printf("own window: closed")
	return nil
}
