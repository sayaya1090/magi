//go:build !windows

package builtin

import (
	"syscall"

	"github.com/sayaya1090/magi/internal/port"
)

// sandboxProcAttr is a no-op off Windows; those platforms confine via an argv
// wrapper (sandbox-exec / bwrap) instead of a process token.
func sandboxProcAttr(spec port.SandboxSpec) *syscall.SysProcAttr { return nil }
