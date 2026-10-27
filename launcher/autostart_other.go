//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
)

// autostartFile is the freedesktop autostart entry (GNOME, KDE, Xfce,
// Cinnamon, MATE all read it).
func autostartFile() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "autostart", "oros-tray.desktop"), nil
}

// autostartEntry starts "<exe> tray" at login.
func autostartEntry(exe string) string {
	return "[Desktop Entry]\nType=Application\nName=orOS\nComment=orOS in the tray: add or search\n" +
		"Comment[el]=Το orOS στο tray: προσθήκη ή αναζήτηση\n" +
		"Exec=" + desktopQuote(exe) + " tray\nIcon=oros\nTerminal=false\nX-GNOME-Autostart-enabled=true\n"
}

// setAutostart starts the tray at login, or stops doing so.
func setAutostart(on bool, exe string) error {
	p, err := autostartFile()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if exe == "" {
		return errors.New("no program path")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(autostartEntry(exe)), 0o644)
}

// autostartOn reports whether the entry exists.
func autostartOn() bool {
	p, err := autostartFile()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}
