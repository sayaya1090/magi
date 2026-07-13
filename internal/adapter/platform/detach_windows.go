//go:build windows

package platform

import "os/exec"

// detach is a no-op on Windows: there is no controlling-terminal concept to detach,
// and stdin is already not wired to the console for captured-output commands.
func detach(cmd *exec.Cmd) {}
