package main

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// parseTCPTable finds the listener on port in a Windows
// MIB_TCPTABLE_OWNER_PID (v6 false) or MIB_TCP6TABLE_OWNER_PID (v6 true):
// a DWORD count, then rows of 24 or 56 bytes (portowner_windows.go). It
// is plain byte work, so it is tested on every system.
func parseTCPTable(b []byte, v6 bool, port int) (int, bool) {
	if len(b) < 4 {
		return 0, false
	}
	n := int(binary.LittleEndian.Uint32(b))
	rowLen, portAt, pidAt := 24, 8, 20
	if v6 {
		rowLen, portAt, pidAt = 56, 20, 52
	}
	for i := 0; i < n; i++ {
		off := 4 + i*rowLen
		if off+rowLen > len(b) {
			break
		}
		row := b[off : off+rowLen]
		raw := binary.LittleEndian.Uint32(row[portAt:])
		p := int(uint16(raw)>>8 | uint16(raw)<<8) // network byte order
		if p == port {
			return int(binary.LittleEndian.Uint32(row[pidAt:])), true
		}
	}
	return 0, false
}

// listenInodes returns the inodes of the sockets in a /proc/net/tcp(6)
// table that listen (state 0A) on port.
func listenInodes(table string, port int) []string {
	var out []string
	suffix := fmt.Sprintf(":%04X", port)
	for _, line := range strings.Split(table, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[3] != "0A" || !strings.HasSuffix(f[1], suffix) {
			continue
		}
		out = append(out, f[9])
	}
	return out
}
