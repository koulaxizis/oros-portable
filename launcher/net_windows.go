//go:build windows

package main

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows: Wi-Fi through the Native Wi-Fi API (wlanapi.dll), no cgo and
// no netsh (whose output is in the computer's language). A network that
// Windows knows already connects with its saved profile; a new one gets a
// profile with the password (WPA2/WPA3 personal or open). Enterprise
// networks (a user name as well) stay with Windows' own settings.
// Bluetooth opens Windows' Settings page.

type windowsNet struct{}

func hostNet() netOps { return windowsNet{} }

var (
	wlanapi             = windows.NewLazySystemDLL("wlanapi.dll")
	procWlanOpen        = wlanapi.NewProc("WlanOpenHandle")
	procWlanClose       = wlanapi.NewProc("WlanCloseHandle")
	procWlanEnum        = wlanapi.NewProc("WlanEnumInterfaces")
	procWlanFree        = wlanapi.NewProc("WlanFreeMemory")
	procWlanScan        = wlanapi.NewProc("WlanScan")
	procWlanList        = wlanapi.NewProc("WlanGetAvailableNetworkList")
	procWlanConnect     = wlanapi.NewProc("WlanConnect")
	procWlanSetProfile  = wlanapi.NewProc("WlanSetProfile")
	procWlanQueryIntf   = wlanapi.NewProc("WlanQueryInterface")
	procWlanSetIntf     = wlanapi.NewProc("WlanSetInterface")
	errWlanUnavailable  = errors.New("Windows' Wi-Fi service is not running")
	errWifiNotConnected = errors.New("the network did not accept the connection; check the password")
)

// Values from wlanapi.h (layouts: wlanparse.go).
const (
	opcodeRadioState   = 4 // wlan_intf_opcode_radio_state
	radioOn            = 1 // dot11_radio_state_on
	radioOff           = 2 // dot11_radio_state_off
	modeProfile        = 0 // wlan_connection_mode_profile
	bssInfrastructure  = 1 // dot11_BSS_type_infrastructure
	errorServiceNotRun = 1062
)

type wlanConnParams struct {
	Mode    uint32
	Profile *uint16
	SSID    uintptr
	BSSIDs  uintptr
	BssType uint32
	Flags   uint32
}

type phyRadioState struct {
	PhyIndex, Software, Hardware uint32
}

// wlan is an open handle on the first Wi-Fi interface.
type wlan struct {
	h    uintptr
	guid windows.GUID
}

func openWlan() (*wlan, error) {
	var negotiated uint32
	var h uintptr
	if r, _, _ := procWlanOpen.Call(2, 0, uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&h))); r != 0 {
		if r == errorServiceNotRun {
			return nil, errWlanUnavailable
		}
		return nil, syscall.Errno(r)
	}
	var list unsafe.Pointer
	if r, _, _ := procWlanEnum.Call(h, 0, uintptr(unsafe.Pointer(&list))); r != 0 {
		procWlanClose.Call(h, 0)
		return nil, syscall.Errno(r)
	}
	defer procWlanFree.Call(uintptr(list))
	if *(*uint32)(list) == 0 {
		procWlanClose.Call(h, 0)
		return nil, errNoWifi
	}
	// The first WLAN_INTERFACE_INFO starts after the two DWORD counters.
	w := &wlan{h: h, guid: *(*windows.GUID)(unsafe.Add(list, 8))}
	return w, nil
}

func (w *wlan) close() { procWlanClose.Call(w.h, 0) }

func (w *wlan) networks() ([]availNetwork, error) {
	var list unsafe.Pointer
	if r, _, _ := procWlanList.Call(w.h, uintptr(unsafe.Pointer(&w.guid)), 0, 0, uintptr(unsafe.Pointer(&list))); r != 0 {
		return nil, syscall.Errno(r)
	}
	defer procWlanFree.Call(uintptr(list))
	n := *(*uint32)(list)
	out := make([]availNetwork, 0, n)
	for i := uint32(0); i < n; i++ {
		out = append(out, parseAvailNetwork(unsafe.Slice((*byte)(unsafe.Add(list, 8+int(i)*availNetworkSize)), availNetworkSize)))
	}
	return out, nil
}

