package main

// Extensions a user adds and orOS's own pages.
//
// The Internet window opens in the orOS window's profile (sysweb.go), so an
// extension a user installs there (a VPN, for example) runs next to orOS.
// An extension allowed on "all sites" could then read an unlocked orOS
// (Passwords, Notes). Chromium's ExtensionSettings policy keeps every
// extension away from orOS's addresses (runtime_blocked_hosts) and gives
// only the System extension and the orOS add-on their own entry without
// it; a proxy still works for the whole browser. orOS USB has the same in
// its managed policy (oros-usb config/.../policies/managed/oros.json).
//
// Chromium reads policies only from the system: on Windows the launcher
// writes the value under HKCU\Software\Policies\Chromium
// (extpolicy_windows.go); a portable copy takes it away again when orOS
// closes. Linux keeps them in /etc, which a stick cannot write, so there
// the guide warns instead.
//
// webstoreDir holds chromium-web-store (NeverDecaf, MIT; scripts/webstore.sh),
// which puts an "Add to Chromium" button on the Chrome Web Store pages and
// checks the extensions installed from there for updates: ungoogled-chromium
// has neither. It needs mimeFlag, ungoogled-chromium's switch that offers
// to install a downloaded extension instead of saving it as a file.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
)

const (
	webstoreDir = "webstore"
	mimeFlag    = "--extension-mime-request-handling=always-prompt-for-install"
)

// orosHosts are the addresses other extensions may not reach.
var orosHosts = []string{"*://localhost", "*://127.0.0.1", "*://*.useoros.online"}

// webstoreHere: the full edition and the installers carry the helper.
func webstoreHere(lay layout) bool {
	_, err := os.Stat(filepath.Join(lay.root, webstoreDir, "manifest.json"))
	return err == nil
}

// extensionID is Chromium's ID for a manifest key: the first 16 bytes of
// the key's SHA-256, one letter a-p per hex digit.
func extensionID(key string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(der) == 0 {
		return "", errors.New("not a manifest key")
	}
	sum := sha256.Sum256(der)
	id := make([]byte, 32)
	for i, b := range sum[:16] {
		id[2*i] = 'a' + b>>4
		id[2*i+1] = 'a' + b&15
	}
	return string(id), nil
}

// orosExtensionIDs are the IDs of the System extension and the orOS add-on
// next to the launcher, from the keys in their manifests. When one of them
// is there without a key (an add-on from before oros-usb gave it one) it
// returns nil: a policy without its ID would lock it out of orOS.
func orosExtensionIDs(lay layout) []string {
	var ids []string
	for _, name := range []string{sysExtFolder, addonDir} {
		b, err := os.ReadFile(filepath.Join(lay.root, name, "manifest.json"))
		if err != nil {
			continue
		}
		var m struct{ Key string }
		_ = json.Unmarshal(b, &m)
		id, err := extensionID(m.Key)
		if err != nil {
			log.Printf("extension policy: %s has no key, so no policy", name)
			return nil
		}
		ids = append(ids, id)
	}
	return ids
}

// extPolicy is the ExtensionSettings value for these IDs.
func extPolicy(ids []string) string {
	p := map[string]any{"*": map[string]any{"runtime_blocked_hosts": orosHosts}}
	for _, id := range ids {
		p[id] = map[string]any{"runtime_blocked_hosts": []string{}}
	}
	b, _ := json.Marshal(p)
	return string(b)
}

// isOurPolicy: a value orOS wrote before (perhaps with other IDs, from an
// older copy), which may be replaced.
func isOurPolicy(v string) bool {
	var p map[string]struct {
		Blocked []string `json:"runtime_blocked_hosts"`
	}
	if json.Unmarshal([]byte(v), &p) != nil {
		return false
	}
	all, ok := p["*"]
	if !ok || len(all.Blocked) != len(orosHosts) {
		return false
	}
	for i, h := range orosHosts {
		if all.Blocked[i] != h {
			return false
		}
	}
	return true
}

var policyDone bool // under hostMu, like hostDone

// applyExtPolicyOnce writes the policy once per run, before the bundled
// browser starts, and keeps how to take it back with the host's.
func applyExtPolicyOnce(lay layout) {
	hostMu.Lock()
	defer hostMu.Unlock()
	if policyDone {
		return
	}
	policyDone = true
	ids := orosExtensionIDs(lay)
	if len(ids) == 0 {
		return
	}
	undo, err := setExtPolicy(lay, extPolicy(ids))
	if err != nil {
		log.Printf("extension policy: %v", err)
	}
	if undo != nil {
		hostCleanup = append(hostCleanup, undo)
	}
}
