//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// Disks for "This computer" in orOS Portable and the desktop apps on Linux.
// The launcher is its own disk helper here ("orOS disks", "orOS mount"),
// in the same JSON as orOS USB's oros-system, but it opens disks the way
// the desktop does: udisks (udisksctl), no root, under /run/media/<user>
// or /media/<user>. It lists partitions with a readable filesystem of at
// least 1 GiB that are not part of the running system (/, /boot, /home,
// /usr, /var or swap). The computer's own NTFS disks (Windows) open
// read-only, because writing while Windows hibernates can damage them.

// hostVolume is one volume, as oros-system's "disks" prints it.
type hostVolume struct {
	Path     string `json:"path"`
	Label    string `json:"label"`
	FSType   string `json:"fstype"`
	Size     int64  `json:"size"`
	Model    string `json:"model"`
	USB      bool   `json:"usb"`
	Mount    string `json:"mount,omitempty"`
	ReadOnly bool   `json:"readonly,omitempty"`
	Internal bool   `json:"internal"`
}

var readableFS = map[string]bool{"vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "ext4": true, "ext3": true, "ext2": true, "btrfs": true, "xfs": true}

// systemMounts are the mount points that make a disk part of the running
// system.
var systemMounts = map[string]bool{"/": true, "/boot": true, "/boot/efi": true, "/efi": true, "/home": true, "/usr": true, "/var": true, "[SWAP]": true}

// hostMedia is where udisks opens disks for this user.
func hostMedia() string {
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	if _, err := os.Stat("/run/media"); err == nil {
		return filepath.Join("/run/media", name)
	}
	return filepath.Join("/media", name)
}

var lsblkJSON = func() (string, error) {
	out, err := exec.Command("lsblk", "-J", "-l", "-b", "-o", "NAME,PATH,PKNAME,TYPE,SIZE,FSTYPE,LABEL,MOUNTPOINT,RM,HOTPLUG,TRAN,MODEL").Output()
	return string(out), err
}

func pickHostVolumes(js, media string) ([]hostVolume, error) {
	var l struct {
		Blockdevices []struct {
			Name, Path, Type string
			PKName           *string `json:"pkname"`
			Size             json.Number
			FSType           *string `json:"fstype"`
			Label            *string
			Mountpoint       *string
			RM, Hotplug      any
			Tran, Model      *string
		} `json:"blockdevices"`
	}
	d := json.NewDecoder(strings.NewReader(js))
	d.UseNumber()
	if err := d.Decode(&l); err != nil {
		return nil, err
	}
	str := func(s *string) string {
		if s == nil {
			return ""
		}
		return strings.TrimSpace(*s)
	}
	yes := func(v any) bool { s := fmt.Sprint(v); return s == "true" || s == "1" }
	type diskInfo struct {
		model string
		usb   bool
	}
	disks := map[string]diskInfo{}
	system := map[string]bool{}
	for _, b := range l.Blockdevices {
		if b.Type == "disk" {
			disks[b.Name] = diskInfo{str(b.Model), str(b.Tran) == "usb" || yes(b.RM) || yes(b.Hotplug)}
		}
		if systemMounts[str(b.Mountpoint)] {
			system[b.Name] = true
			if p := str(b.PKName); p != "" {
				system[p] = true
			}
		}
	}
	// A partition inside an encrypted or LVM volume names its parent chain;
	// one level is enough to keep the system disk's other partitions out.
	for _, b := range l.Blockdevices {
		if p := str(b.PKName); p != "" && system[b.Name] {
			system[p] = true
		}
	}
	out := []hostVolume{}
	for _, b := range l.Blockdevices {
		parent := str(b.PKName)
		if b.Type != "part" && b.Type != "disk" {
			continue
		}
		fs := str(b.FSType)
		size, _ := b.Size.Int64()
		if !readableFS[fs] || size < 1<<30 || system[b.Name] || (parent != "" && system[parent]) {
			continue
		}
		mp := str(b.Mountpoint)
		if mp != "" && !strings.HasPrefix(mp, media+"/") {
			continue // the computer uses it somewhere else: leave it alone
		}
		info := disks[b.Name]
		if b.Type == "part" {
			info = disks[parent]
		}
		out = append(out, hostVolume{Path: b.Path, Label: str(b.Label), FSType: fs, Size: size, Model: info.model,
			USB: info.usb, Internal: !info.usb, Mount: mp})
	}
	return out, nil
}

func hostVolumes() ([]hostVolume, error) {
	js, err := lsblkJSON()
	if err != nil {
		return nil, errors.New("the disks cannot be listed (lsblk)")
	}
	return pickHostVolumes(js, hostMedia())
}

// disksMain is "orOS disks".
func disksMain() int {
	vs, err := hostVolumes()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		return 1
	}
	b, _ := json.Marshal(vs)
	fmt.Println(string(b))
	return 0
}

var udisksctl = func(args ...string) (string, error) {
	cmd := exec.Command("udisksctl", append(args, "--no-user-interaction")...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// mountMain is "orOS mount --dev /dev/sdX1 [--unmount]": it prints the
// folder the disk opened in.
func mountMain(args []string) int {
	dev, off := "", false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dev":
			if i+1 < len(args) {
				dev = args[i+1]
				i++
			}
		case "--unmount":
			off = true
		}
	}
	dir, err := hostMount(dev, off)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error: "+err.Error())
		return 1
	}
	fmt.Println(dir)
	return 0
}

func hostMount(dev string, off bool) (string, error) {
	vs, err := hostVolumes()
	if err != nil {
		return "", err
	}
	var v *hostVolume
	for i := range vs {
		if vs[i].Path == dev {
			v = &vs[i]
		}
	}
	if v == nil {
		return "", errors.New("this disk cannot be opened")
	}
	if off {
		if v.Mount == "" {
			return "", nil
		}
		if out, err := udisksctl("unmount", "-b", dev); err != nil {
			return "", udisksError(out, err)
		}
		return "", nil
	}
	if v.Mount != "" {
		return v.Mount, nil
	}
	args := []string{"mount", "-b", dev}
	if v.Internal && strings.HasPrefix(v.FSType, "ntfs") {
		args = append(args, "-o", "ro")
	}
	out, err := udisksctl(args...)
	if err != nil {
		return "", udisksError(out, err)
	}
	// "Mounted /dev/sdb1 at /run/media/chris/STICK"
	if i := strings.LastIndex(out, " at "); i >= 0 {
		return strings.TrimSuffix(strings.TrimSpace(out[i+4:]), "."), nil
	}
	return "", errors.New("the disk did not open")
}

func udisksError(out string, err error) error {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if l := strings.TrimSpace(lines[len(lines)-1]); l != "" {
		if strings.Contains(l, "busy") {
			return errors.New("busy: a program still uses this disk")
		}
		return errors.New(l)
	}
	return err
}

func diskHelper(cmd string, args []string) int {
	if cmd == "disks" {
		return disksMain()
	}
	return mountMain(args)
}
