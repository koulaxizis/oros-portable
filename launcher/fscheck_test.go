package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProfileDirUsable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	if err := profileDirUsable(dir); err != nil {
		t.Fatalf("a normal folder should hold a profile: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".oros-link-test")); !os.IsNotExist(err) {
		t.Fatalf("the test link was left behind: %v", err)
	}
}
