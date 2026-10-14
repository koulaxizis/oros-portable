package main

import (
	"log"
	"os"
	"syscall"
	"unsafe"
)

// The Windows build has no console window, so messages use a message box.
func messageBox(text string, flags uintptr) {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString("orOS Portable")
	_, _, _ = proc.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags)
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
