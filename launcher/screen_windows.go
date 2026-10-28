package main

import "unsafe"

var (
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procGetDpiForSystem       = user32.NewProc("GetDpiForSystem")
)

const spiGetWorkArea = 0x0030

// workArea: the primary screen without the taskbar, in Chromium's units
// (px at 100% scale). A DPI-aware launcher gets device pixels and the
// system DPI, a DPI-unaware one scaled pixels and 96: either way the
// division gives Chromium's units.
func workArea() (x, y, w, h int, ok bool) {
	var r struct{ left, top, right, bottom int32 }
	if ret, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0); ret == 0 {
		return 0, 0, 0, 0, false
	}
	dpi := 96
	if procGetDpiForSystem.Find() == nil { // Windows 10 1607 and later
		if d, _, _ := procGetDpiForSystem.Call(); d > 0 {
			dpi = int(d)
		}
	}
	scale := func(v int32) int { return int(v) * 96 / dpi }
	x, y, w, h = scale(r.left), scale(r.top), scale(r.right-r.left), scale(r.bottom-r.top)
	return x, y, w, h, w >= quickW && h >= quickH
}
