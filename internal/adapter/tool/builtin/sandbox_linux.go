//go:build linux

package builtin

import (
	"os"
	"os/exec"

	"github.com/sayaya1090/magi/internal/port"
)

// sandboxArgv wraps a shell command in a bubblewrap (bwrap) sandbox when the
// spec requests confinement. bwrap is the Linux CLI-wrapper counterpart to
// macOS sandbox-exec: it builds a mount namespace where the whole filesystem is
// read-only except an explicit set of writable binds (the workspace + temp +
// build caches), and optionally a fresh empty network namespace.
//
// Like the macOS profile, the goal is to contain blast radius (no out-of-tree
// writes, no exfiltration) while keeping normal tooling — including the agent's
// own build/test commands — working. If bwrap is absent the command runs
// unconfined and the policy-layer scan + permission prompt remain the guardrails.
func sandboxArgv(spec port.SandboxSpec, command string) ([]string, bool) {
	if !spec.Confined() {
		return nil, false
	}
	bwrap, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, false // not installed → caller falls back to unconfined
	}

	// Whole FS visible read-only; /proc and /dev provided fresh.
	argv := []string{bwrap, "--ro-bind", "/", "/", "--proc", "/proc", "--dev", "/dev", "--die-with-parent"}

	// bindRW makes p writable inside the sandbox. bwrap's --bind requires the
	// source to exist, so optionally create it first (cache dirs may not exist
	// yet, and a read-only rootfs would then block the toolchain from making
	// them — which breaks the build).
	bindRW := func(p string, create bool) {
		if p == "" {
			return
		}
		if create {
			_ = os.MkdirAll(p, 0o755)
		}
		if _, err := os.Stat(p); err == nil {
			argv = append(argv, "--bind", p, p)
		}
	}

	// Temp is always writable (read-only and workspace-write both need it).
	bindRW(os.TempDir(), true)
	bindRW("/tmp", false)
	bindRW("/var/tmp", false)

	if spec.Mode == "workspace-write" {
		bindRW(spec.Workdir, false)
		if home, _ := os.UserHomeDir(); home != "" {
			// Build/package caches, so go/npm/cargo builds keep working. Create them
			// so the bind succeeds even on a fresh environment.
			for _, c := range []string{"/.cache", "/go", "/.cargo", "/.npm"} {
				bindRW(home+c, true)
			}
		}
	}

	if !spec.AllowNet {
		argv = append(argv, "--unshare-net")
	}

	argv = append(argv, "/bin/sh", "-c", command)
	return argv, true
}
