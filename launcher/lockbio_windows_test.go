package main

import "testing"

// The real WinRT call on the runner: the IIDs and vtable slots answer
// (a runner has no Windows Hello, so "no-hw" or "none" is expected).
func TestWindowsHelloAvailabilityAnswers(t *testing.T) {
	c, err := helloCheck()
	if err != nil {
		t.Fatalf("CheckAvailabilityAsync: %v", err)
	}
	t.Logf("Windows Hello on this runner: %s", c)
	if c != "ok" && c != "none" && c != "no-hw" {
		t.Errorf("can %q", c)
	}
}

func TestWindowsHelloSealsForThisUser(t *testing.T) {
	d := []byte("0123456789abcdef0123456789abcdef")
	s, err := (helloBio{}).seal(d)
	if err != nil || string(s) == string(d) {
		t.Fatalf("seal %v", err)
	}
	back, err := (helloBio{}).unseal(s)
	if err != nil || string(back) != string(d) {
		t.Fatalf("unseal %v %q", err, back)
	}
	if _, err := (helloBio{}).unseal(append(s[:len(s)-1], s[len(s)-1]^1)); err == nil {
		t.Error("a changed copy opened")
	}
}

func TestWindowsHelloResults(t *testing.T) {
	want := map[int32]string{1: "no-hw", 2: "none", 3: "no-hw", 4: "busy", 5: "lockout", 6: "cancel", 9: "failed"}
	if helloResult(0) != nil {
		t.Error("verified")
	}
	for v, w := range want {
		if err := helloResult(v); err == nil || err.Error() != w {
			t.Errorf("%d: %v", v, err)
		}
	}
}
