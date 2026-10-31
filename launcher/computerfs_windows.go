//go:build windows

package main

// The Windows side of computerfs.go ("This computer" in orOS Portable and
// the desktop app on Windows):
//   - "/home" is the user's folder, "/disks/C" … the computer's fixed and
//     removable drives. The whole C: drive is there and writable, but
//     Windows' own folders (Windows, Program Files, ProgramData and the
//     like) and every file or folder Windows marks hidden or system are
//     never listed or reachable.
//   - Removing moves to the Windows Recycle Bin (SHFileOperation with
//     undo), and Files offers a button that opens the Recycle Bin to put
//     something back. A drive without a Recycle Bin (a USB stick) refuses,
//     so nothing is deleted for good without the user knowing.
//   - Names Windows cannot hold (CON, a:b, a trailing dot) are refused.

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32           = windows.NewLazySystemDLL("shell32.dll")
	procSHFileOp      = shell32.NewProc("SHFileOperationW")
	errNoRecycle      = fsErr(http.StatusConflict, "norecycle", "this drive has no Recycle Bin")
	errUseRecycleBin  = fsErr(http.StatusBadRequest, "recyclebin", "put it back from the Windows Recycle Bin")
	windowsOwnFolders = map[string]bool{
		"windows": true, "program files": true, "program files (x86)": true, "programdata": true,
		"$recycle.bin": true, "system volume information": true, "recovery": true, "perflogs": true,
		"$winreagent": true, "config.msi": true, "msocache": true, "intel": true, "boot": true,
		"documents and settings": true, "$windows.~bt": true, "$windows.~ws": true, "windows.old": true,
	}
)

func isMountpoint(string) bool { return true }

func hostMedia() string { return "" }

func diskHelper(string, []string) int { return 1 }

func driveType(letter string) uint32 {
	p, _ := windows.UTF16PtrFromString(letter + `:\`)
	return windows.GetDriveType(p)
}

// usableDrive: the computer's own disks and removable ones, not network,
// CD or RAM drives.
func usableDrive(letter string) bool {
	t := driveType(letter)
	return t == windows.DRIVE_FIXED || t == windows.DRIVE_REMOVABLE
}

func driveLetters() []string {
	mask, _ := windows.GetLogicalDrives()
	var out []string
	for i := 0; i < 26; i++ {
		if mask&(1<<i) != 0 {
			if l := string(rune('A' + i)); usableDrive(l) {
				out = append(out, l)
			}
		}
	}
	return out
}

func volumeInfo(root string) (label, fs string, ro bool) {
	p, _ := windows.UTF16PtrFromString(root)
	name := make([]uint16, 261)
	fsName := make([]uint16, 261)
	var flags uint32
	if err := windows.GetVolumeInformation(p, &name[0], uint32(len(name)), nil, nil, &flags, &fsName[0], uint32(len(fsName))); err != nil {
		return "", "", false
	}
	return windows.UTF16ToString(name), windows.UTF16ToString(fsName), flags&windows.FILE_READ_ONLY_VOLUME != 0
}

func diskSpace(dir string) (free, total int64, ro bool) {
	p, _ := windows.UTF16PtrFromString(dir)
	var avail, tot, all uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &tot, &all); err != nil {
		return 0, 0, false
	}
	_, _, ro = volumeInfo(filepath.VolumeName(dir) + `\`)
	return int64(avail), int64(tot), ro
}

func (c *computerFS) disks() []rootInfo {
	out := []rootInfo{}
	for _, l := range driveLetters() {
		root := l + `:\`
		label, _, _ := volumeInfo(root)
		name := l + ":"
		if label != "" {
			name = label + " (" + l + ":)"
		}
		out = append(out, c.rootInfo("/disks/"+l, name, root))
	}
	return out
}

func (c *computerFS) diskRoot(name string) (string, bool) {
	if len(name) != 1 || name[0] < 'A' || name[0] > 'Z' {
		return "", false
	}
	return name + `:\`, usableDrive(name)
}

// within: path is root or inside it, as Windows compares names.
func within(path, root string) bool {
	if strings.EqualFold(path, root) {
		return true
	}
	r := strings.TrimSuffix(root, `\`) + `\`
	return len(path) > len(r) && strings.EqualFold(path[:len(r)], r)
}

func crossDevice(err error) bool { return errors.Is(err, windows.ERROR_NOT_SAME_DEVICE) }

var reservedNames = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true}

// nameOK refuses names Windows cannot hold or treats specially.
func nameOK(name string) bool {
	if name == "" || strings.ContainsAny(name, `<>:"|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	for _, r := range name {
		if r < 32 {
			return false
		}
	}
	base := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if reservedNames[base] || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9') {
		return false
	}
	return true
}

