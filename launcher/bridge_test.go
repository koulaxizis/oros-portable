package main

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBridgeCodeInPingAndBridgeCode(t *testing.T) {
	withFake(t)
	lay := portableLayout(t)
	if _, ok := handleSys(lay, sysRequest{Cmd: "ping"})["bridgeCode"]; ok {
		t.Fatal("no code file: the ping carries no code")
	}
	for _, bad := range []string{"12345\n", "1234567\n", "12a456\n", ""} {
		_ = os.WriteFile(bridgeCodeFile(lay), []byte(bad), 0o644)
		if c := bridgeCode(lay); c != "" {
			t.Fatalf("%q read as code %q", bad, c)
		}
	}
	_ = os.WriteFile(bridgeCodeFile(lay), []byte("042719\n"), 0o644)
	if r := handleSys(lay, sysRequest{Cmd: "ping"}); r["bridgeCode"] != "042719" || r["ok"] != true {
		t.Fatalf("ping: %v", r)
	}
	if r := handleSys(lay, sysRequest{Cmd: "bridge.code"}); r["code"] != "042719" || r["ok"] != true {
		t.Fatalf("bridge.code: %v", r)
	}
	_ = os.Remove(bridgeCodeFile(lay))
	if r := handleSys(lay, sysRequest{Cmd: "bridge.code"}); r["code"] != "" {
		t.Fatalf("bridge.code without a file: %v", r)
	}
}

func TestNoBridgeInThisEdition(t *testing.T) {
	lay := portableLayout(t)
	if bridgeExe(lay) != "" {
		t.Fatal("no bridge/ folder: no bridge")
	}
	startBridge(lay) // nothing to start, nothing breaks
	stopBridge(lay)
}

// A stand-in bridge (a shell script) is started with --bundled, orOS's
// own address and the code file, and ended with stopBridge.
func TestBridgeStartsAndStops(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the stand-in is a shell script")
	}
	if bridgeAnswers() {
		t.Skip("a bridge runs on this computer")
	}
	lay := portableLayout(t)
	dir := filepath.Join(lay.root, "bridge", "linux")
	_ = os.MkdirAll(dir, 0o755)
	args := filepath.Join(t.TempDir(), "args")
	script := "#!/bin/sh\necho \"$@\" > " + args + "\necho 123456 > \"$5\"\necho ready\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "oros-bridge"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	startBridge(lay)
	defer stopBridge(lay)
	deadline := time.Now().Add(5 * time.Second)
	for bridgeCode(lay) == "" {
		if time.Now().After(deadline) {
			t.Fatal("the bridge did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	b, _ := os.ReadFile(args)
	want := "--bundled --allow-origin http://localhost:47800 --code-file " + bridgeCodeFile(lay)
	if strings.TrimSpace(string(b)) != want {
		t.Fatalf("args %q, want %q", b, want)
	}
	startBridge(lay) // once only
	bridgeState.Lock()
	pid := bridgeState.cmd.Process.Pid
	bridgeState.Unlock()
	stopBridge(lay)
	if bridgeCode(lay) != "" {
		t.Fatal("the code file goes with the bridge")
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat("/proc/" + strconv.Itoa(pid)); err != nil {
			break
		}
		if st, _ := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); strings.Contains(string(st), ") Z ") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the bridge still runs")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if log, _ := os.ReadFile(filepath.Join(lay.data, "bridge.log")); !strings.Contains(string(log), "ready") {
		t.Fatalf("bridge.log: %q", log)
	}
}

// A bridge the person runs already (its port answers) is used: none is started.
func TestBridgeAlreadyRunning(t *testing.T) {
	l, err := net.Listen("tcp", bridgeAddr)
	if err != nil {
		t.Skip("the bridge's port is taken")
	}
	defer l.Close()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	lay := portableLayout(t)
	dir := filepath.Join(lay.root, "bridge", runtime.GOOS)
	_ = os.MkdirAll(dir, 0o755)
	name := "oros-bridge"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	_ = os.WriteFile(filepath.Join(dir, name), []byte("never run"), 0o755)
	startBridge(lay)
	bridgeState.Lock()
	started := bridgeState.cmd != nil
	bridgeState.Unlock()
	if started {
		stopBridge(lay)
		t.Fatal("a second bridge was started")
	}
}
