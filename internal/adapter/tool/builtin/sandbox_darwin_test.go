//go:build darwin

package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// runSandboxed runs a shell command under the workspace-write sandbox argv and
// returns combined output + exit code.
func runSandboxed(t *testing.T, workdir, command string) (string, int) {
	t.Helper()
	argv, wrapped := sandboxArgv(port.SandboxSpec{Mode: "workspace-write", Workdir: workdir}, command)
	if !wrapped {
		t.Skip("sandbox-exec not available")
	}
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

func TestSandboxConfinesWrites(t *testing.T) {
	work := t.TempDir()
	// "Outside" must be a path the sandbox does not allow — not under the
	// workspace, temp, or cache dirs. A file at the home-dir root qualifies.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	outside := filepath.Join(home, ".magi_sbx_outside_test")
	_ = os.Remove(outside)
	t.Cleanup(func() { _ = os.Remove(outside) })

	// Write inside the workspace succeeds.
	if out, code := runSandboxed(t, work, "echo ok > "+filepath.Join(work, "in.txt")); code != 0 {
		t.Errorf("write inside workdir should succeed, got code=%d out=%q", code, out)
	}
	if _, err := os.Stat(filepath.Join(work, "in.txt")); err != nil {
		t.Errorf("expected file written inside workdir: %v", err)
	}

	// Write outside the workspace is denied by the sandbox.
	if _, code := runSandboxed(t, work, "echo nope > "+outside); code == 0 {
		t.Errorf("write outside workdir should be denied")
	}
	if _, err := os.Stat(outside); err == nil {
		t.Errorf("file outside workdir must NOT exist (sandbox failed to confine)")
	}
}

func TestSandboxReadsStillAllowed(t *testing.T) {
	work := t.TempDir()
	// Reading outside the workspace is still permitted (read-only world access).
	if out, code := runSandboxed(t, work, "head -1 /etc/hosts"); code != 0 {
		t.Errorf("reads outside workdir should be allowed, got code=%d out=%q", code, out)
	}
}

func TestSandboxFullModeUnconfined(t *testing.T) {
	if _, wrapped := sandboxArgv(port.SandboxSpec{Mode: "full"}, "echo hi"); wrapped {
		t.Error("full mode must not wrap the command")
	}
	if _, wrapped := sandboxArgv(port.SandboxSpec{}, "echo hi"); wrapped {
		t.Error("empty mode must not wrap the command")
	}
}

// An isolated child's spec names its own TempDir, and the shared temp trees drop off the allow
// list. Observed live (the escape-attempt wave run): a child's `echo … > /tmp/…` sailed through
// the seatbelt on the strength of the general temp allowance — /tmp is world-shared, and the
// per-user temp tree holds every sibling's clone.
func TestSandboxNamedTempDirNarrowsTheTempAllowance(t *testing.T) {
	work, ownTmp, sibling := t.TempDir(), t.TempDir(), t.TempDir()
	run := func(command string) int {
		t.Helper()
		argv, wrapped := sandboxArgv(port.SandboxSpec{Mode: "workspace-write", Workdir: work, TempDir: ownTmp}, command)
		if !wrapped {
			t.Skip("sandbox-exec not available")
		}
		out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		if err != nil {
			t.Fatalf("%s: %v (%s)", command, err, out)
		}
		return 0
	}
	// Its own workdir and its own temp: allowed.
	if code := run("echo ok > " + work + "/in.txt"); code != 0 {
		t.Errorf("workdir write must be allowed, got %d", code)
	}
	if code := run("echo ok > " + ownTmp + "/scratch.txt"); code != 0 {
		t.Errorf("own-temp write must be allowed, got %d", code)
	}
	// The world-shared tree and a sibling's per-user temp dir: denied.
	if code := run("echo escaped > /tmp/magi-sbx-escape-test.txt"); code == 0 {
		os.Remove("/tmp/magi-sbx-escape-test.txt")
		t.Errorf("/tmp write must be denied when the spec names its own TempDir")
	}
	if code := run("echo escaped > " + sibling + "/two.txt"); code == 0 {
		t.Errorf("a sibling temp dir must be denied when the spec names its own TempDir")
	}
	if _, err := os.Stat(sibling + "/two.txt"); err == nil {
		t.Errorf("the sibling escape file exists — the sandbox did not confine")
	}
}
