package main

// Windows Hello for orOS's screen lock (lockbio.go): the system's own
// prompt (fingerprint, face or the Windows Hello PIN; Chris 2026-11-12),
// through WinRT's UserConsentVerifier with IUserConsentVerifierInterop,
// which opens it over the browser window Chromium named. Not WebAuthn,
// no passkey: Windows only says whether the person passed. D is sealed
// with DPAPI for this Windows user.

import (
	"errors"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type helloBio struct{}

func hostBio() bioOps { return helloBio{} }

var (
	modCombase              = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize        = modCombase.NewProc("RoInitialize")
	procRoUninitialize      = modCombase.NewProc("RoUninitialize")
	procRoGetActivationFact = modCombase.NewProc("RoGetActivationFactory")
	procWindowsCreateString = modCombase.NewProc("WindowsCreateString")
	procWindowsDeleteString = modCombase.NewProc("WindowsDeleteString")
	modUser32Hello          = windows.NewLazySystemDLL("user32.dll")
	procGetForegroundWindow = modUser32Hello.NewProc("GetForegroundWindow")
)

const helloClass = "Windows.Security.Credentials.UI.UserConsentVerifier"

var (
	// IUserConsentVerifierStatics, IUserConsentVerifierInterop, IAsyncInfo,
	// IAsyncOperation<UserConsentVerifierAvailability> and
	// IAsyncOperation<UserConsentVerificationResult> (the last two are the
	// WinRT parameterized IIDs, derived from their signatures).
	iidHelloStatics = windows.GUID{Data1: 0xAF4F3F91, Data2: 0x564C, Data3: 0x4DDC, Data4: [8]byte{0xB8, 0xB5, 0x97, 0x34, 0x47, 0x62, 0x7C, 0x65}}
	iidHelloInterop = windows.GUID{Data1: 0x39E050C3, Data2: 0x4E74, Data3: 0x441A, Data4: [8]byte{0x8D, 0xC0, 0xB8, 0x11, 0x04, 0xDF, 0x94, 0x9C}}
	iidAsyncInfo    = windows.GUID{Data1: 0x00000036, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidOpAvail      = windows.GUID{Data1: 0xDDD384F3, Data2: 0xD818, Data3: 0x5D83, Data4: [8]byte{0xAB, 0x4B, 0x32, 0x11, 0x9C, 0x28, 0x58, 0x7C}}
	iidOpResult     = windows.GUID{Data1: 0xFD596FFD, Data2: 0x2318, Data3: 0x558F, Data4: [8]byte{0x9D, 0xBE, 0xD2, 0x1D, 0xF4, 0x37, 0x64, 0xA5}}
)

func hstring(s string) (uintptr, error) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h uintptr
	r, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return h, nil
}

// helloFactory: the class's activation factory as interface iid.
func helloFactory(iid *windows.GUID) (unsafe.Pointer, func(), error) {
	if err := procRoInitialize.Find(); err != nil {
		return nil, nil, err
	}
	// WinRT is set up per thread: keep this goroutine on it until done.
	runtime.LockOSThread()
	// RO_INIT_MULTITHREADED; S_FALSE (already) is fine too.
	r, _, _ := procRoInitialize.Call(1)
	inited := r == 0 || r == 1
	done := func() {
		if inited {
			procRoUninitialize.Call()
		}
		runtime.UnlockOSThread()
	}
	cls, err := hstring(helloClass)
	if err != nil {
		done()
		return nil, nil, err
	}
	defer procWindowsDeleteString.Call(cls)
	var f unsafe.Pointer
	if r, _, _ := procRoGetActivationFact.Call(cls, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f))); r != 0 {
		done()
		return nil, nil, syscall.Errno(r)
	}
	return f, done, nil
}

