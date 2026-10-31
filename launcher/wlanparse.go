package main

import (
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf16"
)

// The parts of the Windows Wi-Fi code (net_windows.go) that are plain data,
// here so the tests run everywhere: reading WLAN_AVAILABLE_NETWORK and
// writing a WLAN profile.

const (
	availNetworkSize = 628 // sizeof(WLAN_AVAILABLE_NETWORK)
	flagConnected    = 1   // WLAN_AVAILABLE_NETWORK_CONNECTED
	flagHasProfile   = 2   // WLAN_AVAILABLE_NETWORK_HAS_PROFILE
)

var errEnterpriseWifi = errors.New("this network needs a user name as well: connect to it once in Windows' settings, then orOS can use it")

type availNetwork struct {
	network
	profile string
	flags   uint32
	auth    uint32
	cipher  uint32
	ssidRaw []byte
}

// parseAvailNetwork reads one WLAN_AVAILABLE_NETWORK.
func parseAvailNetwork(b []byte) availNetwork {
	u32 := func(off int) uint32 {
		return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
	}
	prof := make([]uint16, 256)
	for i := range prof {
		prof[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		if prof[i] == 0 {
			prof = prof[:i]
			break
		}
	}
	l := int(u32(512))
	if l > 32 {
		l = 32
	}
	raw := append([]byte(nil), b[516:516+l]...)
	a := availNetwork{
		profile: string(utf16.Decode(prof)),
		ssidRaw: raw,
		flags:   u32(620),
		auth:    u32(612),
		cipher:  u32(616),
	}
	a.SSID = string(raw)
	a.Signal = int(u32(604))
	a.Secure = u32(608) != 0
	a.InUse = a.flags&flagConnected != 0
	return a
}

// wifiProfileXML is a WLAN profile for a personal or open network. The
// SSID goes in as hex, so any name works.
func wifiProfileXML(ssid []byte, auth, cipher uint32, password string) (string, error) {
	var a, e string
	switch auth {
	case 1: // 802.11 open
		a, e = "open", "none"
		if cipher != 0 {
			return "", errors.New("WEP networks are not supported")
		}
	case 4: // WPA-PSK
		a = "WPAPSK"
	case 7: // RSNA-PSK (WPA2 personal)
		a = "WPA2PSK"
	case 9: // WPA3 SAE
		a = "WPA3SAE"
	default:
		return "", errEnterpriseWifi
	}
	if e == "" {
		e = "AES"
		if cipher == 2 {
			e = "TKIP"
		}
		if len(password) < 8 || len(password) > 63 {
			return "", errors.New("the password must have 8 to 63 characters")
		}
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><WLANProfile xmlns="http://www.microsoft.com/networking/WLAN/profile/v1">`)
	b.WriteString("<name>" + xmlText(string(ssid)) + "</name>")
	b.WriteString("<SSIDConfig><SSID><hex>" + strings.ToUpper(hex.EncodeToString(ssid)) + "</hex></SSID></SSIDConfig>")
	b.WriteString("<connectionType>ESS</connectionType><connectionMode>auto</connectionMode>")
	b.WriteString("<MSM><security><authEncryption><authentication>" + a + "</authentication><encryption>" + e +
		"</encryption><useOneX>false</useOneX></authEncryption>")
	if e != "none" {
		b.WriteString("<sharedKey><keyType>passPhrase</keyType><protected>false</protected><keyMaterial>" +
			xmlText(password) + "</keyMaterial></sharedKey>")
	}
	b.WriteString("</security></MSM></WLANProfile>")
	return b.String(), nil
}

func xmlText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}
