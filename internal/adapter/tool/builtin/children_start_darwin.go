package builtin

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// procStart reads when the process pid started, as the kernel records it.
func procStart(pid int) (string, bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || kp == nil || int(kp.Proc.P_pid) != pid {
		return "", false
	}
	t := kp.Proc.P_starttime
	return fmt.Sprintf("%d.%06d", t.Sec, t.Usec), true
}
