//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Windows "This computer" rules that need Windows itself (CI: launcher-windows).

func TestWindowsNames(t *testing.T) {
	for name, want := range map[string]bool{
		"notes.txt": true, "Ημερολόγιο": true, "a.b.c": true, "COM10": true,
		"": false, "a:b": false, "what?": false, "x*": false, "trail.": false, "trail ": false,
		"CON": false, "con.txt": false, "NUL": false, "COM1": false, "lpt9.log": false, "tab\tname": false,
	} {
		if got := nameOK(name); got != want {
			t.Errorf("nameOK(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestWindowsWithin(t *testing.T) {
	for _, c := range []struct {
		path, root string
		want       bool
	}{
		{`C:\Users\a`, `C:\Users\a`, true},
		{`c:\users\A\Docs`, `C:\Users\a`, true},
		{`C:\Users\ab`, `C:\Users\a`, false},
		{`D:\x`, `D:\`, true},
		{`E:\x`, `D:\`, false},
	} {
		if got := within(c.path, c.root); got != c.want {
			t.Errorf("within(%q, %q) = %v", c.path, c.root, got)
		}
	}
}

func TestWindowsDrives(t *testing.T) {
	c := newComputerFS(t.TempDir(), hostMedia(), "")
	sys := os.Getenv("SystemDrive")[:1]
	if root, ok := c.diskRoot(sys); !ok || root != sys+`:\` {
		t.Fatalf("diskRoot(%s) = %q, %v", sys, root, ok)
	}
	for _, bad := range []string{"c", "CC", "1", ""} {
		if _, ok := c.diskRoot(bad); ok {
			t.Errorf("diskRoot(%q) accepted", bad)
		}
	}
	found := false
	for _, d := range c.disks() {
		found = found || d.Path == "/disks/"+sys
	}
	if !found {
		t.Fatalf("system drive missing from %+v", c.disks())
	}
	if p, err := c.mount(sys+":", false); err != nil || p != "/disks/"+sys {
		t.Fatalf("mount = %q, %v", p, err)
	}
	if _, err := c.mount(sys+":", true); err == nil {
		t.Fatal("unmount of the system drive allowed")
	}
}

func TestWindowsHidden(t *testing.T) {
	sys := os.Getenv("SystemDrive") + `\`
	for _, f := range []string{"Windows", "program files", "ProgramData"} {
		if !systemHidden(sys, []string{f}) {
			t.Errorf("%s is shown", f)
		}
	}
	if systemHidden(sys, []string{"Users"}) {
		t.Error("Users is hidden")
	}
	dir := t.TempDir()
	h := filepath.Join(dir, "secret")
	if err := os.WriteFile(h, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	u, _ := windows.UTF16PtrFromString(h)
	if err := windows.SetFileAttributes(u, windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatal(err)
	}
	if !systemHidden(dir, []string{"secret"}) {
		t.Error("a hidden file is shown")
	}
	if systemHidden(dir, []string{"plain"}) {
		t.Error("a missing file counts as hidden")
	}
}

func TestWindowsDiskListPairsWithRoots(t *testing.T) {
	if driveName("C", "") != "C:" || driveName("D", "Data") != "Data (D:)" {
		t.Fatal("driveName")
	}
	c := newComputerFS(t.TempDir(), hostMedia(), "")
	raw, err := c.diskList()
	if err != nil {
		t.Fatal(err)
	}
	var vols []struct{ Path, Label, Mount string }
	if err := json.Unmarshal(raw, &vols); err != nil || len(vols) == 0 {
		t.Fatalf("%s %v", raw, err)
	}
	for _, v := range vols {
		if v.Mount != "/disks/"+v.Path[:1] || !strings.Contains(v.Label, v.Path) {
			t.Errorf("volume %+v does not pair with /disks", v)
		}
	}
}
