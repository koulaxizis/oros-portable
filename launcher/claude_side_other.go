//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Linux under X11 with wmctrl: the windows are listed with their process
// (wmctrl -lpG), the work area comes from wmctrl -d, and both windows are
// moved with wmctrl -e. Under Wayland, or without wmctrl, nothing happens.

// wmWindows reads "wmctrl -lpG": id desktop pid x y w h host title.
func wmWindows(out string, exeOf func(pid int) string) []topWin {
	var ws []topWin
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		id, err1 := strconv.ParseUint(strings.TrimPrefix(f[0], "0x"), 16, 64)
		pid, err2 := strconv.Atoi(f[2])
		w, err3 := strconv.Atoi(f[5])
		h, err4 := strconv.Atoi(f[6])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || pid <= 0 {
			continue
		}
		ws = append(ws, topWin{id: uintptr(id), exe: exeOf(pid), w: w, h: h})
	}
	return ws
}

// wmWork reads the current desktop's work area from "wmctrl -d"
// ("0  * DG: 1920x1080  VP: 0,0  WA: 0,27 1920x1053  Desk 1").
func wmWork(out string) (rect, bool) {
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[1] != "*" {
			continue
		}
		for i := 0; i+2 < len(f); i++ {
			if f[i] != "WA:" {
				continue
			}
			xy, wh := strings.Split(f[i+1], ","), strings.Split(f[i+2], "x")
			if len(xy) != 2 || len(wh) != 2 {
				return rect{}, false
			}
			x, e1 := strconv.Atoi(xy[0])
			y, e2 := strconv.Atoi(xy[1])
			w, e3 := strconv.Atoi(wh[0])
			h, e4 := strconv.Atoi(wh[1])
			if e1 != nil || e2 != nil || e3 != nil || e4 != nil || w <= 0 || h <= 0 {
				return rect{}, false
			}
			return rect{x, y, w, h}, true
		}
	}
	return rect{}, false
}

func procExe(pid int) string {
	p, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	return p
}

func wmMove(id uintptr, r rect) {
	hex := "0x" + strconv.FormatUint(uint64(id), 16)
	_ = exec.Command("wmctrl", "-i", "-r", hex, "-b", "remove,maximized_vert,maximized_horz,fullscreen").Run()
	_ = exec.Command("wmctrl", "-i", "-r", hex, "-e", "0,"+strconv.Itoa(r.x)+","+strconv.Itoa(r.y)+","+strconv.Itoa(r.w)+","+strconv.Itoa(r.h)).Run()
}

func claudeArrange(root string, wait time.Duration) {
	if os.Getenv("DISPLAY") == "" || os.Getenv("WAYLAND_DISPLAY") != "" {
		return
	}
	if _, err := exec.LookPath("wmctrl"); err != nil {
		return
	}
	d, err := exec.Command("wmctrl", "-d").Output()
	if err != nil {
		return
	}
	work, ok := wmWork(string(d))
	if !ok {
		return
	}
	left, right := halves(work)
	for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(500 * time.Millisecond) {
		out, err := exec.Command("wmctrl", "-lpG").Output()
		if err != nil {
			return
		}
		oros, claude := pickWins(wmWindows(string(out), procExe), root)
		if claude == 0 {
			continue
		}
		time.Sleep(700 * time.Millisecond)
		wmMove(claude, right)
		if oros != 0 {
			wmMove(oros, left)
		}
		return
	}
}
