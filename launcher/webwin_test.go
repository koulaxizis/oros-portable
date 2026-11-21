package main

import (
	"strings"
	"testing"
)

func TestOwnWindowOpensOnlyWebAndMailOutside(t *testing.T) {
	for _, ok := range []string{"https://useoros.online/support/", "http://example.org/a?b=c", "mailto:info@example.org"} {
		if !externalOK(ok) {
			t.Errorf("%q should open in the default browser", ok)
		}
	}
	for _, bad := range []string{"file:///C:/Windows/system32/calc.exe", "ms-settings:", "javascript:alert(1)", "http://localhost:47800/", "http://127.0.0.1:47800/x",
		"https://", "mailto:", "https://ex.org/\"&calc", "https://ex.org/\nx", strings.Repeat("a", 5000)} {
		if externalOK(bad) {
			t.Errorf("%q must not be opened", bad)
		}
	}
	if !strings.Contains(webWinJS, "window.orosOpenExternal(u)") || !strings.Contains(webWinJS, "x.origin !== O") {
		t.Error("the window's script sends only other sites to the default browser")
	}
}
