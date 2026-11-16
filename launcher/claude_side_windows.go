package main

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSetWindowPos  = user32.NewProc("SetWindowPos")
	procShowWindow    = user32.NewProc("ShowWindow")
	procGetWindowRect = user32.NewProc("GetWindowRect")
	procGetWindow     = user32.NewProc("GetWindow")
)

const (
	gwOwner       = 4
	swRestore     = 9
	swpNoZOrder   = 0x0004
	swpShowWindow = 0x0040
)

// topWindows lists the visible top-level windows with their programs.
func topWindows() []topWin {
	var out []topWin
	cb := windows.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		if !windows.IsWindowVisible(h) {
			return 1
		}
		var r struct{ l, t, r, b int32 }
		if ok, _, _ := procGetWindowRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r))); ok == 0 {
			return 1
		}
		owner, _, _ := procGetWindow.Call(uintptr(h), gwOwner)
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(h, &pid); err != nil || pid == 0 {
			return 1
		}
		exe := ""
		if p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid); err == nil {
			buf := make([]uint16, windows.MAX_PATH)
			n := uint32(len(buf))
			if windows.QueryFullProcessImageName(p, 0, &buf[0], &n) == nil {
				exe = windows.UTF16ToString(buf[:n])
			}
			windows.CloseHandle(p)
		}
		out = append(out, topWin{id: uintptr(h), exe: exe, w: int(r.r - r.l), h: int(r.b - r.t), owned: owner != 0})
		return 1
	})
	_ = windows.EnumWindows(cb, nil)
	return out
}

// workRect: the primary screen without the taskbar, in this process's
// own units (the same ones SetWindowPos takes).
func workRect() (rect, bool) {
	var r struct{ l, t, r, b int32 }
	if ok, _, _ := procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&r)), 0); ok == 0 {
		return rect{}, false
	}
	return rect{int(r.l), int(r.t), int(r.r - r.l), int(r.b - r.t)}, r.r > r.l && r.b > r.t
}

func placeWin(id uintptr, r rect) {
	procShowWindow.Call(id, swRestore) // out of maximised (or minimised) first
	procSetWindowPos.Call(id, 0, uintptr(r.x), uintptr(r.y), uintptr(r.w), uintptr(r.h), swpNoZOrder|swpShowWindow)
}

func claudeArrange(root string, wait time.Duration) {
	work, ok := workRect()
	if !ok {
		return
	}
	left, right := halves(work)
	for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
		oros, claude := pickWins(topWindows(), root)
		if claude == 0 {
			continue
		}
		time.Sleep(700 * time.Millisecond) // Electron sizes its window once more as it shows
		placeWin(claude, right)
		if oros != 0 {
			placeWin(oros, left)
		}
		return
	}
}
