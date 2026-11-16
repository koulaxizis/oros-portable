package main

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procCreateWindowExW = user32.NewProc("CreateWindowExW")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
)

// A real window titled like the bar: found, framed off, made a tool
// window, topmost at the top of the screen; its space is given back.
func TestWindowsDeskbarDock(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	cls, _ := windows.UTF16PtrFromString("STATIC")
	title, _ := windows.UTF16PtrFromString(barTitle)
	const wsOverlappedWindow = 0x00CF0000
	h, _, err := procCreateWindowExW.Call(wsExAppWindow, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow, 100, 100, 600, 200, 0, 0, 0, 0)
	if h == 0 {
		t.Skipf("no window station here: %v", err)
	}
	defer procDestroyWindow.Call(h)
	if findBar() != h {
		t.Fatal("findBar did not find the window by its title")
	}
	abd := dock(h)
	defer undock(abd)
	st, _, _ := procGetWindowLongPtrW.Call(h, uintptr(^uintptr(15))) // GWL_STYLE (-16)
	ex, _, _ := procGetWindowLongPtrW.Call(h, uintptr(^uintptr(19))) // GWL_EXSTYLE (-20)
	if st&(wsCaption|wsThickFrame) != 0 {
		t.Fatalf("still framed: %#x", st)
	}
	if ex&wsExToolWin == 0 || ex&wsExAppWindow != 0 {
		t.Fatalf("not a tool window: %#x", ex)
	}
	const wsExTopmost = 0x00000008
	if ex&wsExTopmost == 0 {
		t.Fatalf("not topmost: %#x", ex)
	}
	var r struct{ left, top, right, bottom int32 }
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	if r.bottom-r.top < barH || r.bottom-r.top > barH*3 {
		t.Fatalf("height %d", r.bottom-r.top)
	}
	cx, _, _ := procGetSystemMetrics.Call(smCx)
	if r.right-r.left != int32(cx) {
		t.Fatalf("width %d, screen %d", r.right-r.left, cx)
	}
}

func TestWindowsDeskbarStyle(t *testing.T) {
	st, ex := barStyle(0x16CF0000, wsExAppWindow|0x100)
	if st&(wsCaption|wsThickFrame|wsSysMenu|wsMinimizeBox|wsMaximizeBox) != 0 || st&0x10000000 == 0 {
		t.Fatalf("style %#x", st)
	}
	if ex != wsExToolWin|0x100 {
		t.Fatalf("ex %#x", ex)
	}
}
