//go:build !windows

package builtin

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// ptySupported reports whether ptyStart can allocate a real pseudo-terminal on
// this platform. Unix (Linux/macOS/BSD) can; Windows cannot (see the stub).
const ptySupported = true

// ptyStart launches cmd attached to a new pseudo-terminal and returns the master
// side. creack/pty points the child's stdin/stdout/stderr at the PTY slave and makes
// that slave the child's controlling terminal (Setsid + Setctty), so a program that
// reads a password or a login prompt from /dev/tty — ssh, a serial getty — sees a
// real terminal. Writing to the returned master types into that terminal (bash_input);
// reading it yields the terminal's output (stdout and stderr merged, as a tty does).
//
// This deliberately reverses the default detachTTY path: with a tty attached, a stuck
// interactive prompt can hang until the timeout, which is exactly why a PTY is never the
// default and only a caller that intends to interact opts in via pty:true.
func ptyStart(cmd *exec.Cmd) (*os.File, error) {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	// A sane default geometry: a 0x0 terminal makes some full-screen programs misbehave.
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120})
	return ptmx, nil
}
