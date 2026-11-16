package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeskbarSetting(t *testing.T) {
	dir := t.TempDir()
	if deskbarOn(dir) {
		t.Fatal("on before it was set")
	}
	if err := setDeskbar(dir, true); err != nil || !deskbarOn(dir) {
		t.Fatalf("set on: %v", err)
	}
	if err := setDeskbar(dir, false); err != nil || deskbarOn(dir) {
		t.Fatalf("set off: %v", err)
	}
	if err := setDeskbar(dir, false); err != nil {
		t.Fatalf("off twice: %v", err)
	}
}

func TestDeskbarOpenEndpoint(t *testing.T) {
	var got []string
	openMain = func(app string) { got = append(got, app) }
	defer func() { openMain = nil }()
	h := newHandler(t.TempDir())
	do := func(method, target string, hdr map[string]string) int {
		r := httptest.NewRequest(method, "http://localhost:47800"+target, nil)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	ok := map[string]string{"X-Oros-Bar": "1"}
	if c := do("POST", "/__oros-open?app=calendar", ok); c != http.StatusNoContent {
		t.Fatalf("open calendar: %d", c)
	}
	if c := do("POST", "/__oros-open", map[string]string{"X-Oros-Bar": "1", "Origin": "http://localhost:47800"}); c != http.StatusNoContent {
		t.Fatalf("open orOS: %d", c)
	}
	for _, bad := range []struct {
		m, p string
		h    map[string]string
	}{
		{"GET", "/__oros-open?app=calendar", ok},   // only POST
		{"POST", "/__oros-open?app=calendar", nil}, // the header is needed
		{"POST", "/__oros-open?app=calendar", map[string]string{"X-Oros-Bar": "1", "Origin": "https://evil.example"}}, // another origin
		{"POST", "/__oros-open?app=../x", ok}, // not an app id
		{"POST", "/__oros-open?app=Calendar", ok},
	} {
		if c := do(bad.m, bad.p, bad.h); c < 400 {
			t.Fatalf("%s %s %v: %d", bad.m, bad.p, bad.h, c)
		}
	}
	if len(got) != 2 || got[0] != "calendar" || got[1] != "" {
		t.Fatalf("opened %v", got)
	}
	openMain = nil
	if c := do("POST", "/__oros-open", ok); c != http.StatusNotFound {
		t.Fatalf("no tray: %d", c)
	}
	if mainURLFor("") != appURL || mainURLFor("time") != appURL+"?open=time" {
		t.Fatal("main window address")
	}
	// the bar's own address goes on to orOS, like the small window's
	r := httptest.NewRequest("GET", "http://localhost:47800"+barPath, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/" {
		t.Fatalf("bar path: %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestDeskbarLayout(t *testing.T) {
	data := t.TempDir()
	lay := layout{data: data}
	ul, ok := barLayout(lay)
	if !ok || ul.data != data {
		t.Fatalf("one user: %v %+v", ok, ul)
	}
	if barPlacement != "localhost_/__oros-bar" || barURL != "http://localhost:47800/__oros-bar#bar-win" {
		t.Fatal("names")
	}
}
