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
	procIsWindow          = user32.NewProc("IsWindow")
	procPostMessageW      = user32.NewProc("PostMessageW")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	procGetDpiForWindow   = user32.NewProc("GetDpiForWindow")
	procEnumChildWindows  = user32.NewProc("EnumChildWindows")
	procGetClassNameW     = user32.NewProc("GetClassNameW")
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

	wmClose = 0x0010
	smCx    = 0 // SM_CXSCREEN
)

var hwndTopmost = ^uintptr(0) // HWND_TOPMOST = (HWND)-1

type winRect struct{ left, top, right, bottom int32 }

type appBarData struct {
	cbSize           uint32
	hWnd             uintptr
	uCallbackMessage uint32
	uEdge            uint32
	rc               winRect
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
	fit(h, abd.rc)
	return abd
}

// pageRect: where the page shows inside Chromium's window h, in screen
// units: Chromium's render widget window covers exactly the page.
func pageRect(h uintptr) (winRect, bool) {
	var found uintptr
	cb := windows.NewCallback(func(c windows.HWND, _ uintptr) uintptr {
		buf := make([]uint16, 64)
		procGetClassNameW.Call(uintptr(c), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if windows.UTF16ToString(buf) == "Chrome_RenderWidgetHostHWND" {
			found = uintptr(c)
			return 0
		}
		return 1
	})
	procEnumChildWindows.Call(h, cb, 0)
	if found == 0 {
		return winRect{}, false
	}
	var r winRect
	if ok, _, _ := procGetWindowRect.Call(found, uintptr(unsafe.Pointer(&r))); ok == 0 || r.bottom <= r.top {
		return winRect{}, false
	}
	return r, true
}

// fitted: the window rect that puts the page of a window at w, whose
// page is at p, exactly on want. Chromium draws its own title bar inside
// the window, so taking the native frame away leaves it there: the
// window grows by it and its title bar goes above the screen's edge.
func fitted(w, p, want winRect) winRect {
	return winRect{
		left:   want.left - (p.left - w.left),
		top:    want.top - (p.top - w.top),
		right:  want.right + (w.right - p.right),
		bottom: want.bottom + (w.bottom - p.bottom),
	}
}

// fit moves h so that only its page shows, on the app bar's rect.
func fit(h uintptr, want winRect) {
	p, ok := pageRect(h)
	if !ok {
		return
	}
	var w winRect
	if r, _, _ := procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&w))); r == 0 {
		return
	}
	if p == want {
		return
	}
	f := fitted(w, p, want)
	procSetWindowPos.Call(h, hwndTopmost, uintptr(f.left), uintptr(f.top),
		uintptr(f.right-f.left), uintptr(f.bottom-f.top), swpNoActivate|swpShowWindow)
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
		fit(h, abd.rc) // the page shows late, or Chromium sized it again
	}
	logBar("closed")
}

// closeBar closes the bar window (it is a window of the user's browser).
func closeBar() {
	if h := findBar(); h != 0 {
		procPostMessageW.Call(h, wmClose, 0, 0)
	}
}
