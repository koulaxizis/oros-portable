package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The embedded tray icons are copies of the packaging icons.
func TestTrayIconsMatchPackaging(t *testing.T) {
	for _, c := range [][2]string{{"icons/oros.png", "../packaging/linux/oros.png"}, {"icons/oros.ico", "../packaging/windows/oros.ico"}} {
		a, err := os.ReadFile(c[0])
		must(t, err)
		b, err := os.ReadFile(c[1])
		must(t, err)
		if !bytes.Equal(a, b) {
			t.Fatalf("%s differs from %s: copy it again", c[0], c[1])
		}
	}
}

func TestQuickWindowURLAndSize(t *testing.T) {
	if quickURL != "http://localhost:47800/#quick-win" {
		t.Fatal(quickURL)
	}
	args := strings.Join(browserArgs("/p", quickURL, quickArgs...), " ")
	for _, want := range []string{"--app=http://localhost:47800/#quick-win", "--window-size=640,460", "--user-data-dir=/p"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %s in %s", want, args)
		}
	}
	if a := strings.Join(browserArgs("/p", appURL), " "); strings.Contains(a, "window-size") {
		t.Fatal("a normal window has no fixed size")
	}
}

// The tray switches to a downloaded orOS without restarting the server.
func TestDirHandlerFollowsTheFolder(t *testing.T) {
	a, b := testApp(t), testApp(t)
	must(t, os.WriteFile(filepath.Join(b, "index.html"), []byte("new"), 0o644))
	dir := a
	h := newDirHandler(func() string { return dir })
	if w := get(h, "localhost:47800", "/"); !strings.Contains(w.Body.String(), "orOS") {
		t.Fatal(w.Body.String())
	}
	dir = b
	if w := get(h, "localhost:47800", "/"); w.Body.String() != "new" {
		t.Fatal(w.Body.String())
	}
	if w := get(h, "evil.example:47800", "/"); w.Code != 403 {
		t.Fatal("foreign host must stay refused")
	}
}

func TestTrayWords(t *testing.T) {
	for lang, want := range map[string]string{"el_GR.UTF-8": "Έξοδος", "de_DE.UTF-8": "Beenden", "en_US.UTF-8": "Quit", "C": "Quit"} {
		t.Setenv("LC_ALL", lang)
		if runtime.GOOS != "windows" {
			if got := word("Quit", "Έξοδος", "Beenden"); got != want {
				t.Fatalf("%s: %s", lang, got)
			}
		}
	}
}

func TestDesktopEntriesHaveTheQuickAction(t *testing.T) {
	b, err := os.ReadFile("../packaging/linux/oros.desktop")
	must(t, err)
	for _, want := range []string{"Actions=quick;", "[Desktop Action quick]", "Exec=oros quick", "Name[el]="} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("oros.desktop lacks %q", want)
		}
	}
	d := t.TempDir()
	desktop, icon := filepath.Join(d, "a.desktop"), filepath.Join(d, "i.png")
	must(t, writeAppImageMenu("/x/orOS.AppImage", "missing.png", desktop, icon))
	b, err = os.ReadFile(desktop)
	must(t, err)
	if !strings.Contains(string(b), "Exec=\"/x/orOS.AppImage\" quick") || !strings.Contains(string(b), "Actions=quick;") {
		t.Fatalf("AppImage entry:\n%s", b)
	}
}

func TestSetupRelaunchesTheTray(t *testing.T) {
	b, err := os.ReadFile("../packaging/windows/orOS.iss")
	must(t, err)
	s := string(b)
	for _, want := range []string{
		`Name: "autostart"; Description: "{cm:AutoStart}"; Flags: unchecked`,
		`ValueName: "orOS"; ValueData: """{app}\orOS.exe"" tray"; Tasks: autostart; Flags: uninsdeletevalue`,
		`Parameters: "tray"; Flags: nowait; Check: RelaunchTray`,
		`RegDeleteValue(HKEY_CURRENT_USER, 'Software\Microsoft\Windows\CurrentVersion\Run', 'orOS')`,
		`Parameters: "/F /IM orOS.exe"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("orOS.iss lacks %s", want)
		}
	}
	if strings.Join(relaunchArgs(true), " ") != "tray" || relaunchArgs(false) != nil {
		t.Fatal("relaunchArgs")
	}
}
