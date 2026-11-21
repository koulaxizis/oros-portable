package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateAPIRefusesOtherPagesAndRunsOnce(t *testing.T) {
	d := t.TempDir()
	lay := layout{root: d, data: d, updates: d, installed: true}
	oldV := version
	version = "0.9.4"
	defer func() { version = oldV }()
	got := make(chan bool, 4)
	a := &updateAPI{lay: lay,
		fetch: func(l layout) error {
			_ = os.MkdirAll(stagedAppDir(l), 0o755)
			_ = os.WriteFile(filepath.Join(stagedAppDir(l), "index.html"), []byte("x"), 0o644)
			time.Sleep(50 * time.Millisecond)
			return nil
		},
		apply: func(l layout) { got <- true }}

	r := httptest.NewRequest(http.MethodPost, updatePath, strings.NewReader("{}"))
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("a page without the header and same-origin: %d", w.Code)
	}

	post := func() string {
		r := httptest.NewRequest(http.MethodPost, updatePath, strings.NewReader("{}"))
		r.Header.Set("X-Oros-Gate", "1")
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("orOS's own page: %d %s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if b := post(); strings.Contains(b, "already") {
		t.Fatalf("first ask starts: %s", b)
	}
	if b := post(); !strings.Contains(b, "already") {
		t.Fatalf("second ask while running: %s", b)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the downloaded orOS was not swapped in")
	}
}
