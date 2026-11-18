package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func chromeForTest() string {
	if p := os.Getenv("OROS_TEST_CHROME"); p != "" {
		return p
	}
	for _, p := range []string{
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func dumpWin(t *testing.T, label string, h uintptr) {
	var r struct{ l, t, r, b int32 }
	procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	st, _, _ := procGetWindowLongPtrW.Call(h, uintptr(^uintptr(15)))
	t.Logf("%s: top %#x rect %v style %#x", label, h, r, st)
	cb := windows.NewCallback(func(c windows.HWND, _ uintptr) uintptr {
		buf := make([]uint16, 256)
		procGetClassNameW.Call(uintptr(c), uintptr(unsafe.Pointer(&buf[0])), 256)
		var cr struct{ l, t, r, b int32 }
		procGetWindowRect.Call(uintptr(c), uintptr(unsafe.Pointer(&cr)))
		t.Logf("   child %#x %s %v visible=%v", c, windows.UTF16ToString(buf), cr, windows.IsWindowVisible(c))
		return 1
	})
	procEnumChildWindows.Call(h, cb, 0)
}

func shot(t *testing.T, name string) {
	dir := os.Getenv("OROS_SHOTS")
	if dir == "" {
		return
	}
	p := filepath.Join(dir, name+".png")
	ps := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms,System.Drawing; $b=[System.Windows.Forms.Screen]::PrimaryScreen.Bounds; $bm=New-Object System.Drawing.Bitmap $b.Width,$b.Height; $g=[System.Drawing.Graphics]::FromImage($bm); $g.CopyFromScreen(0,0,0,0,$bm.Size); $bm.Save('%s')`, p)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).CombinedOutput()
	t.Logf("shot %s: %v %s", p, err, out)
}

// A real Chrome app window, as the tray opens the bar: docked, only its
// page shows, exactly on the app bar's strip (Chromium's own title bar
// above the screen). OROS_SHOTS=<dir> saves screenshots before and after.
func TestWindowsDeskbarChrome(t *testing.T) {
	chrome := chromeForTest()
	if chrome == "" {
		t.Skip("no Chrome")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><title>x</title><style>html,body{margin:0;background:#c00;color:#fff;font:20px sans-serif}</style><body><div id=b>BAR CONTENT TOP</div><script>document.title="orOS bar";setInterval(function(){document.getElementById("b").textContent="inner "+innerWidth+"x"+innerHeight+" outer "+outerWidth+"x"+outerHeight+" dpr "+devicePixelRatio},300)</script>`)
	}))
	prof := t.TempDir()
	url := "http://" + ln.Addr().String() + "/"
	cmd := exec.Command(chrome, "--user-data-dir="+prof, "--no-first-run", "--no-default-browser-check", "--app="+url, "--window-size=1200,40", "--window-position=0,0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { closeBar(); time.Sleep(time.Second); cmd.Process.Kill() }()
	var h uintptr
	for i := 0; i < 60 && h == 0; i++ {
		time.Sleep(500 * time.Millisecond)
		h = findBar()
	}
	if h == 0 {
		t.Fatal("no window")
	}
	time.Sleep(2 * time.Second)
	dumpWin(t, "before dock", h)
	shot(t, "1-before")
	abd := dock(h)
	defer undock(abd)
	time.Sleep(3 * time.Second)
	fit(h, abd.rc)
	time.Sleep(time.Second)
	dumpWin(t, "after dock", h)
	shot(t, "2-after")
	p, ok := pageRect(h)
	t.Logf("page %v ok %v want %v", p, ok, abd.rc)
	if !ok || p != abd.rc {
		t.Fatalf("the page is at %v (found %v), the bar's place is %v", p, ok, abd.rc)
	}
}
