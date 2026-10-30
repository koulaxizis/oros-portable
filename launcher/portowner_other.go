//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// findPortOwner reads /proc (Linux): the listening socket's inode from
// /proc/net/tcp and tcp6, then the process that has it open. Elsewhere,
// or for another user's process, nothing is found.
func findPortOwner(port int) (portOwner, bool) {
	inodes := map[string]bool{}
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, ino := range listenInodes(string(b), port) {
			inodes[ino] = true
		}
	}
	if len(inodes) == 0 {
		return portOwner{}, false
	}
	procs, _ := filepath.Glob("/proc/[0-9]*")
	for _, dir := range procs {
		fds, _ := os.ReadDir(filepath.Join(dir, "fd"))
		for _, fd := range fds {
			l, err := os.Readlink(filepath.Join(dir, "fd", fd.Name()))
			if err != nil || !strings.HasPrefix(l, "socket:[") {
				continue
			}
			if inodes[strings.TrimSuffix(strings.TrimPrefix(l, "socket:["), "]")] {
				pid, _ := strconv.Atoi(filepath.Base(dir))
				exe, _ := os.Readlink(filepath.Join(dir, "exe"))
				return portOwner{pid: pid, exe: exe}, true
			}
		}
	}
	return portOwner{}, false
}

func accessDenied(err error) bool {
	return errors.Is(err, syscall.EACCES)
}
