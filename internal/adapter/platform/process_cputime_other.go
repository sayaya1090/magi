//go:build !linux && !windows

package platform

import (
	"syscall"
	"time"
)

// ProcessCPUTime on non-Linux/Windows hosts (darwin dev machines, the BSDs)
// reports liveness only: signal 0 probes the pid without delivering anything, and
// CPU time is left at 0 because there is no cheap pure-Go per-process CPU counter
// here. Since the lease gate extends only on a MEASURED CPU advance, the returned
// zero means the CPU-active extension is inert on these hosts — the lease judge
// decides as before. That is acceptable because the benchmark/production workers
// run on Linux and Windows, where the real CPU-delta signal is available; reading
// /proc-equivalents via cgo would break the cross-compile invariant (§9.5).
func (OS) ProcessCPUTime(pid int) (time.Duration, bool) {
	if pid <= 0 {
		return 0, false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return 0, false
	}
	return 0, true
}
