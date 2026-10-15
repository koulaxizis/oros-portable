package main

import (
	"os"
	"syscall"
	"unsafe"
)

const fileFlagNoBuffering = 0x20000000

// openUncached opens the file without the Windows file cache, so the read
// test measures the stick. Reads must then use sector-aligned buffers.
func openUncached(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL|fileFlagNoBuffering, 0)
	if err != nil {
		return os.Open(path)
	}
	return os.NewFile(uintptr(h), path), nil
}

// alignedBuf returns a buffer whose start is aligned to 4 KiB.
func alignedBuf(n int) []byte {
	const align = 4096
	b := make([]byte, n+align)
	off := align - int(uintptr(unsafe.Pointer(&b[0]))%align)
	if off == align {
		off = 0
	}
	return b[off : off+n]
}
