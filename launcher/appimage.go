package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// An AppImage is one file that the user may keep anywhere, so it adds
// itself to the desktop's application menu on every start (the path may
// have changed since the last one). "remove-menu" takes the entry away.
// AppImage sets $APPIMAGE to the image's own path.

func appImageMenuFiles() (desktop, icon string) {
	share := os.Getenv("XDG_DATA_HOME")
	if share == "" {
		home, _ := os.UserHomeDir()
		share = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(share, "applications", "oros-appimage.desktop"),
		filepath.Join(share, "icons", "hicolor", "512x512", "apps", "oros-appimage.png")
}

func integrateAppImage(lay layout) {
	img := os.Getenv("APPIMAGE")
	if img == "" || strings.ContainsAny(img, "\r\n") {
		return
	}
	// $APPDIR is where the image is mounted; the launcher sits in usr/lib/oros.
	appDir := os.Getenv("APPDIR")
	if appDir == "" {
		appDir = filepath.Join(lay.root, "..", "..", "..")
	}
	desktop, icon := appImageMenuFiles()
	if err := writeAppImageMenu(img, filepath.Join(appDir, "oros.png"), desktop, icon); err != nil {
		log.Printf("menu entry: %v", err)
	}
}

func writeAppImageMenu(img, iconSrc, desktop, icon string) error {
	if b, err := os.ReadFile(iconSrc); err == nil {
		if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(icon, b, 0o644); err != nil {
			return err
		}
	}
	q := desktopQuote(img)
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=orOS
Comment=Notes, calendar, files, office and games, offline and private
Comment[el]=Σημειώσεις, ημερολόγιο, αρχεία, γραφείο και παιχνίδια, χωρίς σύνδεση και ιδιωτικά
Exec=%s
Icon=%s
Terminal=false
Categories=Office;
Actions=quick;
X-AppImage-Path=%s

[Desktop Action quick]
Name=Add or search
Name[el]=Προσθήκη ή αναζήτηση
Name[de]=Hinzufügen oder suchen
Exec=%s quick
`, q, icon, img, q)
	if old, err := os.ReadFile(desktop); err == nil && string(old) == entry {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(desktop), 0o755); err != nil {
		return err
	}
	return os.WriteFile(desktop, []byte(entry), 0o644)
}

// desktopQuote quotes a path for a Desktop Entry's Exec line: double
// quotes with \, ", ` and $ escaped, then escaped again as a string value.
func desktopQuote(path string) string {
	return `"` + strings.NewReplacer(`\`, `\\\\`, `"`, `\\"`, "`", "\\\\`", `$`, `\\$`).Replace(path) + `"`
}

func removeAppImageMenu() {
	_ = setAutostart(false, "") // the tray at login goes with the menu entry
	desktop, icon := appImageMenuFiles()
	_ = os.Remove(desktop)
	_ = os.Remove(icon)
}
