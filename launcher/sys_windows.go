//go:build windows

package main

import (
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Windows: sound through the Core Audio API (COM, no cgo), brightness
// through WMI (laptop screens; external monitors have none), battery from
// GetSystemPowerStatus, power through shutdown.exe and SetSuspendState.

type windowsOps struct{}

func hostPlatform() platformOps { return windowsOps{} }

var (
	ole32         = windows.NewLazySystemDLL("ole32.dll")
	procCoInit    = ole32.NewProc("CoInitializeEx")
	procCoUninit  = ole32.NewProc("CoUninitialize")
	procCoCreate  = ole32.NewProc("CoCreateInstance")
	kernel32Sys   = windows.NewLazySystemDLL("kernel32.dll")
	procPowerStat = kernel32Sys.NewProc("GetSystemPowerStatus")
	powrprof      = windows.NewLazySystemDLL("powrprof.dll")
	procSuspend   = powrprof.NewProc("SetSuspendState")

	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioEndpointVolume = windows.GUID{Data1: 0x5CDF2C82, Data2: 0x841E, Data3: 0x4546, Data4: [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
)

// comCall calls method i of a COM object's vtable.
func comCall(obj unsafe.Pointer, i int, args ...uintptr) uintptr {
	vtbl := *(**[32]uintptr)(obj)
	r, _, _ := syscall.SyscallN(vtbl[i], append([]uintptr{uintptr(obj)}, args...)...)
	return r
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		comCall(obj, 2) // IUnknown::Release
	}
}

// withEndpointVolume opens the default playback device's IAudioEndpointVolume.
func withEndpointVolume(fn func(vol unsafe.Pointer) error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const coinitMultithreaded = 0
	if hr, _, _ := procCoInit.Call(0, coinitMultithreaded); int32(hr) < 0 && hr != 0x80010106 {
		return errors.New("COM is not available")
	}
	defer procCoUninit.Call()
	var enum unsafe.Pointer
	const clsctxAll = 0x17
	if hr, _, _ := procCoCreate.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum))); int32(hr) < 0 || enum == nil {
		return errors.New("no sound device found")
	}
	defer comRelease(enum)
	var dev unsafe.Pointer
	// IMMDeviceEnumerator::GetDefaultAudioEndpoint(eRender, eConsole, &dev)
	if hr := comCall(enum, 4, 0, 0, uintptr(unsafe.Pointer(&dev))); int32(hr) < 0 || dev == nil {
		return errors.New("no sound device found")
	}
	defer comRelease(dev)
	var vol unsafe.Pointer
	// IMMDevice::Activate(&IID_IAudioEndpointVolume, CLSCTX_ALL, NULL, &vol)
	if hr := comCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&vol))); int32(hr) < 0 || vol == nil {
		return errors.New("no sound device found")
	}
	defer comRelease(vol)
	return fn(vol)
}

// IAudioEndpointVolume methods (after IUnknown's three).
const (
	aevSetMasterScalar = 7
	aevGetMasterScalar = 9
	aevSetMute         = 14
	aevGetMute         = 15
)

func (windowsOps) volume() (int, bool, bool) {
	var level float32
	var muted int32
	err := withEndpointVolume(func(vol unsafe.Pointer) error {
		if hr := comCall(vol, aevGetMasterScalar, uintptr(unsafe.Pointer(&level))); int32(hr) < 0 {
			return errors.New("volume")
		}
		comCall(vol, aevGetMute, uintptr(unsafe.Pointer(&muted)))
		return nil
	})
	if err != nil {
		return 0, false, false
	}
	return clamp(int(math.Round(float64(level)*100)), 0, 100), muted != 0, true
}

func (windowsOps) setVolume(v int) error {
	return withEndpointVolume(func(vol unsafe.Pointer) error {
		// A float argument: Go's Windows call puts each argument in the
		// integer and the XMM register, so the float's bits go as is.
		if hr := comCall(vol, aevSetMasterScalar, uintptr(math.Float32bits(float32(v)/100)), 0); int32(hr) < 0 {
			return errors.New("the volume could not be set")
		}
		return nil
	})
}

func (windowsOps) setMute(on bool) error {
	return withEndpointVolume(func(vol unsafe.Pointer) error {
		b := uintptr(0)
		if on {
			b = 1
		}
		if hr := comCall(vol, aevSetMute, b, 0); int32(hr) < 0 {
			return errors.New("the sound could not be muted")
		}
		return nil
	})
}

