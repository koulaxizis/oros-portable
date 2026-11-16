package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// The background browser runs in its own process group (so one kill ends
// its helpers too) and ends with the tray, also when the tray is killed.
func gateHostProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGTERM}
}

func gateHostAfterStart(*exec.Cmd) {}

// killGateHost ends the background browser's process group, only when
// that group's leader is still a browser on an orOS profile (a pid from
// an old gate-host.run may belong to something else by now).
func killGateHost(pid int) {
	if pid <= 1 || !gateHostIsBrowser(pid) {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}

// gateHostIsBrowser: /proc/<pid>/cmdline names --headless and an orOS
// --user-data-dir.
func gateHostIsBrowser(pid int) bool {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "--headless") && strings.Contains(s, "--user-data-dir=") && strings.Contains(s, "gate-host=")
}
