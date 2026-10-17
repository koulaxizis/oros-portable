//go:build !windows

package main

import (
	"log"
	"os"
	"os/exec"
)

func notify(msg string) {
	log.Print(msg)
	desktopMessage("info", msg)
}

func fail(msg string) {
	log.Print(msg)
	desktopMessage("error", msg)
	os.Exit(1)
}

// desktopMessage shows msg in a dialog, because a launcher started from a
// file manager has no terminal to print to. It tries the common dialog
// tools and stays quiet when none is installed or there is no display.
func desktopMessage(kind, msg string) {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return
	}
	tries := [][]string{
		{"zenity", "--" + kind, "--title=" + appTitle, "--text=" + msg},
		{"kdialog", "--title", appTitle, map[string]string{"error": "--error", "info": "--msgbox"}[kind], msg},
		{"notify-send", appTitle, msg},
	}
	for _, t := range tries {
		if p, err := exec.LookPath(t[0]); err == nil {
			_ = exec.Command(p, t[1:]...).Run()
			return
		}
	}
}

// ask shows a yes/no question and reports whether the answer was yes.
// Without a dialog tool or a display the answer is no.
func ask(msg string) bool {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	for _, t := range [][]string{
		{"zenity", "--question", "--title=" + appTitle, "--text=" + msg},
		{"kdialog", "--title", appTitle, "--yesno", msg},
	} {
		if p, err := exec.LookPath(t[0]); err == nil {
			return exec.Command(p, t[1:]...).Run() == nil
		}
	}
	return false
}
