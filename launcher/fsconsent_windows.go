//go:build windows

package main

import "golang.org/x/sys/windows/registry"

// computerID is the ID Windows gives each installation (MachineGuid).
func computerID() string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Cryptography`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, err := k.GetStringValue("MachineGuid")
	if err != nil {
		return ""
	}
	return s
}
