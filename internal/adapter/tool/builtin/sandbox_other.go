//go:build !darwin && !linux && !windows

package builtin

import "github.com/sayaya1090/magi/internal/port"

// sandboxArgv has no OS-level confinement on this platform. Commands run
// unconfined; the policy layer's command scan + permission prompt remain the
// active guardrails.
func sandboxArgv(spec port.SandboxSpec, command string) ([]string, bool) {
	return nil, false
}
