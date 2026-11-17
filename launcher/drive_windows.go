package main

import (
	"os/exec"
	"syscall"
)

// No console window; orOS Drive does not end with the tray.
func driveProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 /* CREATE_NO_WINDOW */}
}
