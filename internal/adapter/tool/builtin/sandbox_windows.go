//go:build windows

package builtin

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/sayaya1090/magi/internal/port"
)

// Windows has no CLI sandbox wrapper (unlike macOS sandbox-exec / Linux bwrap),
// so there is no argv to rewrite.
func sandboxArgv(spec port.SandboxSpec, command string) ([]string, bool) { return nil, false }

// detachTTY denies the foreground command a console. Windows has no controlling terminal,
// but its analog of "no tty, so an interactive prompt fails fast instead of hanging" is
// DETACHED_PROCESS: the child inherits no console, so ssh/git/sudo host-key or password reads
// — which use the console directly, not stdin — fail immediately rather than blocking on the
// console the agent process owns (a foreground `ssh host cmd` on a first-seen host otherwise
// hangs on "Are you sure you want to continue connecting?" AND freezes the shared-console TUI,
// while the timeout kills only the shell and leaves the ssh grandchild holding that console).
// stdout/stderr are redirected to a file, so the missing console costs nothing for output, and
// programs that truly need a terminal go through the explicit pty path (background+pty=true).
// A sandbox Token, when present, is preserved.
func detachTTY(attr *syscall.SysProcAttr) *syscall.SysProcAttr {
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.CreationFlags |= windows.DETACHED_PROCESS
	return attr
}

// killGroup is a no-op on Windows: there is no POSIX process-group signalling, and
// the background command's context-cancel already terminates its process. Callers
// treat a nil return as "nothing more to do".
func killGroup(pid int) error { return nil }

var procCreateRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

// DISABLE_MAX_PRIVILEGE: strip ALL privileges from the new token.
const disableMaxPrivilege = 0x1

// sandboxProcAttr returns process attributes that launch the command under a
// restricted token — Stage 1 of Windows confinement: every privilege is removed
// (no driver loading, debug, take-ownership, etc.), shrinking the blast radius of
// a prompt-injected or runaway command. It is a RESTRICTED VERSION of our own
// token, so CreateProcessAsUser accepts it without special privileges.
//
// NOTE: this does NOT yet jail writes to the workspace or block the network the
// way macOS/Linux do — that requires an AppContainer (a custom SID + workspace
// ACLs + CreateProcess with a security-capabilities attribute list, which os/exec
// can't express). That is the planned Stage 2 and needs a real Windows host to
// validate. On any error here we return nil (run unconfined) so confinement never
// breaks the bash tool; the policy-layer command scan + permission prompt remain
// the active guardrails meanwhile.
func sandboxProcAttr(spec port.SandboxSpec) *syscall.SysProcAttr {
	if !spec.Confined() {
		return nil
	}
	tok, err := restrictedSelfToken()
	if err != nil {
		return nil // fail open
	}
	return &syscall.SysProcAttr{Token: syscall.Token(tok)}
}

func restrictedSelfToken() (windows.Token, error) {
	var cur windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_QUERY, &cur); err != nil {
		return 0, err
	}
	defer cur.Close()

	var restricted windows.Token
	// CreateRestrictedToken(existing, DISABLE_MAX_PRIVILEGE, 0,nil, 0,nil, 0,nil, &new)
	r, _, e := procCreateRestrictedToken.Call(
		uintptr(cur), uintptr(disableMaxPrivilege),
		0, 0, // SidsToDisable
		0, 0, // PrivilegesToDelete
		0, 0, // SidsToRestrict
		uintptr(unsafe.Pointer(&restricted)),
	)
	if r == 0 {
		return 0, e
	}
	return restricted, nil
}

// signalGroup has no POSIX equivalent on Windows: there is no process-group
// SIGINT/SIGTERM to deliver from outside a console. Callers surface this as a
// friendly error; the hard bash_kill path (context cancel) still works.
func signalGroup(pid int, sig string) error {
	return errors.New("graceful signals are not supported on Windows — use bash_kill without signal (hard stop)")
}
