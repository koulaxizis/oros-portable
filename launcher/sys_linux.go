//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
)

// Linux: sound through PipeWire (wpctl) or PulseAudio (pactl), the
// backlight through logind (no root needed), battery from sysfs, power
// through systemd.

type linuxOps struct{ sysfs string }

func hostPlatform() platformOps { return linuxOps{sysfs: "/sys"} }

func cmdOut(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return string(out), err
}

var (
	wpVolume = regexp.MustCompile(`Volume:\s*([0-9.]+)`)
	paVolume = regexp.MustCompile(`(\d+)%`)
)

func (linuxOps) volume() (int, bool, bool) {
	if out, err := cmdOut("wpctl", "get-volume", "@DEFAULT_AUDIO_SINK@"); err == nil {
		if m := wpVolume.FindStringSubmatch(out); m != nil {
			f, _ := strconv.ParseFloat(m[1], 64)
			return clamp(int(f*100+0.5), 0, 100), strings.Contains(out, "MUTED"), true
		}
	}
	if out, err := cmdOut("pactl", "get-sink-volume", "@DEFAULT_SINK@"); err == nil {
		if m := paVolume.FindStringSubmatch(out); m != nil {
			v, _ := strconv.Atoi(m[1])
			mute, _ := cmdOut("pactl", "get-sink-mute", "@DEFAULT_SINK@")
			return clamp(v, 0, 100), strings.Contains(mute, "yes"), true
		}
	}
	return 0, false, false
}

func (linuxOps) setVolume(v int) error {
	if _, err := exec.LookPath("wpctl"); err == nil {
		return exec.Command("wpctl", "set-volume", "@DEFAULT_AUDIO_SINK@", fmt.Sprintf("%.2f", float64(v)/100)).Run()
	}
	return exec.Command("pactl", "set-sink-volume", "@DEFAULT_SINK@", fmt.Sprintf("%d%%", v)).Run()
}

func (linuxOps) setMute(on bool) error {
	n := map[bool]string{true: "1", false: "0"}[on]
	if _, err := exec.LookPath("wpctl"); err == nil {
		return exec.Command("wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", n).Run()
	}
	return exec.Command("pactl", "set-sink-mute", "@DEFAULT_SINK@", n).Run()
}

func readInt(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return n, err == nil
}

func (o linuxOps) battery() (int, bool, bool) {
	list, _ := filepath.Glob(filepath.Join(o.sysfs, "class", "power_supply", "BAT*"))
	sort.Strings(list)
	for _, d := range list {
		pc, ok := readInt(filepath.Join(d, "capacity"))
		if !ok {
			continue
		}
		st, _ := os.ReadFile(filepath.Join(d, "status"))
		s := strings.TrimSpace(string(st))
		return clamp(pc, 0, 100), s == "Charging" || s == "Full", true
	}
	return 0, false, false
}

// backlight is the first backlight device: its name and the two values.
func (o linuxOps) backlight() (name string, cur, max int, ok bool) {
	list, _ := filepath.Glob(filepath.Join(o.sysfs, "class", "backlight", "*"))
	sort.Strings(list)
	for _, d := range list {
		c, ok1 := readInt(filepath.Join(d, "brightness"))
		m, ok2 := readInt(filepath.Join(d, "max_brightness"))
		if ok1 && ok2 && m > 0 {
			return filepath.Base(d), c, m, true
		}
	}
	return "", 0, 0, false
}

func (o linuxOps) brightness() (int, bool) {
	_, c, m, ok := o.backlight()
	if !ok {
		return 0, false
	}
	return clamp((c*100+m/2)/m, 0, 100), true
}

// setBrightness asks logind, which lets the user of the active session
// set the backlight without root.
func (o linuxOps) setBrightness(v int) error {
	name, _, m, ok := o.backlight()
	if !ok {
		return errors.New("this screen has no brightness control")
	}
	conn, err := dbus.SystemBus()
	if err != nil {
		return err
	}
	obj := conn.Object("org.freedesktop.login1", "/org/freedesktop/login1/session/auto")
	return obj.Call("org.freedesktop.login1.Session.SetBrightness", 0, "backlight", name, uint32(v*m/100)).Err
}

func (linuxOps) power(action string) error {
	verb := map[string]string{"off": "poweroff", "reboot": "reboot", "sleep": "suspend"}[action]
	if verb == "" {
		return errors.New("unknown action")
	}
	return exec.Command("systemctl", verb).Run()
}

func (linuxOps) downloads() string {
	if out, err := cmdOut("xdg-user-dir", "DOWNLOAD"); err == nil {
		if d := strings.TrimSpace(out); d != "" {
			if home, _ := os.UserHomeDir(); d != home {
				return d
			}
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

// registerHost puts the native messaging manifest into the profile, where
// Chromium on Linux looks for it (<user-data-dir>/NativeMessagingHosts).
// Nothing outside the profile changes, so there is nothing to undo.
func registerHost(lay layout, profile, exe string) (func(), error) {
	dir := filepath.Join(profile, "NativeMessagingHosts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return func() {}, err
	}
	return func() {}, os.WriteFile(filepath.Join(dir, hostName+".json"), hostManifest(exe), 0o644)
}

// detach starts a program that keeps running after the native host exits
// (Chromium ends the host's process group).
func detach(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// killTree ends a browser the watchdog gave up on.
func killTree(p *os.Process) {
	_ = p.Signal(syscall.SIGTERM)
	time.Sleep(3 * time.Second)
	_ = p.Kill()
}
