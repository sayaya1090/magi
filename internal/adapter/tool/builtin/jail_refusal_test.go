package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The jail's refusal names the two things the caller cannot see: WHERE the boundary is, and that a
// way past it exists.
//
// It used to be `outside workdir: /etc/environment` and nothing else. Observed live
// (sqlite-with-gcov, 2026-07-30): a task whose goal was to put a binary on the PATH tried
// write{/etc/profile.d/sqlite.sh}, then read{/etc/environment}, then edit{/etc/environment} —
// three calls, three identical refusals, no mention of the workdir it was measured against and no
// hint that the same run had already written that directory through bash minutes earlier.
func TestTheJailRefusalNamesTheBoundaryAndTheWayPast(t *testing.T) {
	wd := t.TempDir()
	env := port.ToolEnv{Workdir: wd}
	run := func(tool port.Tool, args map[string]any) (string, bool) {
		t.Helper()
		b, _ := json.Marshal(args)
		res, err := tool.Execute(context.Background(), b, env)
		if err != nil {
			t.Fatal(err)
		}
		var s string
		if json.Unmarshal(res.Content, &s) != nil {
			s = string(res.Content)
		}
		return s, res.IsError
	}

	for _, c := range []struct {
		name string
		tool port.Tool
		args map[string]any
	}{
		{"read", Read{}, map[string]any{"path": "/etc/environment"}},
		{"write", Write{}, map[string]any{"path": "/etc/profile.d/sqlite.sh", "content": "x\n"}},
		{"edit", Edit{}, map[string]any{"path": "/etc/environment", "old": "a", "new": "b"}},
		{"list", List{}, map[string]any{"path": "/etc"}},
		{"relative escape", Read{}, map[string]any{"path": "../../etc/passwd"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, isErr := run(c.tool, c.args)
			if !isErr {
				t.Fatalf("the jail still refuses: %s", out)
			}
			if !strings.Contains(out, "outside workdir") {
				t.Errorf("the refusal keeps its name:\n%s", out)
			}
			// WHERE the boundary is — a caller cannot judge "outside" without it.
			if !strings.Contains(out, filepath.Clean(wd)) {
				t.Errorf("the refusal must name the workdir it measured against:\n%s", out)
			}
			// THAT a way past exists. Three calls were spent discovering this by trial.
			if !strings.Contains(out, "bash") {
				t.Errorf("the refusal must name the route that can reach outside:\n%s", out)
			}
			// Honest about it: bash's own confinement is a separate axis, so this promises an
			// attempt, not a success.
			if !strings.Contains(out, "sandbox") {
				t.Errorf("the bash route is offered with its caveat:\n%s", out)
			}
		})
	}

	// A path INSIDE the workdir is untouched by any of this.
	if _, isErr := run(Write{}, map[string]any{"path": "in.txt", "content": "hi\n"}); isErr {
		t.Error("a path inside the workdir is not refused")
	}
	out, isErr := run(Read{}, map[string]any{"path": "in.txt"})
	if isErr || !strings.Contains(out, "hi") {
		t.Errorf("and reads back: %s", out)
	}
}
