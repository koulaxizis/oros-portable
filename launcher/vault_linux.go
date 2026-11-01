package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// sysVault: fscrypt on the data partition of orOS USB. The folder that
// holds the vaults is made (once, as root) by "oros-system vault-base",
// which prints it; it must be ours and closed to others.
type sysVault struct {
	helper string
	mu     sync.Mutex
	base   string
}

func newSysVault(helper string) vaultFS {
	if helper == "" {
		return nil
	}
	return &sysVault{helper: helper}
}

func (v *sysVault) Base() (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.base != "" {
		if fi, err := os.Stat(v.base); err == nil && fi.IsDir() {
			return v.base, nil
		}
		v.base = ""
	}
	out, err := exec.Command("sudo", "-n", v.helper, "vault-base").Output()
	if err != nil {
		return "", errors.New("vault: no data partition for encrypted users")
	}
	p := strings.TrimSpace(string(out))
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return "", errors.New("vault: bad folder")
	}
	var st unix.Stat_t
	if err := unix.Lstat(p, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR ||
		int(st.Uid) != os.Getuid() || st.Mode&0o077 != 0 {
		return "", errors.New("vault: folder not ours alone")
	}
	v.base = p
	return p, nil
}

func (v *sysVault) Free(path string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func (v *sysVault) Add(dir string, key []byte) (fsKeyID, error) { return fsAddKey(dir, key) }
func (v *sysVault) SetPolicy(dir string, id fsKeyID) error      { return fsSetPolicy(dir, id) }
func (v *sysVault) Remove(base string, id fsKeyID) (bool, error) {
	return fsRemoveKey(base, id)
}
