package main

import (
	"net/url"
	"strings"
)

// orOS in a window of its own (Chris 2026-11-21, item 4 of «Πρέπει να
// βρούμε λύσεις»): on Windows, "orOS.exe window" shows orOS in a WebView2
// window that belongs to orOS.exe itself, not to a browser. It shows only
// the local orOS (http://localhost:47800); links to anything else open in
// the default browser. Its data (WebView2's own profile, data\webview2)
// is separate from the browser window's, so orOS opens there like on a
// new device: unlock once, then it syncs like the others.

const webWinDir = "webview2"

// webWinJS: links and window.open to another site go to the default
// browser through orosOpenExternal; orOS's own pages stay in the window.
const webWinJS = `(function () {
  if (window.__orosWebWin) return; window.__orosWebWin = 1;
  var O = location.origin;
  function ext(u) {
    try { var x = new URL(u, location.href); return x.origin !== O && /^(https?|mailto):$/.test(x.protocol) ? x.href : ""; } catch (e) { return ""; }
  }
  document.addEventListener("click", function (e) {
    var a = e.target && e.target.closest ? e.target.closest("a[href]") : null;
    if (!a) return;
    var u = ext(a.href);
    if (u) { e.preventDefault(); e.stopPropagation(); window.orosOpenExternal(u); }
  }, true);
  var wo = window.open;
  window.open = function (u) {
    var x = ext(u || "");
    if (x) { window.orosOpenExternal(x); return null; }
    return wo.apply(window, arguments);
  };
})();`

// externalOK: what orosOpenExternal hands to the default browser: web
// and mail addresses only, never a file, a program or another scheme.
func externalOK(raw string) bool {
	if len(raw) > 4096 || strings.ContainsAny(raw, "\x00\r\n\"") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "https", "http":
		return u.Host != "" && !strings.HasPrefix(u.Host, "localhost") && !strings.HasPrefix(u.Host, "127.")
	case "mailto":
		return u.Opaque != ""
	}
	return false
}