// helloWait waits for an IAsyncOperation<enum> and returns its value.
func helloWait(op unsafe.Pointer, limit time.Duration) (int32, error) {
	var info unsafe.Pointer
	if r := comCall(op, 0, uintptr(unsafe.Pointer(&iidAsyncInfo)), uintptr(unsafe.Pointer(&info))); r != 0 {
		return 0, syscall.Errno(r)
	}
	defer comRelease(info)
	end := time.Now().Add(limit)
	for {
		var st int32
		if r := comCall(info, 7, uintptr(unsafe.Pointer(&st))); r != 0 {
			return 0, syscall.Errno(r)
		}
		switch st {
		case 1: // Completed
			var v int32
			if r := comCall(op, 8, uintptr(unsafe.Pointer(&v))); r != 0 {
				return 0, syscall.Errno(r)
			}
			return v, nil
		case 2: // Canceled
			return 0, bioError("cancel")
		case 3: // Error
			return 0, errors.New("windows hello error")
		}
		if time.Now().After(end) {
			comCall(info, 9) // Cancel
			return 0, bioError("timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func (helloBio) kind() string {
	if procRoGetActivationFact.Find() != nil {
		return ""
	}
	return "hello"
}

// helloAvail maps UserConsentVerifierAvailability.
func helloAvail(v int32) string {
	switch v {
	case 0: // Available
		return "ok"
	case 2: // NotConfiguredForUser
		return "none"
	case 4: // DeviceBusy: there, just busy now
		return "ok"
	}
	return "no-hw" // DeviceNotPresent, DisabledByPolicy
}

func (helloBio) can() string {
	c, err := helloCheck()
	if err != nil {
		return "no-hw"
	}
	return c
}

// helloCheck asks UserConsentVerifier.CheckAvailabilityAsync; an error
// means WinRT itself did not answer (CI checks that it does).
func helloCheck() (string, error) {
	f, done, err := helloFactory(&iidHelloStatics)
	if err != nil {
		return "", err
	}
	defer done()
	defer comRelease(f)
	var op unsafe.Pointer
	if r := comCall(f, 6, uintptr(unsafe.Pointer(&op))); r != 0 || op == nil {
		return "", syscall.Errno(r)
	}
	defer comRelease(op)
	v, err := helloWait(op, 15*time.Second)
	if err != nil {
		return "", err
	}
	return helloAvail(v), nil
}

// helloResult maps UserConsentVerificationResult.
func helloResult(v int32) error {
	switch v {
	case 0: // Verified
		return nil
	case 1, 3: // DeviceNotPresent, DisabledByPolicy
		return bioError("no-hw")
	case 2: // NotConfiguredForUser
		return bioError("none")
	case 4: // DeviceBusy
		return bioError("busy")
	case 5: // RetriesExhausted
		return bioError("lockout")
	case 6: // Canceled
		return bioError("cancel")
	}
	return bioError("failed")
}

func (helloBio) verify(text string) error {
	f, done, err := helloFactory(&iidHelloInterop)
	if err != nil {
		return bioError("no-hw")
	}
	defer done()
	defer comRelease(f)
	msg, err := hstring(text)
	if err != nil {
		return err
	}
	defer procWindowsDeleteString.Call(msg)
	hwnd := hostParentWindow
	if hwnd == 0 {
		hwnd, _, _ = procGetForegroundWindow.Call()
	}
	var op unsafe.Pointer
	// IUserConsentVerifierInterop::RequestVerificationForWindowAsync
	if r := comCall(f, 6, hwnd, msg, uintptr(unsafe.Pointer(&iidOpResult)), uintptr(unsafe.Pointer(&op))); r != 0 || op == nil {
		return bioError("failed")
	}
	defer comRelease(op)
	v, err := helloWait(op, 2*time.Minute)
	if err != nil {
		return err
	}
	return helloResult(v)
}

func dpapi(in []byte, protect bool) ([]byte, error) {
	if len(in) == 0 {
		return nil, errors.New("empty")
	}
	src := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	var out windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&src, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&src, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func (helloBio) seal(d []byte) ([]byte, error) { return dpapi(d, true) }

func (helloBio) unseal(b []byte) ([]byte, error) { return dpapi(b, false) }
