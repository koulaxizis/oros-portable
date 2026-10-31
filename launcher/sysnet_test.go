package main

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

type fakeNet struct {
	state     wifiState
	scan      []network
	connected []string
	radio     []bool
	bt        bool
	btOpened  int
}

func (f *fakeNet) wifi() wifiState              { return f.state }
func (f *fakeNet) wifiScan() ([]network, error) { return f.scan, nil }
func (f *fakeNet) wifiConnect(s, p string) error {
	f.connected = append(f.connected, s+"/"+p)
	return nil
}
func (f *fakeNet) wifiRadio(on bool) error { f.radio = append(f.radio, on); return nil }
func (f *fakeNet) hasBtSettings() bool     { return f.bt }
func (f *fakeNet) btSettings() error       { f.btOpened++; return nil }

func withFakeNet(t *testing.T, f *fakeNet) {
	t.Helper()
	old := sysNet
	sysNet = f
	t.Cleanup(func() { sysNet = old })
}

func TestWifiCommands(t *testing.T) {
	withFake(t)
	lay := portableLayout(t)
	f := &fakeNet{}
	withFakeNet(t, f)
	st := handleSys(lay, sysRequest{Cmd: "status"})
	caps := st["caps"].(map[string]bool)
	if caps["wifi"] || caps["btSettings"] || st["wifiOn"] != nil {
		t.Fatalf("no Wi-Fi, no Bluetooth: %v", st)
	}
	if r := handleSys(lay, sysRequest{Cmd: "wifi.scan"}); r["ok"] != false {
		t.Fatal("scan without Wi-Fi")
	}
	if r := handleSys(lay, sysRequest{Cmd: "bt.settings"}); r["ok"] != false {
		t.Fatal("Bluetooth settings without an app")
	}

	cur := network{SSID: "Home", Signal: 70, Secure: true, InUse: true}
	f.state = wifiState{Present: true, On: true, Current: &cur}
	f.bt = true
	f.scan = []network{{SSID: "Cafe", Signal: 30}, {SSID: "Cafe", Signal: 80}, {SSID: ""}, cur, {SSID: "Home", Signal: 99}}
	st = handleSys(lay, sysRequest{Cmd: "status"})
	caps = st["caps"].(map[string]bool)
	if !caps["wifi"] || !caps["btSettings"] || st["wifiOn"] != true || st["wifi"].(*network).SSID != "Home" {
		t.Fatalf("status: %v", st)
	}
	r := handleSys(lay, sysRequest{Cmd: "wifi.scan"})
	nets := r["networks"].([]network)
	if len(nets) != 2 || nets[0].Signal != 80 || !nets[1].InUse || nets[1].Signal != 70 {
		t.Fatalf("one entry per name, the strongest or the one in use, no hidden ones: %+v", nets)
	}
	if r := handleSys(lay, sysRequest{Cmd: "wifi.connect"}); r["ok"] != false {
		t.Fatal("connect without a name")
	}
	handleSys(lay, sysRequest{Cmd: "wifi.connect", SSID: "Cafe", Password: "secret-pw"})
	handleSys(lay, sysRequest{Cmd: "wifi.radio", On: false})
	handleSys(lay, sysRequest{Cmd: "bt.settings"})
	if strings.Join(f.connected, ",") != "Cafe/secret-pw" || len(f.radio) != 1 || f.radio[0] || f.btOpened != 1 {
		t.Fatalf("%+v", f)
	}
	for _, c := range []string{"bt.list", "printers.list", "disks.list", "time.get", "install.disks"} {
		if r := handleSys(lay, sysRequest{Cmd: c}); r["ok"] != false {
			t.Fatalf("%s must stay unsupported", c)
		}
	}
}

// availBytes builds a WLAN_AVAILABLE_NETWORK as wlanapi lays it out.
func availBytes(profile, ssid string, signal, secure, auth, cipher, flags uint32) []byte {
	b := make([]byte, availNetworkSize)
	for i, u := range utf16.Encode([]rune(profile)) {
		binary.LittleEndian.PutUint16(b[2*i:], u)
	}
	binary.LittleEndian.PutUint32(b[512:], uint32(len(ssid)))
	copy(b[516:], ssid)
	binary.LittleEndian.PutUint32(b[548:], 1) // infrastructure
	binary.LittleEndian.PutUint32(b[604:], signal)
	binary.LittleEndian.PutUint32(b[608:], secure)
	binary.LittleEndian.PutUint32(b[612:], auth)
	binary.LittleEndian.PutUint32(b[616:], cipher)
	binary.LittleEndian.PutUint32(b[620:], flags)
	return b
}

func TestParseAvailNetwork(t *testing.T) {
	a := parseAvailNetwork(availBytes("Σπίτι", "Σπίτι 5G", 88, 1, 7, 4, flagConnected|flagHasProfile))
	if a.profile != "Σπίτι" || a.SSID != "Σπίτι 5G" || a.Signal != 88 || !a.Secure || !a.InUse || a.auth != 7 || a.cipher != 4 {
		t.Fatalf("%+v", a)
	}
	if b := parseAvailNetwork(availBytes("", "Open", 20, 0, 1, 0, 0)); b.InUse || b.Secure || b.profile != "" {
		t.Fatalf("%+v", b)
	}
}

func TestWifiProfileXML(t *testing.T) {
	x, err := wifiProfileXML([]byte("Café <&>"), 7, 4, `pa"ss&word`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<name>Café &lt;&amp;&gt;</name>", "<hex>436166C3A9203C263E</hex>", "<authentication>WPA2PSK</authentication>",
		"<encryption>AES</encryption>", "<keyMaterial>pa&quot;ss&amp;word</keyMaterial>"} {
		if !strings.Contains(x, want) {
			t.Fatalf("missing %s in %s", want, x)
		}
	}
	if x, _ := wifiProfileXML([]byte("Open"), 1, 0, ""); !strings.Contains(x, "<authentication>open</authentication><encryption>none</encryption>") || strings.Contains(x, "sharedKey") {
		t.Fatalf("open: %s", x)
	}
	if x, _ := wifiProfileXML([]byte("New"), 9, 4, "12345678"); !strings.Contains(x, "WPA3SAE") {
		t.Fatalf("WPA3: %s", x)
	}
	if _, err := wifiProfileXML([]byte("Work"), 6, 4, "x"); err != errEnterpriseWifi {
		t.Fatalf("enterprise: %v", err)
	}
	if _, err := wifiProfileXML([]byte("Short"), 7, 4, "1234"); err == nil {
		t.Fatal("a 4-character password was accepted")
	}
	if _, err := wifiProfileXML([]byte("Old"), 1, 1, ""); err == nil {
		t.Fatal("WEP was accepted")
	}
}
