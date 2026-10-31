//go:build !linux && !windows

package main

func computerID() string { return "" }
