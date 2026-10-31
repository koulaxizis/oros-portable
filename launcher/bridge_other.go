//go:build !linux && !windows

package main

import "os/exec"

func bridgeProcAttr(*exec.Cmd)   {}
func bridgeAfterStart(*exec.Cmd) {}
