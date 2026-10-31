package main

import (
	"errors"
	"log"
)

// Wi-Fi and Bluetooth in the System panel of orOS Portable and the desktop
// apps (wave 3). Wi-Fi is controlled inside orOS, as on orOS USB: the list
// of networks, connecting with a password, the radio on or off, and which
// network is in use (Linux: NetworkManager's nmcli; Windows: the Native
// Wi-Fi API). Bluetooth stays with the computer: its button opens the
// computer's own Bluetooth settings, where pairing is safe and familiar.

// network is one Wi-Fi network, as the extension knows it from orOS USB.
type network struct {
	SSID   string `json:"ssid"`
	Signal int    `json:"signal"`
	Secure bool   `json:"secure"`
	InUse  bool   `json:"inUse"`
}

// wifiState is the part of "status" about the network.
type wifiState struct {
	Present bool     // the computer has a Wi-Fi adapter we can drive
	On      bool     // its radio is on
	Current *network // the network in use, if any
	Wired   bool     // a cable is connected
}

type netOps interface {
	wifi() wifiState
	wifiScan() ([]network, error)
	wifiConnect(ssid, password string) error
	wifiRadio(on bool) error
	hasBtSettings() bool
	btSettings() error
}

// sysNet is this computer's network part; tests replace it.
var sysNet netOps = hostNet()

var errNoWifi = errors.New("this computer has no Wi-Fi that orOS can use")

func handleNet(req sysRequest) (sysReply, bool) {
	n := sysNet
	switch req.Cmd {
	case "wifi.scan":
		if !n.wifi().Present {
			return sysFail(errNoWifi), true
		}
		list, err := n.wifiScan()
		if err != nil {
			return sysFail(err), true
		}
		return ok(sysReply{"networks": dedupeNetworks(list)}), true
	case "wifi.connect":
		if req.SSID == "" {
			return sysFail(errors.New("no network")), true
		}
		if err := n.wifiConnect(req.SSID, req.Password); err != nil {
			log.Printf("wifi: connecting failed: %v", err) // never the password
			return sysFail(err), true
		}
		return ok(nil), true
	case "wifi.radio":
		if err := n.wifiRadio(req.On); err != nil {
			return sysFail(err), true
		}
		return ok(nil), true
	case "bt.settings":
		if !n.hasBtSettings() {
			return sysFail(errUnsupported), true
		}
		if err := n.btSettings(); err != nil {
			return sysFail(err), true
		}
		return ok(nil), true
	}
	return nil, false
}

// netStatus adds the network fields orOS USB's status has.
func netStatus(r sysReply, caps map[string]bool) {
	w := sysNet.wifi()
	caps["wifi"] = w.Present
	caps["btSettings"] = sysNet.hasBtSettings()
	if w.Present {
		r["wifiOn"] = w.On
		if w.Current != nil {
			r["wifi"] = w.Current
		}
	}
	r["wired"] = w.Wired
}

// dedupeNetworks keeps one entry per name, the one in use or else the
// strongest, and drops hidden networks.
func dedupeNetworks(in []network) []network {
	best := map[string]int{}
	list := []network{}
	for _, n := range in {
		if n.SSID == "" {
			continue
		}
		if i, seen := best[n.SSID]; seen {
			if n.InUse || (n.Signal > list[i].Signal && !list[i].InUse) {
				list[i] = n
			}
			continue
		}
		best[n.SSID] = len(list)
		list = append(list, n)
	}
	return list
}
