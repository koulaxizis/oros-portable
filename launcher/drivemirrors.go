//go:build linux || windows

package main

// orOS Drive's mirrored folders in "This computer" (DRIVE-OPEN, Chris
// 2026-11-27: «να είναι άμεσα διαθέσιμα τα αρχεία και οι φάκελοι του
// υπολογιστή στο orOS»). orOS Drive mirrors folders of this computer
// into /internal/Drive/<device>/<name> of the orOS disk. orOS Files'
// "This computer" lists the same folders from the disk, and without
// this answer it cannot know they are already in orOS, so it offered to
// copy them there.
//
// GET /__oros/fs/drive answers {"folders": [{path, remote, paused}]}:
// each folder from Drive's own settings (app.json, as the tray reads it
// in drive.go) that "This computer" can show, by its virtual path
// ("/home/Documents/Books", "/disks/D/Photos"). orOS Files then opens a
// file there from the orOS Drive copy, which Drive keeps in step with
// this folder both ways. Only read; nothing is written to Drive's
// settings, and a folder this API would not show (private, hidden, a
// link out of its place) is left out.

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// driveMirror is one mirrored folder as "This computer" names it.
type driveMirror struct {
	Path   string `json:"path"`   // virtual path in this API
	Remote string `json:"remote"` // "/internal/Drive/<device>/<name>"
	Paused bool   `json:"paused,omitempty"`
}

// drivePair is the part of a Drive folder (oros-drive mirror.Pair) read here.
type drivePair struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
	Paused bool   `json:"paused"`
}

const driveRemoteRoot = "/internal/Drive/"

// driveRemoteOK: a remote path orOS may open: under /internal/Drive/,
// at least <device>/<name>, no empty, "." or ".." segment.
func driveRemoteOK(r string) bool {
	if !strings.HasPrefix(r, driveRemoteRoot) || strings.ContainsAny(r, "\\\x00") {
		return false
	}
	segs := strings.Split(strings.TrimPrefix(r, driveRemoteRoot), "/")
	if len(segs) < 2 {
		return false
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			return false
		}
	}
	return true
}

// readDrivePairs reads the folders from Drive's app.json; none when it
// is missing or unreadable.
func readDrivePairs() []drivePair {
	b, err := os.ReadFile(filepath.Join(driveConfigDir(), "app.json"))
	if err != nil {
		return nil
	}
	var s struct {
		Folders []drivePair `json:"folders"`
	}
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return s.Folders
}

// virtualOf maps a real folder to this API's virtual path: under the home
// folder first, then under a disk. "" when this API cannot show it.
func (c *computerFS) virtualOf(real string) string {
	if real == "" || !filepath.IsAbs(real) {
		return ""
	}
	real = filepath.Clean(real)
	type root struct{ v, real string }
	roots := []root{{"/home", c.homeDir()}}
	for _, d := range c.disks() {
		if r, ok := c.diskRoot(strings.TrimPrefix(d.Path, "/disks/")); ok {
			roots = append(roots, root{d.Path, r})
		}
	}
	for _, r := range roots {
		if r.real == "" || !within(real, r.real) {
			continue
		}
		rel := ""
		if len(real) > len(filepath.Clean(r.real)) {
			rel = strings.TrimLeft(real[len(filepath.Clean(r.real)):], `\/`)
		}
		v := r.v
		if rel != "" {
			v = path.Join(r.v, filepath.ToSlash(rel))
		}
		if p, err := c.resolve(v); err != nil || p.real == "" {
			continue
		}
		return v
	}
	return ""
}

// driveMirrors is the answer of GET /__oros/fs/drive.
func (c *computerFS) driveMirrors() []driveMirror {
	out := []driveMirror{}
	for _, p := range readDrivePairs() {
		if !driveRemoteOK(p.Remote) {
			continue
		}
		v := c.virtualOf(p.Local)
		if v == "" {
			continue
		}
		out = append(out, driveMirror{Path: v, Remote: p.Remote, Paused: p.Paused})
	}
	return out
}
