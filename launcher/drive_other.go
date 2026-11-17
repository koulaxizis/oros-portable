//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// Its own session: orOS Drive does not end with the tray.
func driveProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
