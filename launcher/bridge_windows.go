package main

import (
	"log"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func bridgeProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// bridgeJob ends the bridge when the launcher ends, also when it is
// killed: the job closes with the launcher's last handle.
var bridgeJob windows.Handle

func bridgeAfterStart(cmd *exec.Cmd) {
	if bridgeJob == 0 {
		j, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			log.Printf("orOS Bridge: job: %v", err)
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		if _, err := windows.SetInformationJobObject(j, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			log.Printf("orOS Bridge: job: %v", err)
			windows.CloseHandle(j)
			return
		}
		bridgeJob = j
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(bridgeJob, h); err != nil {
		log.Printf("orOS Bridge: job: %v", err)
	}
}