func (w *wlan) radio() (bool, bool) {
	var size uint32
	var data unsafe.Pointer
	if r, _, _ := procWlanQueryIntf.Call(w.h, uintptr(unsafe.Pointer(&w.guid)), opcodeRadioState, 0,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&data)), 0); r != 0 {
		return false, false
	}
	defer procWlanFree.Call(uintptr(data))
	if *(*uint32)(data) == 0 {
		return false, false
	}
	st := (*phyRadioState)(unsafe.Add(data, 4))
	return st.Software == radioOn && st.Hardware == radioOn, true
}

func (windowsNet) wifi() wifiState {
	w, err := openWlan()
	if err != nil {
		return wifiState{}
	}
	defer w.close()
	s := wifiState{Present: true}
	s.On, _ = w.radio()
	if list, err := w.networks(); err == nil {
		for _, a := range list {
			if a.InUse {
				n := a.network
				s.Current = &n
			}
		}
	}
	return s
}

func (windowsNet) wifiScan() ([]network, error) {
	w, err := openWlan()
	if err != nil {
		return nil, err
	}
	defer w.close()
	if r, _, _ := procWlanScan.Call(w.h, uintptr(unsafe.Pointer(&w.guid)), 0, 0, 0); r == 0 {
		time.Sleep(4 * time.Second) // Windows asks for up to 4 s before the list is fresh
	}
	list, err := w.networks()
	if err != nil {
		return nil, err
	}
	out := make([]network, 0, len(list))
	for _, a := range list {
		out = append(out, a.network)
	}
	return out, nil
}

func (windowsNet) wifiConnect(ssid, password string) error {
	w, err := openWlan()
	if err != nil {
		return err
	}
	defer w.close()
	list, err := w.networks()
	if err != nil {
		return err
	}
	var target *availNetwork
	for i := range list {
		if list[i].SSID == ssid && (target == nil || list[i].flags&flagHasProfile != 0) {
			target = &list[i]
		}
	}
	if target == nil {
		return errors.New("the network is out of reach")
	}
	profile := target.profile
	if target.flags&flagHasProfile == 0 || password != "" {
		xml, err := wifiProfileXML(target.ssidRaw, target.auth, target.cipher, password)
		if err != nil {
			return err
		}
		x, _ := windows.UTF16PtrFromString(xml)
		var reason uint32
		if r, _, _ := procWlanSetProfile.Call(w.h, uintptr(unsafe.Pointer(&w.guid)), 0, uintptr(unsafe.Pointer(x)), 0, 1, 0,
			uintptr(unsafe.Pointer(&reason))); r != 0 {
			return fmt.Errorf("Windows refused the network settings (%d)", reason)
		}
		profile = ssid
	}
	p, _ := windows.UTF16PtrFromString(profile)
	params := wlanConnParams{Mode: modeProfile, Profile: p, BssType: bssInfrastructure}
	if r, _, _ := procWlanConnect.Call(w.h, uintptr(unsafe.Pointer(&w.guid)), uintptr(unsafe.Pointer(&params)), 0); r != 0 {
		return syscall.Errno(r)
	}
	// WlanConnect only starts it: wait until the network shows as connected.
	for end := time.Now().Add(25 * time.Second); time.Now().Before(end); time.Sleep(time.Second) {
		if list, err := w.networks(); err == nil {
			for _, a := range list {
				if a.SSID == ssid && a.InUse {
					return nil
				}
			}
		}
	}
	return errWifiNotConnected
}

func (windowsNet) wifiRadio(on bool) error {
	w, err := openWlan()
	if err != nil {
		return err
	}
	defer w.close()
	st := phyRadioState{PhyIndex: 0, Software: radioOff}
	if on {
		st.Software = radioOn
	}
	if r, _, _ := procWlanSetIntf.Call(w.h, uintptr(unsafe.Pointer(&w.guid)), opcodeRadioState,
		unsafe.Sizeof(st), uintptr(unsafe.Pointer(&st)), 0); r != 0 {
		return errors.New("Windows did not let orOS switch Wi-Fi; use the Wi-Fi button in Windows' taskbar")
	}
	return nil
}

func (windowsNet) hasBtSettings() bool { return true }

func (windowsNet) btSettings() error {
	return hidden("explorer.exe", "ms-settings:bluetooth").Start()
}
