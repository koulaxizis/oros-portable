//go:build !windows

package main

// workArea is not known here (X11 and Wayland differ, and on Wayland a
// window cannot choose its place): the orOS page centres "#quick-win".
func workArea() (x, y, w, h int, ok bool) { return 0, 0, 0, 0, false }
