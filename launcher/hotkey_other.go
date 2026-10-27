//go:build !windows

package main

// hotkeyLoop: Linux desktops (Wayland) give no global shortcut to an app;
// the user binds "oros quick" in the desktop's keyboard settings instead.
func hotkeyLoop(func()) {}
