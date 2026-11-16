package main

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const deskbarSupported = true

var (
	shell32Bar            = windows.NewLazySystemDLL("shell32.dll")
	procSHAppBarMessage   = shell32Bar.NewProc("SHAppBarMessage")
	procFindWindowW       = user32.NewProc("FindWindowW")
	procGetWindowLongPtrW = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos      = user32.NewProc("SetWindowPos")
	procIsWindow          = user32.NewProc("IsWindow")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	procGetDpiForWindow   = user32.NewProc("GetDpiForWindow")
)

const (
	abmNew      = 0x0
	abmRemove   = 0x1
	abmQueryPos = 0x2
	abmSetPos   = 0x3
	abeTop      = 1

	gwlStyle   = -16
	gwlExStyle = -20

	wsCaption     = 0x00C00000
	wsThickFrame  = 0x00040000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsMaximizeBox = 0x00010000
	wsExToolWin   = 0x00000080
	wsExAppWindow = 0x00040000

	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020
	swpShowWindow   = 0x0040

	wmClose = 0x0010
	smCx    = 0 // SM_CXSCREEN
)

var hwndTopmost = ^uintptr(0) // HWND_TOPMOST = (HWND)-1

type appBarData struct {
	cbSize           uint32
	hWnd             uintptr
	uCallbackMessage uint32
	uEdge            uint32
	rc               struct{ left, top, right, bottom int32 }
	lParam           uintptr
}

func findBar() uintptr {
	title, _ := windows.UTF16PtrFromString(barTitle)
	h, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	return h
}

func isWindow(h uintptr) bool {
	r, _, _ := procIsWindow.Call(h)
	return r != 0
}

// barStyle: no caption or frame, a tool window (no taskbar button, not
// in Alt+Tab).
func barStyle(style, ex uintptr) (uintptr, uintptr) {
	style &^= wsCaption | wsThickFrame | wsSysMenu | wsMinimizeBox | wsMaximizeBox
	ex = (ex | wsExToolWin) &^ wsExAppWindow
	return style, ex
}

// dock makes h the top app bar of the primary screen, barH px high at the
// window's DPI. → the app bar data to remove it again.
func dock(h uintptr) *appBarData {
	gs := func(i int32) uintptr { r, _, _ := procGetWindowLongPtrW.Call(h, uintptr(i)); return r }
	ss := func(i int32, v uintptr) { procSetWindowLongPtrW.Call(h, uintptr(i), v) }
	st, ex := barStyle(gs(gwlStyle), gs(gwlExStyle))
	ss(gwlStyle, st)
	ss(gwlExStyle, ex)
	dpi := uintptr(96)
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(h); d > 0 {
			dpi = d
		}
	}
	height := int32(uintptr(barH) * dpi / 96)
	cx, _, _ := procGetSystemMetrics.Call(smCx)
	abd := &appBarData{hWnd: h, uEdge: abeTop}
	abd.cbSize = uint32(unsafe.Sizeof(*abd))
	procSHAppBarMessage.Call(abmNew, uintptr(unsafe.Pointer(abd)))
	abd.rc.left, abd.rc.top, abd.rc.right, abd.rc.bottom = 0, 0, int32(cx), height
	procSHAppBarMessage.Call(abmQueryPos, uintptr(unsafe.Pointer(abd)))
	abd.rc.bottom = abd.rc.top + height
	procSHAppBarMessage.Call(abmSetPos, uintptr(unsafe.Pointer(abd)))
	procSetWindowPos.Call(h, hwndTopmost, uintptr(abd.rc.left), uintptr(abd.rc.top),
		uintptr(abd.rc.right-abd.rc.left), uintptr(abd.rc.bottom-abd.rc.top), swpNoActivate|swpFrameChanged|swpShowWindow)
	return abd
}

func undock(abd *appBarData) {
	if abd != nil {
		procSHAppBarMessage.Call(abmRemove, uintptr(unsafe.Pointer(abd)))
	}
}

// dockBar waits up to 30 s for the bar window, docks it, and gives its
// space back when the window closes or stop is closed.
func dockBar(stop <-chan struct{}) {
	var h uintptr
	for i := 0; i < 60 && h == 0; i++ {
		select {
		case <-stop:
			return
		case <-time.After(500 * time.Millisecond):
		}
		h = findBar()
	}
	if h == 0 {
		logBar("the window did not show")
		return
	}
	abd := dock(h)
	logBar("docked")
	defer undock(abd)
	for isWindow(h) {
		select {
		case <-stop:
			return
		case <-time.After(2 * time.Second):
		}
	}
	logBar("closed")
}

// closeBar closes the bar window (it is a window of the user's browser).
func closeBar() {
	if h := findBar(); h != 0 {
		procPostMessageW.Call(h, wmClose, 0, 0)
	}
}
