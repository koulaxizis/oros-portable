//go:build !windows

package main

// setExtPolicy: Chromium on Linux and macOS reads policies only from
// system folders the launcher cannot write.
func setExtPolicy(lay layout, value string) (func(), error) { return nil, nil }
