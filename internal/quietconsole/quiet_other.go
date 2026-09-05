//go:build !windows

package quietconsole

import "syscall"

// Only Windows conjures a window for a console child. Elsewhere a detached parent simply has no
// controlling terminal and the child inherits that — nothing to hide.
func quiet(attr *syscall.SysProcAttr) *syscall.SysProcAttr { return attr }
