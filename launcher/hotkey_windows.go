package main

import (
	"log"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	procRegisterHotKey = user32.NewProc("RegisterHotKey")
	procGetMessageW    = user32.NewProc("GetMessageW")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modNoRepeat = 0x4000
	vkSpace     = 0x20
	wmHotkey    = 0x0312
)

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

// hotkeyLoop calls open on Ctrl+Alt+Shift+Space anywhere in Windows
// (the orOS shortcut family, R12). Another program holding it already
// only costs the shortcut; the tray works on.
func hotkeyLoop(open func()) {
	runtime.LockOSThread() // WM_HOTKEY goes to the thread that registered it
	r, _, err := procRegisterHotKey.Call(0, 1, modControl|modAlt|modShift|modNoRepeat, vkSpace)
	if r == 0 {
		log.Printf("Ctrl+Alt+Shift+Space is taken: %v", err)
		return
	}
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if m.message == wmHotkey {
			open()
		}
	}
}
