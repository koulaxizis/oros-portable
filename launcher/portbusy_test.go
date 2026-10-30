package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func tcpRow(n int, put func(row []byte)) []byte {
	b := make([]byte, n)
	put(b)
	return b
}

func netPort(p int) uint32 { return uint32(uint16(p)>>8 | uint16(p)<<8) }

func TestParseTCPTable(t *testing.T) {
	v4 := make([]byte, 4)
	binary.LittleEndian.PutUint32(v4, 2)
	for _, r := range [][2]int{{80, 11}, {47800, 4242}} {
		v4 = append(v4, tcpRow(24, func(b []byte) {
			binary.LittleEndian.PutUint32(b[8:], netPort(r[0]))
			binary.LittleEndian.PutUint32(b[20:], uint32(r[1]))
		})...)
	}
	if pid, ok := parseTCPTable(v4, false, 47800); !ok || pid != 4242 {
		t.Fatalf("v4: %d %v", pid, ok)
	}
	if _, ok := parseTCPTable(v4, false, 47811); ok {
		t.Fatal("v4 found a port that is not there")
	}
	v6 := make([]byte, 4)
	binary.LittleEndian.PutUint32(v6, 1)
	v6 = append(v6, tcpRow(56, func(b []byte) {
		binary.LittleEndian.PutUint32(b[20:], netPort(47800))
		binary.LittleEndian.PutUint32(b[52:], 77)
	})...)
	if pid, ok := parseTCPTable(v6, true, 47800); !ok || pid != 77 {
		t.Fatalf("v6: %d %v", pid, ok)
	}
	if _, ok := parseTCPTable(v6[:30], true, 47800); ok {
		t.Fatal("a cut table must not be read past its end")
	}
}

func TestListenInodes(t *testing.T) {
	table := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:BAB8 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 5551 1 0000000000000000 100 0 0 10 0
   1: 0100007F:BAB8 0100007F:D431 01 00000000:00000000 00:00000000 00000000  1000        0 5552 1 0000000000000000 20 4 30 10 -1
   2: 0100007F:BAC3 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 5553 1 0000000000000000 100 0 0 10 0`
	got := listenInodes(table, 47800)
	if len(got) != 1 || got[0] != "5551" {
		t.Fatalf("got %v", got)
	}
}

func TestFindPortOwnerFindsThisProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc only on Linux")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	defer ln.Close()
	o, ok := findPortOwner(ln.Addr().(*net.TCPAddr).Port)
	if !ok || o.pid != os.Getpid() {
		t.Fatalf("got %+v %v, want pid %d", o, ok, os.Getpid())
	}
	self, _ := os.Executable()
	if o.exe != self {
		t.Fatalf("exe %q, want %q", o.exe, self)
	}
}

func TestOwnerKinds(t *testing.T) {
	for _, c := range []struct {
		exe          string
		own, isDrive bool
	}{
		{`C:\Users\x\AppData\Local\Programs\orOS\orOS.exe`, true, false},
		{`E:\orOS-Portable\orOS.exe`, true, false},
		{"/opt/oros/orOS", true, false},
		{`C:\Users\x\AppData\Local\Programs\orOS Drive\orosdrive.exe`, false, true},
		{`C:\Program Files\Some\server.exe`, false, false},
		{"", false, false},
	} {
		o := portOwner{pid: 5, exe: c.exe}
		if o.ownLauncher() != c.own || o.drive() != c.isDrive {
			t.Errorf("%q: own %v drive %v", c.exe, o.ownLauncher(), o.drive())
		}
	}
}

func TestPortBusyMessages(t *testing.T) {
	t.Setenv("LC_ALL", "en_US.UTF-8")
	if runtime.GOOS == "windows" {
		t.Skip("language comes from the Windows display language")
	}
	cases := []struct {
		o        portOwner
		found    bool
		reserved bool
		want     string
	}{
		{portOwner{9, `C:\x\orosdrive.exe`}, true, false, "Dropbox sign-in"},
		{portOwner{9, `C:\x\server.exe`}, true, false, "used by server.exe (process 9)"},
		{portOwner{}, false, true, "net start winnat"},
		{portOwner{}, false, false, "another program"},
	}
	for _, c := range cases {
		if m := portBusyMessage(c.o, c.found, c.reserved); !strings.Contains(m, c.want) {
			t.Errorf("%+v: %q lacks %q", c.o, m, c.want)
		}
	}
	t.Setenv("LC_ALL", "el_GR.UTF-8")
	if m := portBusyMessage(portOwner{}, false, false); !strings.Contains(m, "θύρα 47800") {
		t.Errorf("Greek: %q", m)
	}
}

// A launcher or tray that is closing frees the port within portWait: the
// new start waits for it instead of failing.
func TestWaitForPortTakesAFreedPort(t *testing.T) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("port %d not free here: %v", port, err)
	}
	go func() { time.Sleep(time.Second); ln.Close() }()
	defer func(w time.Duration) { portWait = w }(portWait)
	portWait = 5 * time.Second
	srv, err := waitForPort(testApp(t))
	if err != nil {
		t.Fatal(err)
	}
	shutdown(srv)
}

// Another orOS answers on the port: claimPort leaves it and the caller
// opens a window only.
func TestClaimPortJoinsARunningOrOS(t *testing.T) {
	first, err := startServer(testApp(t))
	if err != nil {
		t.Skipf("port %d not free here: %v", port, err)
	}
	defer shutdown(first)
	srv, err := claimPort(testApp(t))
	if err != nil || srv != nil {
		t.Fatalf("got %v %v, want to join", srv, err)
	}
}
