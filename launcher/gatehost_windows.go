package main

import (
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

func gateHostProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// The background browser ends with the tray, also when the tray is
// killed (the bridge's job object, bridge_windows.go; its helpers join
// the job with it).
func gateHostAfterStart(cmd *exec.Cmd) { bridgeAfterStart(cmd) }

// killGateHost ends the background browser, only when that pid is still
// a chrome.exe (a pid from an old gate-host.run may be something else).
func killGateHost(pid int) {
	if pid <= 0 || !gateHostIsBrowser(pid) {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 0)
}

func gateHostIsBrowser(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(windows.UTF16ToString(buf[:n])), "chrome.exe")
}
