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
		{"zenity", "--" + kind, "--title=orOS Portable", "--text=" + msg},
		{"kdialog", "--title", "orOS Portable", map[string]string{"error": "--error", "info": "--msgbox"}[kind], msg},
		{"notify-send", "orOS Portable", msg},
	}
	for _, t := range tries {
		if p, err := exec.LookPath(t[0]); err == nil {
			_ = exec.Command(p, t[1:]...).Run()
			return
		}
	}
}
