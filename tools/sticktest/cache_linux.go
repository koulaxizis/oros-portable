package main

import (
	"os"
	"syscall"
)

// openUncached drops the file from the page cache, so the read test measures
// the stick and not memory.
func openUncached(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	const fadvDontneed = 4
	_, _, _ = syscall.Syscall6(syscall.SYS_FADVISE64, f.Fd(), 0, 0, fadvDontneed, 0, 0)
	return f, nil
}

func alignedBuf(n int) []byte { return make([]byte, n) }