// systemHidden: Windows' own folders at the top of the system drive, and
// anything marked hidden or system on the way.
func systemHidden(root string, rest []string) bool {
	if len(rest) > 0 && strings.EqualFold(filepath.VolumeName(root), os.Getenv("SystemDrive")) &&
		len(strings.TrimRight(root, `\`)) == 2 && windowsOwnFolders[strings.ToLower(rest[0])] {
		return true
	}
	p := root
	for _, s := range rest {
		p = filepath.Join(p, s)
		u, err := windows.UTF16PtrFromString(p)
		if err != nil {
			return true
		}
		a, err := windows.GetFileAttributes(u)
		if err != nil {
			return false // not there (yet): nothing further to check
		}
		if a&(windows.FILE_ATTRIBUTE_HIDDEN|windows.FILE_ATTRIBUTE_SYSTEM) != 0 {
			return true
		}
	}
	return false
}

// SHFILEOPSTRUCTW
type shFileOp struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom, pTo            *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete         = 3
	fofSilent        = 0x0004
	fofNoConfirm     = 0x0010
	fofAllowUndo     = 0x0040
	fofNoErrorUI     = 0x0400
	fofNoConfirmMkdr = 0x0200
)

// trash moves p to the Recycle Bin. The id is empty: Windows keeps it.
func (c *computerFS) trash(p place) (string, error) {
	if driveType(strings.TrimSuffix(filepath.VolumeName(p.real), ":")) != windows.DRIVE_FIXED {
		return "", errNoRecycle
	}
	from, err := windows.UTF16FromString(p.real)
	if err != nil {
		return "", errBadPath
	}
	from = append(from, 0) // a double-NUL-terminated list of one
	op := shFileOp{wFunc: foDelete, pFrom: &from[0], fFlags: fofAllowUndo | fofNoConfirm | fofSilent | fofNoErrorUI | fofNoConfirmMkdr}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if r, _, _ := procSHFileOp.Call(uintptr(unsafe.Pointer(&op))); r != 0 || op.fAnyOperationsAborted != 0 {
		return "", fsErr(http.StatusInternalServerError, "io", "Windows did not move it to the Recycle Bin")
	}
	if _, err := os.Lstat(p.real); err == nil {
		return "", fsErr(http.StatusInternalServerError, "io", "Windows did not move it to the Recycle Bin")
	}
	return "", nil
}

// The Recycle Bin is Windows' own: orOS lists nothing and opens it.
func (c *computerFS) trashList() []trashItem                 { return []trashItem{} }
func (c *computerFS) restore(string, string) (string, error) { return "", errUseRecycleBin }
func (c *computerFS) purge(string, string) error             { return errUseRecycleBin }

func openRecycleBin() error {
	return hidden("explorer.exe", "shell:RecycleBinFolder").Start()
}

// diskList is the drives in oros-system's format, all of them open
// already (Windows mounts by itself); removable ones count as USB, so
// orOS's "a stick was plugged in" note works.
func (c *computerFS) diskList() (json.RawMessage, error) {
	type vol struct {
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
	out := []vol{}
	for _, l := range driveLetters() {
		root := l + `:\`
		label, fs, ro := volumeInfo(root)
		_, total, _ := diskSpace(root)
		usb := driveType(l) == windows.DRIVE_REMOVABLE
		out = append(out, vol{Path: l + ":", Label: label, FSType: strings.ToLower(fs), Size: total, USB: usb,
			Mount: root, ReadOnly: ro, Internal: !usb})
	}
	b, _ := json.Marshal(out)
	return b, nil
}

// mount: Windows has every drive open already; "mount" names its place.
func (c *computerFS) mount(dev string, off bool) (string, error) {
	l := strings.TrimSuffix(strings.ToUpper(dev), ":")
	if _, found := c.diskRoot(l); !found {
		return "", errNotFound
	}
	if off {
		return "", fsErr(http.StatusBadRequest, "badop", "use \"Safely remove\" in Windows' taskbar")
	}
	return "/disks/" + l, nil
}
