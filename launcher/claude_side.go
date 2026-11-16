package main

// Claude beside orOS (CLAUDE-1 side by side, Chris 2026-11-16 «Claude
// δίπλα στο orOS»): when orOS opens Claude desktop, Claude takes the right
// half of the screen and orOS the left, so the person talks to Claude
// (with their own subscription, and orOS Gate for their data) without
// switching windows. Windows places both windows itself
// (claude_side_windows.go); Linux does it under X11 where wmctrl is there
// (claude_side_other.go); anywhere else Claude simply opens as before.

import (
	"path/filepath"
	"strings"
	"time"
)

type rect struct{ x, y, w, h int }

// halves splits the work area: orOS on the left, Claude on the right
// (the odd pixel goes to Claude).
func halves(work rect) (left, right rect) {
	half := work.w / 2
	return rect{work.x, work.y, half, work.h}, rect{work.x + half, work.y, work.w - half, work.h}
}

// topWin is one visible top-level window with the program that owns it.
type topWin struct {
	id    uintptr
	exe   string // full path of the program
	w, h  int
	owned bool // a dialog or pop-up of another window
}

// pickWins finds Claude desktop's main window (its program is called
// claude) and orOS's main window (the browser under the launcher's own
// folder root; the largest one, so never the small quick window).
func pickWins(ws []topWin, root string) (oros, claude uintptr) {
	var ob, cb int
	pre := strings.ToLower(filepath.Clean(root)) + string(filepath.Separator)
	for _, w := range ws {
		if w.owned || w.w < 200 || w.h < 200 {
			continue
		}
		area := w.w * w.h
		name := strings.ToLower(filepath.Base(w.exe))
		switch {
		case name == "claude.exe" || name == "claude" || name == "claude-desktop":
			if area > cb {
				claude, cb = w.id, area
			}
		case root != "" && strings.HasPrefix(strings.ToLower(filepath.Clean(w.exe)), pre):
			if area > ob {
				oros, ob = w.id, area
			}
		}
	}
	return oros, claude
}

// claudeBeside waits up to 30 s for Claude's window, then places it and
// orOS's window side by side. Nothing happens where it is not possible.
func claudeBeside(lay layout) {
	claudeArrange(lay.root, 30*time.Second)
}
