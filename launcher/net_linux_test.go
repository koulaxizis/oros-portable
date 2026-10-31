//go:build linux

package main

import (
	"errors"
	"strings"
	"testing"
)

func TestNmcli(t *testing.T) {
	var calls []string
	answers := map[string]string{
		"-t -f TYPE,STATE device": "ethernet:connected\nwifi:connected\nloopback:connected (externally)\n",
		"-t -f WIFI radio":        "enabled\n",
		"-t -f IN-USE,SSID,SIGNAL,SECURITY device wifi list --rescan no": "*:Home\\:Net:72:WPA2\n:Cafe:40:\n:Cafe:60:--\n::90:WPA2\n",
	}
	old := nmcli
	nmcli = func(args ...string) (string, error) {
		k := strings.Join(args, " ")
		calls = append(calls, k)
		if strings.Contains(k, "connect") {
			return "Error: Connection activation failed: Secrets were required, but not provided.\n", errors.New("exit status 4")
		}
		return answers[k], nil
	}
	defer func() { nmcli = old }()
	w := linuxNet{}.wifi()
	if !w.Present || !w.On || !w.Wired || w.Current == nil || w.Current.SSID != "Home:Net" || !w.Current.Secure {
		t.Fatalf("%+v %+v", w, w.Current)
	}
	list := parseNmcliList(answers["-t -f IN-USE,SSID,SIGNAL,SECURITY device wifi list --rescan no"])
	if len(list) != 2 || list[1].Signal != 60 || list[1].Secure {
		t.Fatalf("%+v", list)
	}
	err := linuxNet{}.wifiConnect("Cafe", "hunter22")
	if err == nil || err.Error() != "Connection activation failed: Secrets were required, but not provided." || strings.Contains(err.Error(), "hunter22") {
		t.Fatalf("error: %v", err)
	}
	nmcli = func(args ...string) (string, error) { return "", nil }
	if w := (linuxNet{}).wifi(); w.Present {
		t.Fatal("no Wi-Fi device must mean no Wi-Fi")
	}
}
