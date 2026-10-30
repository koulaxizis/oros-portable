package main

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetExtendedTcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

const tcpTableOwnerPidListener = 3 // TCP_TABLE_OWNER_PID_LISTENER

// findPortOwner reads the listening TCP sockets (IPv4, then IPv6) and
// returns the process that listens on port.
func findPortOwner(port int) (portOwner, bool) {
	for _, af := range []uint32{windows.AF_INET, windows.AF_INET6} {
		if pid, ok := listenerPID(af, port); ok {
			return portOwner{pid: pid, exe: processImage(pid)}, true
		}
	}
	return portOwner{}, false
}

// listenerPID walks MIB_TCPTABLE_OWNER_PID / MIB_TCP6TABLE_OWNER_PID.
// Rows are 6 DWORDs (IPv4: state, local addr, local port, remote addr,
// remote port, pid) or 56 bytes (IPv6: local addr[16], scope, local port,
// remote addr[16], scope, remote port, state, pid). Ports are in network
// byte order in the low 16 bits.
func listenerPID(af uint32, port int) (int, bool) {
	var size uint32
	_, _, _ = procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(af), tcpTableOwnerPidListener, 0)
	if size == 0 {
		return 0, false
	}
	buf := make([]byte, size+1024)
	size = uint32(len(buf))
	r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(af), tcpTableOwnerPidListener, 0)
	if r != 0 {
		return 0, false
	}
	return parseTCPTable(buf[:size], af == windows.AF_INET6, port)
}

func processImage(pid int) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}

func accessDenied(err error) bool {
	return errors.Is(err, windows.WSAEACCES)
}
