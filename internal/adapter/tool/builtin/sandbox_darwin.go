//go:build darwin

package builtin

import (
	"os"
	"os/exec"
	"strings"

	"github.com/sayaya1090/magi/internal/port"
)

// sandboxArgv wraps a shell command in macOS seatbelt (sandbox-exec) confinement
// when the spec requests it. It returns the argv to run and true, or (nil,false)
// to run unconfined (full mode, missing sandbox-exec).
//
// The profile starts from "allow default" and then revokes only what matters —
// writes outside the workspace and (optionally) the network. A strict
// "deny default" profile would also have to re-grant process exec, sysctl,
// mach lookups and the build toolchain's many cache paths, which is brittle and
// routinely breaks the agent's own build/test commands. Containing the blast
// radius (no out-of-tree writes, no exfiltration) is the goal here.
func sandboxArgv(spec port.SandboxSpec, command string) ([]string, bool) {
	if !spec.Confined() {
		return nil, false
	}
	exe, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return nil, false // not available → caller falls back to unconfined
	}
	profile := seatbeltProfile(spec)
	return []string{exe, "-p", profile, "/bin/sh", "-c", command}, true
}

func seatbeltProfile(spec port.SandboxSpec) string {
	home, _ := os.UserHomeDir()
	tmp := os.TempDir()

	var b strings.Builder
	b.WriteString("(version 1)\n(allow default)\n")

	// Network: off unless explicitly allowed.
	if !spec.AllowNet {
		b.WriteString("(deny network*)\n")
	}

	// Writes: deny everything, then re-allow the workspace (workspace-write only)
	// plus temp and build-cache locations so normal tooling keeps working.
	b.WriteString("(deny file-write*)\n(allow file-write*\n")
	// The specific per-user temp dir (and its /private-resolved twin) — not the
	// whole /var/folders tree, which would expose every process's temp.
	writePaths := []string{tmp, privateTwin(tmp), "/tmp", "/private/tmp"}
	if spec.Mode == "workspace-write" && spec.Workdir != "" {
		writePaths = append([]string{spec.Workdir, privateTwin(spec.Workdir)}, writePaths...)
	}
	if home != "" {
		writePaths = append(writePaths,
			home+"/Library/Caches",
			home+"/go",
			home+"/.cache",
			home+"/.npm",
			home+"/.cargo",
		)
	}
	for _, p := range writePaths {
		b.WriteString("  (subpath " + quote(p) + ")\n")
	}
	// Character devices the shell and tools expect to write.
	for _, lit := range []string{"/dev/null", "/dev/stdout", "/dev/stderr", "/dev/dtracehelper"} {
		b.WriteString("  (literal " + quote(lit) + ")\n")
	}
	b.WriteString("  (regex #\"^/dev/tty\")\n  (regex #\"^/dev/fd/\"))\n")

	return b.String()
}

// privateTwin returns the /private-prefixed form of a /var or /tmp path, since
// macOS resolves those symlinks and seatbelt matches the resolved path.
func privateTwin(p string) string {
	if strings.HasPrefix(p, "/var/") || strings.HasPrefix(p, "/tmp/") || p == "/tmp" || strings.HasPrefix(p, "/etc/") {
		return "/private" + p
	}
	return p
}

// quote renders a string as a seatbelt (Scheme) string literal.
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
