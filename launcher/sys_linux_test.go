//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxBatteryAndBacklightFromSysfs(t *testing.T) {
	sys := t.TempDir()
	o := linuxOps{sysfs: sys}
	if _, _, ok := o.battery(); ok {
		t.Fatal("a desktop without a battery reported one")
	}
	if _, ok := o.brightness(); ok {
		t.Fatal("a screen without a backlight reported one")
	}
	put := func(p, v string) {
		t.Helper()
		f := filepath.Join(sys, p)
		must(t, os.MkdirAll(filepath.Dir(f), 0o755))
		must(t, os.WriteFile(f, []byte(v+"\n"), 0o644))
	}
	put("class/power_supply/AC/online", "1")
	put("class/power_supply/BAT0/capacity", "81")
	put("class/power_supply/BAT0/status", "Charging")
	put("class/backlight/intel_backlight/brightness", "600")
	put("class/backlight/intel_backlight/max_brightness", "1200")
	if pc, ch, ok := o.battery(); !ok || pc != 81 || !ch {
		t.Fatalf("battery %d %v %v", pc, ch, ok)
	}
	if b, ok := o.brightness(); !ok || b != 50 {
		t.Fatalf("brightness %d %v", b, ok)
	}
	put("class/power_supply/BAT0/status", "Discharging")
	if _, ch, _ := o.battery(); ch {
		t.Fatal("discharging reported as charging")
	}
}
