package main

import (
	"path/filepath"
	"testing"
)

func TestHalves(t *testing.T) {
	l, r := halves(rect{0, 40, 1921, 1000})
	if l != (rect{0, 40, 960, 1000}) || r != (rect{960, 40, 961, 1000}) {
		t.Fatalf("halves: %v %v", l, r)
	}
}

func TestPickWins(t *testing.T) {
	root := filepath.Join(string(filepath.Separator)+"stick", "orOS")
	ws := []topWin{
		{id: 1, exe: filepath.Join(root, "browser", "chrome"), w: 640, h: 460},   // the small quick window
		{id: 2, exe: filepath.Join(root, "browser", "chrome"), w: 1900, h: 1000}, // orOS
		{id: 3, exe: filepath.Join(string(filepath.Separator)+"apps", "Claude", "claude.exe"), w: 1200, h: 800},
		{id: 4, exe: filepath.Join(string(filepath.Separator)+"apps", "Claude", "claude.exe"), w: 400, h: 300, owned: true}, // a dialog
		{id: 5, exe: filepath.Join(string(filepath.Separator)+"stick", "orOSother", "chrome"), w: 3000, h: 2000},            // not under root
		{id: 6, exe: filepath.Join(string(filepath.Separator)+"apps", "claude-desktop"), w: 100, h: 100},                    // too small
	}
	o, c := pickWins(ws, root)
	if o != 2 || c != 3 {
		t.Fatalf("pickWins = %d, %d", o, c)
	}
	if o, c := pickWins(ws[:2], root); o != 2 || c != 0 {
		t.Fatalf("no Claude yet: %d, %d", o, c)
	}
	if o, _ := pickWins(ws, ""); o != 0 {
		t.Fatalf("no root picks no orOS window: %d", o)
	}
}