func (windowsOps) battery() (int, bool, bool) {
	var s struct {
		ACLineStatus, BatteryFlag, BatteryLifePercent, SystemStatusFlag byte
		BatteryLifeTime, BatteryFullLifeTime                            uint32
	}
	if r, _, _ := procPowerStat.Call(uintptr(unsafe.Pointer(&s))); r == 0 {
		return 0, false, false
	}
	if s.BatteryFlag&128 != 0 || s.BatteryFlag == 255 || s.BatteryLifePercent > 100 {
		return 0, false, false // no battery, or unknown
	}
	return int(s.BatteryLifePercent), s.ACLineStatus == 1, true
}

// hidden runs a program without a console window.
func hidden(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

func powershell(script string) (string, error) {
	out, err := hidden("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	return strings.TrimSpace(string(out)), err
}

// noBacklight remembers (in the data folder) that this computer's screen
// has no WMI brightness, so status does not start PowerShell every time.
func noBacklightFile() string {
	root, err := rootDir()
	if err != nil {
		return ""
	}
	name, _ := os.Hostname()
	return filepath.Join(newLayout(root).data, "no-brightness-"+name)
}

func (windowsOps) brightness() (int, bool) {
	f := noBacklightFile()
	if f != "" {
		if _, err := os.Stat(f); err == nil {
			return 0, false
		}
	}
	out, err := powershell("(Get-CimInstance -Namespace root/WMI -ClassName WmiMonitorBrightness -ErrorAction Stop | Select-Object -First 1).CurrentBrightness")
	v, perr := strconv.Atoi(out)
	if err != nil || perr != nil {
		if f != "" {
			_ = os.WriteFile(f, nil, 0o644)
		}
		return 0, false
	}
	return clamp(v, 0, 100), true
}

func (windowsOps) setBrightness(v int) error {
	_, err := powershell("Get-CimInstance -Namespace root/WMI -ClassName WmiMonitorBrightnessMethods -ErrorAction Stop | " +
		"Invoke-CimMethod -MethodName WmiSetBrightness -Arguments @{Timeout=1; Brightness=" + strconv.Itoa(v) + "} | Out-Null")
	return err
}

func (windowsOps) power(action string) error {
	switch action {
	case "off":
		return hidden("shutdown.exe", "/s", "/t", "0").Run()
	case "reboot":
		return hidden("shutdown.exe", "/r", "/t", "0").Run()
	case "sleep":
		if r, _, err := procSuspend.Call(0, 0, 0); r == 0 {
			return err
		}
		return nil
	}
	return errors.New("unknown action")
}

func (windowsOps) downloads() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, 0); err == nil && p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

const hostKey = `Software\Chromium\NativeMessagingHosts\` + hostName

// registerHost: Chromium on Windows finds native hosts through the
// registry. The manifest lives in the data folder; on a stick the key is
// taken away again when orOS closes, so the computer keeps no trace.
func registerHost(lay layout, profile, exe string) (func(), error) {
	dir := filepath.Join(lay.data, "native-host")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return func() {}, err
	}
	file := filepath.Join(dir, hostName+".json")
	if err := os.WriteFile(file, hostManifest(exe), 0o644); err != nil {
		return func() {}, err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, hostKey, registry.SET_VALUE)
	if err != nil {
		return func() {}, err
	}
	defer k.Close()
	if err := k.SetStringValue("", file); err != nil {
		return func() {}, err
	}
	if lay.installed {
		return func() {}, nil
	}
	return func() {
		k, err := registry.OpenKey(registry.CURRENT_USER, hostKey, registry.QUERY_VALUE)
		if err != nil {
			return
		}
		v, _, _ := k.GetStringValue("")
		k.Close()
		if v == file { // another orOS took it over meanwhile: leave it
			_ = registry.DeleteKey(registry.CURRENT_USER, hostKey)
		}
	}, nil
}

// detach starts a program outside Chromium's job object, so it keeps
// running after the native host exits.
func detach(name string, args ...string) error {
	const createBreakawayFromJob, detachedProcess = 0x01000000, 0x00000008
	for _, flags := range []uint32{createBreakawayFromJob | detachedProcess, detachedProcess} {
		cmd := exec.Command(name, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := cmd.Start(); err == nil {
			return cmd.Process.Release()
		}
	}
	return errors.New("could not start " + name)
}

// killTree ends a browser the watchdog gave up on, with its children.
func killTree(p *os.Process) {
	_ = hidden("taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(p.Pid)).Run()
}
