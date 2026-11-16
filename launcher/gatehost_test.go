package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gateHostLay(t *testing.T) layout {
	t.Helper()
	d := t.TempDir()
	t.Setenv("OROS_PORTABLE_BROWSER", filepath.Join(d, "fake-chrome"))
	t.Setenv("OROS_NO_GATE_HOST", "")
	return layout{root: d, data: d, updates: d, installed: true}
}

func gateGet(t *testing.T, a *gateHostAPI, q string) gateHostState {
	t.Helper()
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest(http.MethodGet, gateHostPath+q, nil))
	var st gateHostState
	must(t, json.Unmarshal(w.Body.Bytes(), &st))
	return st
}

func gatePost(a *gateHostAPI, body string, hdr map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, gateHostPath, strings.NewReader(body))
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

var gateOK = map[string]string{"X-Oros-Gate": "1", "Sec-Fetch-Site": "same-origin", "Origin": "http://localhost:47800"}

func TestGateHostOffByDefaultAndOnlyInstalled(t *testing.T) {
	lay := gateHostLay(t)
	a := &gateHostAPI{lay: lay}
	st := gateGet(t, a, "")
	if !st.Can || st.On || st.Running || st.Host {
		t.Fatalf("default: %+v", st)
	}
	lay.installed = false
	if gateGet(t, &gateHostAPI{lay: lay}, "").Can {
		t.Fatal("a stick (not installed) can't keep a background orOS")
	}
	t.Setenv("OROS_NO_GATE_HOST", "1")
	if gateGet(t, a, "").Can {
		t.Fatal("OROS_NO_GATE_HOST hides it")
	}
}

func TestGateHostPostOnlyFromOrOS(t *testing.T) {
	lay := gateHostLay(t)
	turned := 0
	a := &gateHostAPI{lay: lay, turnOn: func() { turned++ }}
	for name, h := range map[string]map[string]string{
		"no header":    {"Sec-Fetch-Site": "same-origin"},
		"cross-site":   {"X-Oros-Gate": "1", "Sec-Fetch-Site": "cross-site"},
		"other origin": {"X-Oros-Gate": "1", "Sec-Fetch-Site": "same-origin", "Origin": "http://localhost:8080"},
	} {
		if w := gatePost(a, `{"on":true}`, h); w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", name, w.Code)
		}
	}
	if w := gatePost(a, `{"x":1}`, gateOK); w.Code != http.StatusBadRequest {
		t.Fatalf("no on: %d", w.Code)
	}
	if readGateHostPref(lay.data).On || turned != 0 {
		t.Fatal("nothing turned on yet")
	}
	if w := gatePost(a, `{"on":true}`, gateOK); w.Code != 200 {
		t.Fatalf("on: %d %s", w.Code, w.Body)
	}
	if !readGateHostPref(lay.data).On || turned != 1 {
		t.Fatal("on, and the tray asked to start")
	}
	if w := gatePost(a, `{"on":false}`, gateOK); w.Code != 200 || readGateHostPref(lay.data).On {
		t.Fatalf("off: %d", w.Code)
	}
}

func TestGateHostNonceProvesTheHost(t *testing.T) {
	lay := gateHostLay(t)
	a := &gateHostAPI{lay: lay, running: func() bool { return true }}
	must(t, writeGateHostPref(lay.data, true))
	n := newGateNonce()
	b, _ := json.Marshal(gateHostRunRec{Pid: 4242, Nonce: n, At: 1})
	must(t, os.WriteFile(filepath.Join(lay.data, gateHostRun), b, 0o600))
	if st := gateGet(t, a, ""); st.Host || !st.Running {
		t.Fatalf("a window page is not the host: %+v", st)
	}
	if st := gateGet(t, a, "?n="+strings.Repeat("x", len(n))); st.Host {
		t.Fatal("a wrong nonce is not the host")
	}
	st := gateGet(t, a, "?n="+n)
	if !st.Host || st.Yield {
		t.Fatalf("the host: %+v", st)
	}
	// A window wants the profile: the host is told to yield, and says bye.
	must(t, os.WriteFile(filepath.Join(lay.data, gateHostHold), []byte("x"), 0o600))
	if st := gateGet(t, a, "?n="+n+"&bye=1"); !st.Yield {
		t.Fatalf("yield: %+v", st)
	}
	if _, err := os.Stat(filepath.Join(lay.data, gateHostBye)); err != nil {
		t.Fatal("bye written")
	}
	// Turned off: the host yields too and is no host any more.
	must(t, os.Remove(filepath.Join(lay.data, gateHostHold)))
	must(t, writeGateHostPref(lay.data, false))
	if st := gateGet(t, a, "?n="+n); st.Host || !st.Yield {
		t.Fatalf("off: %+v", st)
	}
}

func TestGateHostArgsAreHeadlessOnTheProfile(t *testing.T) {
	args := strings.Join(gateHostArgs("/p/profile", "NONCE"), " ")
	for _, want := range []string{"--headless=new", "--user-data-dir=/p/profile", "--disable-background-timer-throttling", appURL + "?gate-host=NONCE"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %q in %s", want, args)
		}
	}
	if strings.Contains(args, "--app=") {
		t.Fatal("no window")
	}
}

func TestGateHostYieldWithoutHostIsQuick(t *testing.T) {
	d := t.TempDir()
	gateHostYield(d)
	if !gateHostHeld(d) {
		t.Fatal("a window start holds the background orOS off for a while")
	}
}

func TestGateHostWindowNonceIsTheLockRule(t *testing.T) {
	lay := gateHostLay(t)
	a := &gateHostAPI{lay: lay}
	if u := gateHostURL(lay.data, appURL); u != appURL {
		t.Fatal("off: the window's address stays")
	}
	w := gatePost(a, `{"on":true}`, gateOK)
	var st gateHostState
	must(t, json.Unmarshal(w.Body.Bytes(), &st))
	if st.Win == "" || !st.Bg {
		t.Fatalf("turned on from the window: %+v", st)
	}
	if got := gateGet(t, a, "?n="+st.Win); !got.Host || got.Kind != "lock" {
		t.Fatalf("the window's nonce: %+v", got)
	}
	u := gateHostURL(lay.data, appURL)
	n := strings.TrimPrefix(u, appURL+"?gate-host=")
	if n == u || len(n) < 16 {
		t.Fatalf("a new window gets a nonce: %s", u)
	}
	if got := gateGet(t, a, "?n="+st.Win); got.Host {
		t.Fatal("the old window nonce is gone")
	}
	if gateHostURL(lay.data, quickURL) != quickURL {
		t.Fatal("the small window never gets one")
	}
}

func TestGateHostOnUSBIsTheLockRuleOnly(t *testing.T) {
	lay := gateHostLay(t)
	lay.installed = false
	t.Setenv("OROS_COMPUTER_FILES", "1")
	a := &gateHostAPI{lay: lay, turnOn: func() { t.Fatal("no tray on orOS USB") }}
	st := gateGet(t, a, "")
	if !st.Can || st.Bg {
		t.Fatalf("USB: %+v", st)
	}
	if w := gatePost(a, `{"on":true}`, gateOK); w.Code != 200 {
		t.Fatalf("on: %d", w.Code)
	}
	b, _ := json.Marshal(gateHostRunRec{Pid: 4242, Nonce: strings.Repeat("r", 24), At: 1})
	must(t, os.WriteFile(filepath.Join(lay.data, gateHostRun), b, 0o600))
	if got := gateGet(t, a, "?n="+strings.Repeat("r", 24)); got.Host {
		t.Fatal("no background orOS on USB")
	}
}
