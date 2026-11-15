package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The System extension's manifest key (oros-usb system/extension), whose
// ID the native host's allowed_origins already names.
const sysKey = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvm/8qvQlIFbsbsvYRaN3cbu7ekkckD8PxwTWQlJNsVIlyWgxTOHtcmQiGA8PuAMr+tg5q3fpqVOFpjS/iVs9phkgtFaFMciVThx2cwxf7qPA5wfTYQbjMSYK5r3UzYorjBu+yoR+yI1S1Ld00AUaWyPRMXoUTHB44SWe2HExiKJXRHI1AYNZOeJAJZ4/r+nAwjXn1Yehy3b1QouB6/Q+o1ByJkcmtU6JkEeW2ghW159pPjyV0b7m9wnnrA4h9eyLIVnKdCSc2oaxzNtv3hyx2zlUMCM3VXvqYIivfeA5zmK+NR1aT5se/s4mLjQpJ218AgbJOpXO/0EYY8/GPjNOAQIDAQAB"

func TestExtensionID(t *testing.T) {
	if id, err := extensionID(sysKey); err != nil || id != extID {
		t.Fatalf("got %q %v, want %s", id, err, extID)
	}
	if _, err := extensionID("not base64!"); err == nil {
		t.Fatal("a broken key has no ID")
	}
}

// Only the System extension and the orOS add-on may reach orOS's pages;
// every other extension (a VPN the user adds) is kept away from them.
func TestExtPolicy(t *testing.T) {
	lay := portableLayout(t)
	t.Cleanup(unregisterHost)
	for name, m := range map[string]string{
		sysExtFolder: `{"key":"` + sysKey + `"}`,
		addonDir:     `{"name":"no key"}`,
		ubolDir:      `{"key":"` + sysKey + `"}`, // never exempt, whatever its key
	} {
		must(t, os.MkdirAll(filepath.Join(lay.root, name), 0o755))
		must(t, os.WriteFile(filepath.Join(lay.root, name, "manifest.json"), []byte(m), 0o644))
	}
	if ids := orosExtensionIDs(lay); ids != nil {
		t.Fatalf("ids %v: with an add-on without a key there is no policy, or the add-on is locked out", ids)
	}
	must(t, os.RemoveAll(filepath.Join(lay.root, addonDir)))
	ids := orosExtensionIDs(lay)
	if len(ids) != 1 || ids[0] != extID {
		t.Fatalf("ids %v: uBlock Origin Lite never counts", ids)
	}
	v := extPolicy(append(ids, "cbabbleeefcdjmjjdpfiojpmiekpmcci"))
	var p map[string]map[string][]string
	must(t, json.Unmarshal([]byte(v), &p))
	if len(p) != 3 || strings.Join(p["*"]["runtime_blocked_hosts"], " ") != "*://localhost *://127.0.0.1 *://*.useoros.online" {
		t.Fatalf("policy %s", v)
	}
	for _, id := range []string{extID, "cbabbleeefcdjmjjdpfiojpmiekpmcci"} {
		if b, ok := p[id]["runtime_blocked_hosts"]; !ok || b == nil || len(b) != 0 {
			t.Fatalf("%s needs its own entry with an empty list (an entry replaces *): %s", id, v)
		}
	}
	if !isOurPolicy(v) || !isOurPolicy(extPolicy(nil)) {
		t.Fatal("orOS's own value, from any copy, may be replaced")
	}
	for _, other := range []string{"", "{}", `{"*":{"installation_mode":"blocked"}}`, `{"*":{"runtime_blocked_hosts":["*://localhost"]}}`} {
		if isOurPolicy(other) {
			t.Fatalf("someone else's value stays: %q", other)
		}
	}
}

// With chromium-web-store next to the launcher the bundled browser loads
// it and offers to install downloaded extensions; without it, no switch.
func TestWebStoreHelper(t *testing.T) {
	lay := portableLayout(t)
	t.Cleanup(unregisterHost)
	c := candidate{path: "chrome", profile: filepath.Join(lay.data, "profile"), bundled: true}
	sys := filepath.Join(lay.root, sysExtFolder)
	must(t, os.MkdirAll(sys, 0o755))
	must(t, os.WriteFile(filepath.Join(sys, "manifest.json"), []byte("{}"), 0o644))
	if a := strings.Join(sysBrowserArgs(lay, c, appURL), " "); strings.Contains(a, "extension-mime") {
		t.Fatalf("no helper, no switch: %s", a)
	}
	ws := filepath.Join(lay.root, webstoreDir)
	must(t, os.MkdirAll(ws, 0o755))
	must(t, os.WriteFile(filepath.Join(ws, "manifest.json"), []byte("{}"), 0o644))
	a := strings.Join(sysBrowserArgs(lay, c, appURL), " ")
	if !strings.Contains(a, "--load-extension="+sys+","+ws) || !strings.Contains(a, mimeFlag) {
		t.Fatalf("orOS window: %s", a)
	}
	if w := strings.Join(webArgs(lay), " "); !strings.Contains(w, ","+ws) || !strings.Contains(w, mimeFlag) {
		t.Fatalf("Internet window: %s", w)
	}
}
