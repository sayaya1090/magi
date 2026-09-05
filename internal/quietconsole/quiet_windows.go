//go:build windows

package quietconsole

import (
	"syscall"

	"golang.org/x/sys/windows"
)

var procGetConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

// hasConsole reports whether this process owns a console window. A var so tests can pretend to be
// the detached daemon on a machine where the test runner itself sits in a terminal.
var hasConsole = func() bool {
	h, _, _ := procGetConsoleWindow.Call()
	return h != 0
}

func quiet(attr *syscall.SysProcAttr) *syscall.SysProcAttr {
	if hasConsole() {
		return attr
	}
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.HideWindow = true
	attr.CreationFlags |= windows.CREATE_NO_WINDOW
	return attr
}
