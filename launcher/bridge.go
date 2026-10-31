package main

// orOS Bridge (plugins and the sound card for orOS Studio) in the full
// edition and the installers: bridge/<os>/ holds the bridge from its latest
// release (scripts/build.sh checks it against the release's SHA256SUMS.txt).
// The launcher that serves orOS starts it with --bundled (no window, no
// console, no updates of its own: a new orOS brings a new bridge) and
// --code-file, and ends it with the server. The System extension shows the
// pairing code over orOS (syshost.go: ping and bridge.code), as on orOS USB.
// When another bridge already answers on its port (one the person
// installed), that one is used and nothing is started.

import (
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	bridgeAddr     = "127.0.0.1:47621"
	bridgeCodeName = "bridge-code"
)

var bridgeState struct {
	sync.Mutex
	cmd *exec.Cmd
}

// bridgeExe is the bundled bridge for this system, or "" when this edition has none.
func bridgeExe(lay layout) string {
	name := "oros-bridge"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(lay.root, "bridge", runtime.GOOS, name)
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return ""
	}
	return p
}

func bridgeCodeFile(lay layout) string { return filepath.Join(lay.data, bridgeCodeName) }

// bridgeArgs are the bundled bridge's options: only orOS on this
// computer's address may pair, besides useoros.online (the bridge's own).
func bridgeArgs(lay layout) []string {
	return []string{"--bundled", "--allow-origin", strings.TrimSuffix(appURL, "/"), "--code-file", bridgeCodeFile(lay)}
}

// bridgeAnswers: something listens on the bridge's port already.
func bridgeAnswers() bool {
	c, err := net.DialTimeout("tcp", bridgeAddr, 300*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// startBridge starts the bundled bridge unless it runs already, this
// edition has none, or OROS_NO_BRIDGE is set. Its output goes to bridge.log.
func startBridge(lay layout) {
	bridgeState.Lock()
	defer bridgeState.Unlock()
	if bridgeState.cmd != nil || os.Getenv("OROS_NO_BRIDGE") != "" {
		return
	}
	exe := bridgeExe(lay)
	if exe == "" {
		return
	}
	if bridgeAnswers() {
		log.Printf("orOS Bridge: another bridge runs on %s, using that one", bridgeAddr)
		return
	}
	_ = os.MkdirAll(lay.data, 0o755)
	logf, err := os.OpenFile(filepath.Join(lay.data, "bridge.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		log.Printf("orOS Bridge: log: %v", err)
	}
	cmd := exec.Command(exe, bridgeArgs(lay)...)
	cmd.Dir = filepath.Dir(exe)
	if logf != nil {
		cmd.Stdout, cmd.Stderr = logf, logf
	}
	bridgeProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("orOS Bridge: %v", err)
		if logf != nil {
			logf.Close()
		}
		return
	}
	bridgeAfterStart(cmd)
	bridgeState.cmd = cmd
	log.Printf("orOS Bridge started (pid %d)", cmd.Process.Pid)
	go func() {
		err := cmd.Wait()
		if logf != nil {
			logf.Close()
		}
		bridgeState.Lock()
		if bridgeState.cmd == cmd {
			bridgeState.cmd = nil
			_ = os.Remove(bridgeCodeFile(lay))
			log.Printf("orOS Bridge ended: %v", err)
		}
		bridgeState.Unlock()
	}()
}

// stopBridge ends the bridge this launcher started, if any.
func stopBridge(lay layout) {
	bridgeState.Lock()
	cmd := bridgeState.cmd
	bridgeState.cmd = nil
	bridgeState.Unlock()
	if cmd == nil {
		return
	}
	_ = cmd.Process.Kill() // the system frees the plugins, as quitting the bridge does
	_ = os.Remove(bridgeCodeFile(lay))
	log.Printf("orOS Bridge stopped")
}

// bridgeCode is the open pairing code the bridge keeps in its code file
// (six digits while orOS Studio waits for it), or "".
func bridgeCode(lay layout) string {
	b, err := os.ReadFile(bridgeCodeFile(lay))
	if err != nil {
		return ""
	}
	c := strings.TrimSpace(string(b))
	if len(c) != 6 || strings.Trim(c, "0123456789") != "" {
		return ""
	}
	return c
}
