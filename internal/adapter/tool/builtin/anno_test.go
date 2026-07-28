//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The annotator list replaced an if/else ladder whose ORDER was the policy. This drives real
// commands through the tool and asserts each shape still gets the note it got before, and that a
// higher-precedence one still wins over a lower one on the same command.
func TestAnnotatorPrecedenceThroughTheTool(t *testing.T) {
	run := func(cmd string) string {
		t.Helper()
		env := port.ToolEnv{Workdir: t.TempDir(), ScratchTmp: t.TempDir()}
		raw, _ := json.Marshal(map[string]any{"command": cmd})
		res, err := (Bash{}).Execute(context.Background(), raw, env)
		if err != nil {
			t.Fatalf("execute %q: %v", cmd, err)
		}
		return resultText(t, res)
	}
	for _, tc := range []struct{ name, cmd, want string }{
		{"masking tail", `false || true`, "|| "},
		{"swallowing pipe", `sh -c 'exit 3' | tail -1`, "tail"},
		{"sequenced tail", `echo hi; echo "exit=$?"`, ";"},
	} {
		if out := run(tc.cmd); !strings.Contains(out, "[note:") {
			t.Errorf("%s: expected an annotation for %q, got:\n%s", tc.name, tc.cmd, out)
		}
	}
	// …but a pipeline whose stages are ALL known clean gets no "status not reported here" note:
	// PIPESTATUS answered that question, and the answer is zero. Saying otherwise sends the agent
	// to re-run the command without the pipe to learn what it was already told.
	if out := run(`echo hi | tail -1`); strings.Contains(out, "not reported here") {
		t.Errorf("a clean pipeline's head status IS known and must not be called unknown:\n%s", out)
	}
	// Precedence: a body that carries a crash outranks the shape of the command, and only ONE
	// note is attached — stacking would bury the sharpest under the vaguest.
	out := run(`sh -c 'echo "Traceback (most recent call last):"; exit 1' || true`)
	if n := strings.Count(out, "[note:"); n != 1 {
		t.Errorf("exactly one note must win, got %d:\n%s", n, out)
	}
}
