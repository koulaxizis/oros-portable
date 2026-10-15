package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSmall(t *testing.T) {
	dir := t.TempDir()
	r, err := run(config{dir: dir, sizeMiB: 8, seconds: 0.2, files: 20}, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if r.SeqWriteMBs <= 0 || r.SeqReadMBs <= 0 || r.RandIOPS <= 0 || r.FilesPerSec <= 0 {
		t.Fatalf("missing measurement: %+v", r)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "sticktest-tmp-*"))
	if len(left) != 0 {
		t.Fatalf("temporary files left behind: %v", left)
	}
}

func TestVerdict(t *testing.T) {
	good := result{SeqWriteMBs: 60, SeqReadMBs: 150, RandIOPS: 400, FilesPerSec: 300}
	if v := verdict(good); !strings.HasPrefix(v, "GOOD") {
		t.Fatal(v)
	}
	slow := good
	slow.RandIOPS = 5
	if v := verdict(slow); !strings.HasPrefix(v, "POOR") {
		t.Fatal(v)
	}
	mid := good
	mid.SeqReadMBs = 40
	if v := verdict(mid); !strings.HasPrefix(v, "OK") {
		t.Fatal(v)
	}
}

func TestAppendResults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.txt")
	for i := 0; i < 2; i++ {
		if err := appendResults(p, format("Test stick", result{SeqWriteMBs: 1})); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), "Test stick") != 2 {
		t.Fatalf("results not appended:\n%s", b)
	}
}
