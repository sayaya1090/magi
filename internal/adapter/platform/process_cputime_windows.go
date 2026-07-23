//go:build windows

package platform

import (
	"syscall"
	"time"
	"unsafe"
)

// Pure-stdlib access to kernel32 (no golang.org/x/sys dependency, so the
// cross-compile invariant §9.5 holds). GetProcessTimes reports a process's
// cumulative kernel+user CPU time as two FILETIMEs in 100-ns units.
var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess        = kernel32.NewProc("OpenProcess")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
	procGetProcessTimes    = kernel32.NewProc("GetProcessTimes")
	procGetExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
)

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259 // STILL_ACTIVE / STATUS_PENDING
)

type filetime struct {
	low  uint32
	high uint32
}

func (f filetime) duration() time.Duration {
	ticks := int64(f.high)<<32 | int64(f.low) // 100-ns units
	return time.Duration(ticks) * 100 * time.Nanosecond
}

// ProcessCPUTime opens the process for limited query, sums kernel+user CPU time,
// and reports liveness. A pid that cannot be opened is treated as dead; an open
// handle whose exit code is no longer STILL_ACTIVE is also reported dead so the
// lease gate does not keep extending a process that has already finished.
func (OS) ProcessCPUTime(pid int) (time.Duration, bool) {
	if pid <= 0 {
		return 0, false
	}
	h, _, _ := procOpenProcess.Call(uintptr(processQueryLimitedInformation), 0, uintptr(pid))
	if h == 0 {
		return 0, false // no such process (or access denied) => treat as dead
	}
	defer procCloseHandle.Call(h)

	var creation, exit, kernel, user filetime
	r, _, _ := procGetProcessTimes.Call(
		h,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	cpu := kernel.duration() + user.duration()
	if r == 0 {
		return 0, true // handle valid but times unreadable => liveness only
	}

	alive := true
	var code uint32
	if rc, _, _ := procGetExitCodeProcess.Call(h, uintptr(unsafe.Pointer(&code))); rc != 0 {
		alive = code == stillActive
	}
	return cpu, alive
}
