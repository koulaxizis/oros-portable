package main

import (
	"os/exec"
	"syscall"
)

// The bridge ends with the launcher, also when the launcher is killed.
func bridgeProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

func bridgeAfterStart(*exec.Cmd) {}
