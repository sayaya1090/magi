package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// selfKillReason blocks exactly the kill-by-match forms that would hit the agent's
// own process — pkill -f vs the command line (the live exit-137 case: "release"
// appeared in the task prompt), pkill/killall vs the process name, and the
// Windows/PowerShell name killers — and passes everything that cannot hit us.
func TestSelfKillReason(t *testing.T) {
	cmdline := `magi -p fix the release build so it runs without crash`
	name := "magi-arm64"
	for _, tc := range []struct {
		testName string
		cmd      string
		block    bool
	}{
		// The observed live self-kill: prompt contains "release".
		{"pkill -f prompt word", `pkill -9 -f "release" 2>/dev/null; cat > /tmp/fix.cpp`, true},
		{"pkill -f combined flags", `pkill -9f release`, true},
		{"pkill -f unrelated", `pkill -f qemu-system-inner`, false},
		// Name matchers.
		{"pkill own name", `pkill magi-arm64`, true},
		{"pkill name regex", `pkill 'magi.*'`, true},
		{"killall own name", `killall magi-arm64`, true},
		{"killall unrelated", `killall release`, false},
		// pkill without -f matches NAMES, so a prompt word passes.
		{"pkill prompt word no -f", `pkill release`, false},
		// Windows / PowerShell forms.
		{"taskkill own image", `taskkill /F /IM magi-arm64.exe`, true},
		{"taskkill wildcard", `taskkill /IM magi*`, true},
		{"taskkill unrelated", `taskkill /F /IM release.exe`, false},
		{"stop-process own name", `Stop-Process -Name magi-arm64 -Force`, true},
		{"stop-process list", `Stop-Process -Name "node,magi-arm64"`, true},
		{"stop-process unrelated", `Stop-Process -ProcessName release`, false},
		// Precise targeting is never blocked.
		{"kill by pid", `kill -9 12345`, false},
		{"pgrep then kill", `kill $(pgrep -x release)`, false},
	} {
		t.Run(tc.testName, func(t *testing.T) {
			got := selfKillReason(tc.cmd, cmdline, name) != ""
			if got != tc.block {
				t.Errorf("selfKillReason(%q) blocked=%v, want %v", tc.cmd, got, tc.block)
			}
		})
	}
}

// The guard is wired into Execute: a pkill -f whose pattern matches the REAL test
// process command line is refused as an error result, and the off flag lets it
// through to the shell.
func TestBashExecuteBlocksSelfKill(t *testing.T) {
	cmdline, _ := selfIdentity()
	// Pick a fragment of our own real argv that pkill -f would match.
	frag := "builtin.test"
	if !strings.Contains(cmdline, frag) {
		t.Skipf("test binary cmdline %q lacks expected fragment", cmdline)
	}
	env := port.ToolEnv{Workdir: t.TempDir()}
	raw := json.RawMessage(`{"command":"pkill -f builtin.test"}`)

	r, _ := Bash{}.Execute(context.Background(), raw, env)
	if !r.IsError || !strings.Contains(resultText(t, r), "OWN process") {
		t.Fatalf("self-matching pkill -f must be blocked, got %s", resultText(t, r))
	}

	// The off flag is checked at the gate only — actually EXECUTING a
	// self-matching pkill here would kill the test process.
	t.Setenv("MAGI_SELFKILL_GUARD", "off")
	if selfKillGuardEnabled() {
		t.Fatal("off flag must disable the guard")
	}
	t.Setenv("MAGI_SELFKILL_GUARD", "")
	if !selfKillGuardEnabled() {
		t.Fatal("default must be ON")
	}
}
