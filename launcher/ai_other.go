//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// memGiB is this computer's memory in GiB (Linux; 0 elsewhere).
func memGiB() float64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[0] == "MemTotal:" {
			kb, _ := strconv.ParseFloat(f[1], 64)
			return kb / (1 << 20)
		}
	}
	return 0
}

// aiFAT: the folder is on a FAT or FAT32 file system (no file of 4 GiB).
func aiFAT(dir string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return false
	}
	return st.Type == 0x4d44 // MSDOS_SUPER_MAGIC
}

// aiSpawn starts Ollama in a session of its own, so it outlives the
// native host (Chromium ends the host's process group).
func aiSpawn(cmd *exec.Cmd) error {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
