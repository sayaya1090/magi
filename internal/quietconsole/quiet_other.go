//go:build !windows

package quietconsole

import "syscall"

// Only Windows conjures a window for a console child. Elsewhere a detached parent simply has no
// controlling terminal and the child inherits that — nothing to hide.
func quiet(attr *syscall.SysProcAttr) *syscall.SysProcAttr { return attr }

// HasConsole is always true off Windows: there is no console-window concept to be without, and
// every caller that asks only wants to know whether to hide one.
var HasConsole = func() bool { return true }
