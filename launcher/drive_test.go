package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDriveSignInPassesOn(t *testing.T) {
	h := newHandler(t.TempDir())
	get := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", target, nil)
		req.Host = fmt.Sprintf("localhost:%d", port)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := get("/__oros/drive-signin"); w.Code != 200 || w.Body.String() != `{"ok":true}` {
		t.Fatalf("drive-signin: %d %s", w.Code, w.Body)
	}
	st := "orosdrive-47812-abcdefghijklmnopqrstuv"
	w := get("/?code=C0DE&state=" + st)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "http://127.0.0.1:47812/oauth/done?code=C0DE&state="+st {
		t.Fatalf("redirect: %d %q", w.Code, w.Header().Get("Location"))
	}
	for _, bad := range []string{
		"/?code=x&state=orosdrive-47800-abcdefghijklmnopqrstuv", // not a Drive page port
		"/?code=x&state=orosdrive-47812-short",
		"/?code=x&state=abcdefghijklmnopqrstuv", // orOS's own sign-in
		"/other?code=x&state=orosdrive-47812-abcdefghijklmnopqrstuv",
	} {
		if to, ok := driveRedirect(httptest.NewRequest("GET", bad, nil)); ok {
			t.Errorf("%s went to %s", bad, to)
		}
	}
}

func TestDriveKeeperStartsAtMostThree(t *testing.T) {
	k := &driveKeeper{}
	now := time.Now()
	for i := 0; i < 3; i++ {
		if !k.mayStart(now) {
			t.Fatalf("start %d refused", i)
		}
	}
	if k.mayStart(now.Add(time.Minute)) {
		t.Fatal("a fourth start within 10 minutes")
	}
	if !k.mayStart(now.Add(11 * time.Minute)) {
		t.Fatal("no start after 10 minutes")
	}
}

func TestDriveCallUsesInstance(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROS_DRIVE_CONFIG", dir)
	if _, err := driveStatus(); err != errDriveOff {
		t.Fatalf("no instance: %v", err)
	}
	var ln net.Listener
	for p := 47811; p <= 47820 && ln == nil; p++ {
		ln, _ = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	}
	if ln == nil {
		t.Skip("no free port in 47811-47820")
	}
	var gotPause any
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("orosdrive"); err != nil || c.Value != "tok" {
			http.Error(w, "forbidden", 403)
			return
		}
		switch r.URL.Path {
		case "/api/summary":
			json.NewEncoder(w).Encode(driveSummary{V: 1, State: "paused", Paused: true, SignedIn: true, Unlocked: true})
		case "/api/pause":
			if r.Header.Get("X-OD") != "1" {
				http.Error(w, "forbidden", 403)
				return
			}
			var in map[string]any
			json.NewDecoder(r.Body).Decode(&in)
			gotPause = in["paused"]
			w.Write([]byte(`{"ok":true}`))
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()
	inst, _ := json.Marshal(driveInstance{Port: ln.Addr().(*net.TCPAddr).Port, Token: "tok"})
	os.WriteFile(filepath.Join(dir, "instance.json"), inst, 0o600)
	s, err := driveStatus()
	if err != nil || s.State != "paused" || driveText(s, nil) == "" {
		t.Fatalf("%+v %v", s, err)
	}
	if err := driveCall("POST", "/api/pause", map[string]bool{"paused": false}, nil); err != nil || gotPause != false {
		t.Fatalf("pause: %v %v", err, gotPause)
	}
}

func TestDriveSettingsRead(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OROS_DRIVE_CONFIG", dir)
	if readDriveSettings().exists {
		t.Fatal("no app.json, yet exists")
	}
	os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{"device":"pc","folders":[{"id":"a"}],"managedBy":"/x/orOS","ownAutostart":true}`), 0o600)
	s := readDriveSettings()
	if !s.exists || len(s.Folders) != 1 || s.ManagedBy != "/x/orOS" || !s.OwnAutostart {
		t.Fatalf("%+v", s)
	}
}
