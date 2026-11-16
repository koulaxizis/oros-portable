//go:build !windows

package main

// The desktop bar docks on Windows only for now; elsewhere the menu
// entry is not shown.
const deskbarSupported = false

func dockBar(stop <-chan struct{}) {}
func closeBar()                    {}
