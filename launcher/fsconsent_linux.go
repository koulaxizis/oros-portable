//go:build linux

package main

import (
	"os"
	"strings"
)

// computerID is systemd's machine ID (D-Bus's on older systems).
func computerID() string {
	for _, f := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(f); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	return ""
}
