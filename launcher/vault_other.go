//go:build !linux

package main

// Encrypted users exist only on orOS USB (Linux, vault_linux.go).
func newSysVault(string) vaultFS { return nil }

type fsKeyID [16]byte

const fsKeyLen = 64
