package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGlobalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

// memGiB is this computer's memory in GiB.
func memGiB() float64 {
	var m struct {
		Length               uint32
		MemoryLoad           uint32
		TotalPhys            uint64
		AvailPhys            uint64
		TotalPageFile        uint64
		AvailPageFile        uint64
		TotalVirtual         uint64
		AvailVirtual         uint64
		AvailExtendedVirtual uint64
	}
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return 0
	}
	return float64(m.TotalPhys) / (1 << 30)
}

// aiFAT: the folder is on a FAT or FAT32 volume (no file of 4 GiB).
func aiFAT(dir string) bool {
	_, fs, _ := volumeInfo(filepath.VolumeName(dir) + `\`)
	return fs == "FAT" || fs == "FAT32"
}

// aiSpawn starts Ollama with no window, outside Chromium's job object,
// so it outlives the native host.
func aiSpawn(cmd *exec.Cmd) error {
	const createBreakawayFromJob, createNoWindow = 0x01000000, 0x08000000
	var err error
	for _, flags := range []uint32{createBreakawayFromJob | createNoWindow, createNoWindow} {
		c := exec.Command(cmd.Path, cmd.Args[1:]...)
		c.Env, c.Stdout, c.Stderr = cmd.Env, cmd.Stdout, cmd.Stderr
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err = c.Start(); err == nil {
			return c.Process.Release()
		}
	}
	return err
}
