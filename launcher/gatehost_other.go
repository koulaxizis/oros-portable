//go:build !linux && !windows

package main

import "os/exec"

func gateHostProcAttr(*exec.Cmd)   {}
func gateHostAfterStart(*exec.Cmd) {}
func killGateHost(int)             {}
func gateHostIsBrowser(int) bool   { return false }
