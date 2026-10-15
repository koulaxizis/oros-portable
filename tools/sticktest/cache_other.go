//go:build !linux && !windows

package main

import "os"

func openUncached(path string) (*os.File, error) { return os.Open(path) }

func alignedBuf(n int) []byte { return make([]byte, n) }
