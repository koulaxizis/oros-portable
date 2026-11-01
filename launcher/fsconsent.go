package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Whether orOS may see this computer's files (Files' "This computer"),
// asked once per computer (Bible: "This computer" on Portable). The
// answer is kept in the data folder under a hash of the computer's own ID,
// so a stick remembers each computer it met without naming it:
//   - orOS USB: always on (the stick is the computer);
//   - the desktop apps: on unless the user turned it off;
//   - a stick on someone's computer: off until the user says yes.

const consentFile = "computer-files.json"

type fsConsent struct {
	mu       sync.Mutex
	file     string // tests: a fixed file; otherwise the signed-in user's (users.go)
	data     string
	computer string
	always   bool   // orOS USB
	def      string // "on" (desktop apps) or "ask" (a stick)
}

func newFSConsent(lay layout, always bool) *fsConsent {
	def := "ask"
	if lay.installed {
		def = "on"
	}
	id := computerID()
	if id != "" {
		sum := sha256.Sum256([]byte("oros-computer-files:" + id))
		id = hex.EncodeToString(sum[:12])
	}
	return &fsConsent{data: lay.data, computer: id, always: always, def: def}
}

// path: each user answers for themselves, so the answer lives in the
// signed-in user's folder (the data folder for the first user).
func (c *fsConsent) path() string {
	if c.file != "" {
		return c.file
	}
	return filepath.Join(currentUserDir(c.data), consentFile)
}

func (c *fsConsent) read() map[string]string {
	m := map[string]string{}
	if b, err := os.ReadFile(c.path()); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// state is "on", "off" or "ask".
func (c *fsConsent) state() string {
	if c.always {
		return "on"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.computer == "" {
		return "off" // no way to tell computers apart: never on by itself
	}
	if s := c.read()[c.computer]; s == "on" || s == "off" {
		return s
	}
	return c.def
}

func (c *fsConsent) on() bool { return c.state() == "on" }

func (c *fsConsent) set(on bool) error {
	if c.always || c.computer == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.read()
	m[c.computer] = map[bool]string{true: "on", false: "off"}[on]
	b, _ := json.MarshalIndent(m, "", "  ")
	file := c.path()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
