package main

import "golang.org/x/sys/windows"

var procGetUserDefaultUILanguage = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

func windowsLang() (string, bool) {
	r, _, _ := procGetUserDefaultUILanguage.Call()
	switch uint16(r) & 0x3ff {
	case 0x08: // LANG_GREEK
		return "el", true
	case 0x07: // LANG_GERMAN
		return "de", true
	}
	return "en", true
}
