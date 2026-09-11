//go:build windows

package builtin

import (
	"os/exec"
	"syscall"
	"testing"
	"unsafe"
)

// openHandles is this process's open kernel-handle count, straight from the OS.
//
// The count rather than a mock: a leaked token is invisible to every other kind of check — nothing
// errors, nothing slows down, nothing is logged. The number is the only witness.
func openHandles(t *testing.T) int {
	t.Helper()
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcessHandleCount")
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		t.Fatalf("GetCurrentProcess: %v", err)
	}
	var n uint32
	if r, _, e := proc.Call(uintptr(h), uintptr(unsafe.Pointer(&n))); r == 0 {
		t.Fatalf("GetProcessHandleCount: %v", e)
	}
	return int(n)
}

// A confined launch must not cost this process a handle for ever.
//
// ⚠ **Every one of them used to.** `sandboxProcAttr` mints a restricted token per launch and
// `os/exec` does not take ownership of `SysProcAttr.Token` — CreateProcessAsUser reads it and hands
// the handle back. Nothing closed it. Measured 2026-09-11 on Windows 11: two hundred confined
// launches, handle count up by exactly two hundred, none returned. The daemon runs shell commands
// for a living and lives for days, so the count only ever went one way, and nothing anywhere failed
// while it climbed.
//
// The window itself is what is measured, not the product path: a bash call spawns a shell, and
// process handles of its own come and go around it. Held to a small allowance rather than zero for
// the same reason — the runtime may open a handle of its own at any moment — but an allowance two
// orders of magnitude under the leak it exists to catch.
func TestConfinedLaunchesDoNotLeakTokens(t *testing.T) {
	spec := confinedSpec(t)
	releaseSandbox(sandboxProcAttr(spec)) // warm every lazy DLL before counting

	before := openHandles(t)
	const runs = 200
	for i := 0; i < runs; i++ {
		attr := sandboxProcAttr(spec)
		if attr == nil || attr.Token == 0 {
			t.Fatalf("run %d: a confined spec produced no token — this guard is measuring nothing", i)
		}
		releaseSandbox(attr)
		if attr.Token != 0 {
			t.Fatalf("run %d: the token was closed but not zeroed — a second release would close "+
				"a handle Windows has since given to somebody else", i)
		}
	}
	grew := openHandles(t) - before

	if grew > 10 {
		t.Errorf("%d confined launches cost %d handles that never came back — one per launch is the "+
			"leak this guards (the daemon runs shell commands for days)", runs, grew)
	}
}

// And the whole bash path, not just the factory: a real confined command, run and finished, leaves
// nothing behind either. This is the one that bites if a spawn path forgets to release — there are
// three of them (bash, wait-for, background) and only this shape walks one end to end.
func TestAConfinedCommandGivesItsTokenBack(t *testing.T) {
	spec := confinedSpec(t)
	run := func() {
		attr := sandboxProcAttr(spec)
		defer releaseSandbox(attr)
		cmd := exec.Command("cmd", "/c", "exit", "0")
		cmd.SysProcAttr = detachTTY(attr)
		_ = cmd.Run()
	}
	run() // warm
	before := openHandles(t)
	for i := 0; i < 50; i++ {
		run()
	}
	if grew := openHandles(t) - before; grew > 10 {
		t.Errorf("50 confined commands cost %d handles — a launch that runs must give its token back", grew)
	}
}
