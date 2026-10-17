package main

import (
	"log"
	"os"
	"syscall"
	"unsafe"
)

// The Windows build has no console window, so messages use a message box.
func messageBox(text string, flags uintptr) uintptr {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString(appTitle)
	r, _, _ := proc.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags)
	return r
}

// ask shows a yes/no question and reports whether the answer was yes.
func ask(msg string) bool {
	return messageBox(msg, 0x4|0x20) == 6 // MB_YESNO|MB_ICONQUESTION, IDYES
}

func notify(msg string) {
	log.Print(msg)
	messageBox(msg, 0x40) // MB_ICONINFORMATION
}

func fail(msg string) {
	log.Print(msg)
	messageBox(msg, 0x10) // MB_ICONERROR
	os.Exit(1)
}
