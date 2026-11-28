package main

// The update endpoint (Chris 2026-11-21 «Πρέπει να βρούμε λύσεις»: work
// stopped for a day because the PC kept an old orOS and nobody was there
// to update it). orOS on this computer asks for an update when the owner
// presses Update on another device, or Claude asks through orOS Gate; the
// page forwards it here.
//   - GET  /__oros/update: { version, commit, staged, auto, running }.
//   - POST /__oros/update (same origin, X-Oros-Gate: 1): downloads the
//     newest orOS now (one at a time) and swaps it in, also under an open
//     window (its service worker reloads it). Every copy whose orOS
//     updates itself (appAutoUpdatesOn: installed, the stick, orOS USB
//     with persistence); a new program still waits for a start.

import (
	"encoding/json"
	"log"
	"net/http"
)

const updatePath = "/__oros/update"

type updateAPI struct {
	lay   layout
	fetch func(layout) error // checkAppUpdate (tests replace it)
	apply func(layout)       // applyStagedApp
}

func newUpdateAPI(lay layout) *updateAPI {
	return &updateAPI{lay: lay, fetch: checkAppUpdate, apply: applyStagedApp}
}

func (a *updateAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{
			"version": appVersion(a.lay.appDir()),
			"commit":  shortSHA(appCommit(a.lay.appDir())),
			"staged":  stagedAppWaiting(a.lay),
			"auto":    appAutoUpdatesOn(a.lay),
			"running": updateBusy.Load(),
		})
	case http.MethodPost:
		if r.Header.Get("X-Oros-Gate") != "1" || r.Header.Get("Sec-Fetch-Site") != "same-origin" ||
			(r.Header.Get("Origin") != "" && !allowedOrigin(r.Header.Get("Origin"))) {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		if !appAutoUpdatesOn(a.lay) {
			http.Error(w, `{"error":"off"}`, http.StatusConflict)
			return
		}
		if !updateBusy.CompareAndSwap(false, true) {
			_, _ = w.Write([]byte(`{"ok":true,"already":true}`))
			return
		}
		go func() {
			defer updateBusy.Store(false)
			err := a.fetch(a.lay)
			if err != nil {
				log.Printf("update asked from orOS: %v", err)
				updateLog(a.lay, "update asked from orOS failed: "+err.Error())
				return
			}
			if stagedAppWaiting(a.lay) {
				a.apply(a.lay)
			}
		}()
		_, _ = w.Write([]byte(`{"ok":true}`))
	default:
		http.Error(w, `{"error":"method"}`, http.StatusMethodNotAllowed)
	}
}

// updAPI serves /__oros/update in every launcher that serves orOS.
var updAPI = newUpdateAPIFromEnv()

func newUpdateAPIFromEnv() *updateAPI {
	root, err := rootDir()
	if err != nil {
		return nil
	}
	return newUpdateAPI(newLayout(root))
}
