//go:build windows

package builtin

import (
	"errors"
	"os"
	"os/exec"
)

// ptySupported is false on Windows: no pseudo-terminal is wired here. A ConPTY-based
// implementation could fill this in later; until then pty:true is rejected up front so
// the caller learns the limitation instead of silently getting a non-interactive pipe.
const ptySupported = false

// ptyStart is unsupported on Windows.
func ptyStart(cmd *exec.Cmd) (*os.File, error) {
	return nil, errors.New("pty background processes are not supported on Windows")
}
