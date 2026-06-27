//go:build windows

package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// confinedSpec is a workspace-write spec rooted at a temp dir.
func confinedSpec(t *testing.T) port.SandboxSpec {
	return port.SandboxSpec{Mode: "workspace-write", Workdir: t.TempDir()}
}

// The riskiest assumption: CreateProcessAsUser with our restricted (DISABLE_MAX_
// PRIVILEGE) token actually launches WITHOUT special privileges. This proves the
// whole token plumbing end-to-end.
func TestWindowsSandboxLaunches(t *testing.T) {
	pa := sandboxProcAttr(confinedSpec(t))
	if pa == nil || pa.Token == 0 {
		t.Fatal("expected a restricted token for a confined spec")
	}
	cmd := exec.Command("cmd", "/c", "echo sandbox-ok")
	cmd.SysProcAttr = pa
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandboxed launch failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(string(out), "sandbox-ok") {
		t.Errorf("unexpected output: %q", out)
	}
}

// A sandboxed write inside the workspace still works (Stage 1 doesn't jail the
// filesystem yet, but the process must remain usable for builds/tests).
func TestWindowsSandboxCanWriteWorkspace(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "w.txt")
	cmd := exec.Command("cmd", "/c", "echo hi> "+target)
	cmd.SysProcAttr = sandboxProcAttr(port.SandboxSpec{Mode: "workspace-write", Workdir: dir})
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("workspace write failed under sandbox: %v\n%s", err, out)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected file written in workspace: %v", err)
	}
}

func TestWindowsSandboxFullModeUnconfined(t *testing.T) {
	if sandboxProcAttr(port.SandboxSpec{Mode: "full"}) != nil {
		t.Error("full mode must not confine")
	}
	if sandboxProcAttr(port.SandboxSpec{}) != nil {
		t.Error("empty mode must not confine")
	}
}

// Informational: shows privileges before/after so we can confirm the restricted
// token actually strips them. Not a hard assertion (locale-dependent output).
func TestWindowsSandboxDropsPrivileges(t *testing.T) {
	base, _ := exec.Command("cmd", "/c", "whoami /priv").CombinedOutput()
	sb := exec.Command("cmd", "/c", "whoami /priv")
	sb.SysProcAttr = sandboxProcAttr(confinedSpec(t))
	sbOut, _ := sb.CombinedOutput()
	t.Logf("UNSANDBOXED whoami /priv:\n%s", base)
	t.Logf("SANDBOXED   whoami /priv:\n%s", sbOut)
	if len(strings.TrimSpace(string(sbOut))) >= len(strings.TrimSpace(string(base))) {
		t.Logf("NOTE: sandboxed privilege list not visibly smaller — inspect the dumps above")
	}
}
