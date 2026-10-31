//go:build !linux

package main

import "net/http"

// Computer files (computerfs.go) exist only on Linux, for orOS USB.
type computerFS struct{}

var fsAPI *computerFS

func (c *computerFS) setCookie(http.ResponseWriter) {}

func (c *computerFS) ServeHTTP(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }

func diskHelper(string, []string) int { return 1 }
