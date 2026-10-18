package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeApp(t *testing.T, dir, ver string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html>"), 0o644)
	js := "(function () {\n  var APP_VERSION = \"" + ver + "\";   // bump\n})();\n"
	_ = os.WriteFile(filepath.Join(dir, "shell.js"), []byte(js), 0o644)
}

func TestPortableLayoutKeepsEverythingNextToLauncher(t *testing.T) {
	root := t.TempDir()
	l := newLayout(root)
	if l.installed || l.data != filepath.Join(root, "data") || l.updates != root || l.appDir() != filepath.Join(root, "app") {
		t.Fatalf("portable layout: %+v app=%s", l, l.appDir())
	}
}

func TestInstalledLayoutUsesUserDataFolder(t *testing.T) {
	root, data := t.TempDir(), t.TempDir()
	t.Setenv("OROS_DATA_DIR", data)
	_ = os.WriteFile(filepath.Join(root, installedMarker), nil, 0o644)
	writeApp(t, filepath.Join(root, "app"), "2.67.00")
	l := newLayout(root)
	if !l.installed || l.data != data || l.updates != data {
		t.Fatalf("installed layout: %+v", l)
	}
	if got := l.appDir(); got != filepath.Join(root, "app") {
		t.Fatalf("no update yet: serves %s", got)
	}
	writeApp(t, filepath.Join(data, "app"), "2.68.00")
	if got := l.appDir(); got != filepath.Join(data, "app") {
		t.Fatalf("newer update: serves %s", got)
	}
	writeApp(t, filepath.Join(root, "app"), "2.70.01")
	if got := l.appDir(); got != filepath.Join(root, "app") {
		t.Fatalf("newer setup: serves %s", got)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.67.00", "2.67.00", 0}, {"2.67.01", "2.67.00", 1}, {"2.9.0", "2.10.0", -1},
		{"3.0", "2.99.99", 1}, {"", "0.0.1", -1}, {"", "", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestAppImageMenuEntryQuotesPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "oros.png")
	_ = os.WriteFile(src, []byte("png"), 0o644)
	desktop, icon := filepath.Join(dir, "a", "oros.desktop"), filepath.Join(dir, "i", "oros.png")
	img := `/home/u/My "Apps"/$orOS.AppImage`
	if err := writeAppImageMenu(img, src, desktop, icon); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(desktop)
	want := `Exec="/home/u/My \\"Apps\\"/\\$orOS.AppImage"`
	if !strings.Contains(string(b), want+"\n") {
		t.Fatalf("Exec line not quoted:\n%s", b)
	}
	if got, _ := os.ReadFile(icon); string(got) != "png" {
		t.Fatal("icon not copied")
	}
}
