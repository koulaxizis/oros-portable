//go:build !windows

package main

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ulikunitz/xz"
)

// Linux: Tor Browser comes as a .tar.xz with one tor-browser/ folder. It
// is unpacked here (no xz or tar needed on the computer) and started with
// its own start-tor-browser script.

var torPlatformKeys = []string{"linux-x86_64", "linux64"}

const torDownloadExt = ".tar.xz"

func torProgram(root string) string { return filepath.Join(root, "Browser", "start-tor-browser") }

func torCommand(p string) *exec.Cmd {
	cmd := exec.Command(p)
	cmd.Dir = filepath.Dir(p)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// torInstall unpacks the tarball's tor-browser/ folder as dest.
func torInstall(file, dest string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	x, err := xz.NewReader(f)
	if err != nil {
		return fmt.Errorf("unpacking Tor Browser: %v", err)
	}
	work := dest + ".x"
	_ = os.RemoveAll(work)
	defer os.RemoveAll(work)
	if err := untar(x, work); err != nil {
		return fmt.Errorf("unpacking Tor Browser: %v", err)
	}
	return os.Rename(filepath.Join(work, "tor-browser"), dest)
}

// untar writes a tar stream under dir. Names that would leave dir, and
// links that point out of it, are refused. Where the file system has no
// symbolic links (FAT, exFAT sticks) a link to a file becomes a copy.
func untar(r io.Reader, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	inside := func(p string) bool {
		rel, err := filepath.Rel(dir, p)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	type link struct{ path, target string }
	var copies []link
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		p := filepath.Join(dir, filepath.FromSlash(h.Name))
		if !inside(p) || filepath.IsAbs(h.Name) {
			return fmt.Errorf("unexpected name %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(0o644)
			if h.FileInfo().Mode()&0o111 != 0 {
				mode = 0o755
			}
			out, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, io.LimitReader(tr, h.Size))
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(h.Linkname) || !inside(filepath.Join(filepath.Dir(p), h.Linkname)) {
				return fmt.Errorf("unexpected link %q", h.Name)
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, p); err != nil {
				copies = append(copies, link{p, filepath.Join(filepath.Dir(p), h.Linkname)})
			}
		default:
			// hard links, devices and the like are not part of Tor Browser
		}
	}
	for _, l := range copies {
		st, err := os.Stat(l.target)
		if err == nil && !st.Mode().IsRegular() {
			err = errors.New("not a file")
		}
		if err == nil {
			err = copyFile(l.target, l.path, st) // vault.go
		}
		if err != nil {
			return fmt.Errorf("link %s: %v", filepath.Base(l.path), err)
		}
	}
	return nil
}
