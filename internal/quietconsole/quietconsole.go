// Package quietconsole keeps a console-less magi from flashing a window every time it runs a
// console program.
//
// A daemon started detached (`magi --daemon --detach`, which the PowerPoint helper does) has no
// console. On Windows a console child of such a parent gets a brand-new VISIBLE console window —
// the black box that blinks on every shell tool call, every `git status`, every `taskkill`. Measured
// on 2026-09-06: `powershell -NoProfile -Command "echo hello"` spawned by magi.exe came up with a
// top-level window handle; the same command from magi run inside a terminal inherits that terminal
// and shows nothing.
//
// The fix is narrow on purpose: when THIS process has no console window, the child is created with
// CREATE_NO_WINDOW, so it gets an invisible console of its own. Redirected stdout/stderr still flow
// through the pipes os/exec set up — this is not DETACHED_PROCESS, which gives the child no console
// at all and broke output capture (internal/adapter/tool/builtin/sandbox_windows.go records that).
// When we DO have a console (the TUI in a terminal), nothing changes: the child inherits it exactly
// as before, so interactive programs that open CONIN$ keep working.
package quietconsole

import (
	"os/exec"
	"syscall"
)

// Apply adjusts cmd so a console child does not open a visible window when this process has none.
// Safe to call on any OS; a no-op everywhere but Windows.
func Apply(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	cmd.SysProcAttr = Attr(cmd.SysProcAttr)
}

// Attr is Apply for code that builds SysProcAttr by hand (a sandbox token, a process group). It
// keeps whatever is already there and returns the same pointer when nothing needs to change.
func Attr(attr *syscall.SysProcAttr) *syscall.SysProcAttr {
	return quiet(attr)
}
