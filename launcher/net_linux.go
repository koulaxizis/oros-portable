//go:build linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Linux: Wi-Fi through NetworkManager (nmcli), as on orOS USB. Most desktop
// systems let the user of the active session change networks without a
// password prompt (polkit); where they do not, nmcli's error is shown.

type linuxNet struct{}

func hostNet() netOps { return linuxNet{} }

// nmcli runs nmcli in the C locale, so its output can be read.
var nmcli = func(args ...string) (string, error) {
	cmd := exec.Command("nmcli", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (linuxNet) wifi() wifiState {
	var w wifiState
	dev, err := nmcli("-t", "-f", "TYPE,STATE", "device")
	if err != nil {
		return w
	}
	for _, l := range strings.Split(dev, "\n") {
		f := splitTerse(l)
		if len(f) < 2 {
			continue
		}
		switch {
		case f[0] == "wifi":
			w.Present = true
		case f[0] == "ethernet" && f[1] == "connected":
			w.Wired = true
		}
	}
	if !w.Present {
		return w
	}
	if out, err := nmcli("-t", "-f", "WIFI", "radio"); err == nil {
		w.On = strings.TrimSpace(out) == "enabled"
	}
	if out, err := nmcli("-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY", "device", "wifi", "list", "--rescan", "no"); err == nil {
		for _, n := range parseNmcliList(out) {
			if n.InUse {
				n := n
				w.Current = &n
			}
		}
	}
	return w
}

func (linuxNet) wifiScan() ([]network, error) {
	out, err := nmcli("-t", "-f", "IN-USE,SSID,SIGNAL,SECURITY", "device", "wifi", "list", "--rescan", "yes")
	if err != nil {
		return nil, nmError(out, err)
	}
	return parseNmcliList(out), nil
}

func (linuxNet) wifiConnect(ssid, password string) error {
	args := []string{"--wait", "30", "device", "wifi", "connect", ssid}
	if password != "" {
		args = append(args, "password", password)
	}
	out, err := nmcli(args...)
	if err != nil {
		return nmError(out, err)
	}
	return nil
}

func (linuxNet) wifiRadio(on bool) error {
	state := map[bool]string{true: "on", false: "off"}[on]
	if out, err := nmcli("radio", "wifi", state); err != nil {
		return nmError(out, err)
	}
	return nil
}

// nmError is nmcli's own message (its last line), never the command line,
// which may hold the password.
func nmError(out string, err error) error {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		return errors.New(strings.TrimPrefix(l, "Error: "))
	}
	return err
}

// splitTerse splits one line of "nmcli -t" output, where ':' inside a
// value is written as "\:".
func splitTerse(line string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) {
			i++
			cur.WriteByte(line[i])
			continue
		}
		if c == ':' {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	return append(out, cur.String())
}

func parseNmcliList(out string) []network {
	var list []network
	for _, line := range strings.Split(out, "\n") {
		f := splitTerse(line)
		if len(f) < 4 || f[1] == "" {
			continue
		}
		sig, _ := strconv.Atoi(f[2])
		list = append(list, network{SSID: f[1], Signal: sig, Secure: f[3] != "" && f[3] != "--", InUse: f[0] == "*"})
	}
	return dedupeNetworks(list)
}

// btApps are the Bluetooth settings of the common desktops, first found
// wins.
var btApps = [][]string{
	{"gnome-control-center", "bluetooth"},
	{"systemsettings", "kcm_bluetooth"},
	{"cinnamon-settings", "bluetooth"},
	{"blueman-manager"},
	{"blueberry"},
	{"xfce4-settings-manager"},
}

func btApp() []string {
	for _, a := range btApps {
		if _, err := exec.LookPath(a[0]); err == nil {
			return a
		}
	}
	return nil
}

func (linuxNet) hasBtSettings() bool { return btApp() != nil }

func (linuxNet) btSettings() error {
	a := btApp()
	if a == nil {
		return errUnsupported
	}
	return detach(a[0], a[1:]...)
}
