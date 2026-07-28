//go:build !windows

package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// `make … | tail` reports the tail's status, so a build that died at rule one comes back as exit 0.
// Recorded runs are 59% pipelines, so this is where most of what magi could not determine actually
// lives. bash knows every stage; this drives a real pipeline through the tool and asserts magi says
// what happened.
func TestAFailedStageIsNamedEvenWhenThePipelineSaysZero(t *testing.T) {
	if !strings.HasSuffix(unixShell(), "bash") {
		t.Skip("this machine has no bash; PIPESTATUS is a bash feature")
	}
	env := port.ToolEnv{Workdir: t.TempDir(), ScratchTmp: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{"command": `sh -c 'echo building; exit 3' | tail -1`})

	res, err := (Bash{}).Execute(context.Background(), raw, env)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	body := resultText(t, res)
	if res.IsError {
		t.Fatalf("the pipeline's own status is 0, so the call must not read as an error: %s", body)
	}
	if !strings.Contains(body, "exit 0") {
		t.Errorf("the status the caller sees must stay the shell's own:\n%s", body)
	}
	if !strings.Contains(body, "3 → 0") {
		t.Errorf("the note must name the stage statuses left to right:\n%s", body)
	}
	if !strings.Contains(body, "FAILED") {
		t.Errorf("the note must say the head of the pipe failed:\n%s", body)
	}
	if !strings.Contains(body, "building") {
		t.Errorf("the command's output must survive intact:\n%s", body)
	}
	if strings.Contains(body, "__magi_ps") || strings.Contains(body, "pipestatus") {
		t.Errorf("magi's bookkeeping must never appear in the output:\n%s", body)
	}
}

// A pipeline whose stages all agree with its exit has nothing to add, and a note on every pipe
// would be noise that teaches the reader to skip notes.
func TestACleanPipelineGetsNoNote(t *testing.T) {
	if !strings.HasSuffix(unixShell(), "bash") {
		t.Skip("this machine has no bash")
	}
	env := port.ToolEnv{Workdir: t.TempDir(), ScratchTmp: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{"command": `echo hello | tail -1`})
	res, _ := (Bash{}).Execute(context.Background(), raw, env)
	if body := resultText(t, res); strings.Contains(body, "the LAST stage's") {
		t.Errorf("a pipeline that agrees with its exit needs no note:\n%s", body)
	}
}

// The failing pipeline's own exit still governs: a pipeline that ends non-zero reads as an error
// exactly as before, and gets no note (there is nothing hidden).
func TestPipelineExitIsPassedThroughUnchanged(t *testing.T) {
	if !strings.HasSuffix(unixShell(), "bash") {
		t.Skip("this machine has no bash")
	}
	env := port.ToolEnv{Workdir: t.TempDir(), ScratchTmp: t.TempDir()}
	raw, _ := json.Marshal(map[string]any{"command": `echo x | sh -c 'exit 4'`})
	res, _ := (Bash{}).Execute(context.Background(), raw, env)
	body := resultText(t, res)
	if !strings.Contains(body, "exit 4") {
		t.Errorf("the pipeline's own status must pass through:\n%s", body)
	}
	if strings.Contains(body, "the LAST stage's") {
		t.Errorf("nothing is hidden when the pipeline itself failed:\n%s", body)
	}
}

// pipeStageNote decides on the statuses alone, so its rule is pinned without a shell.
func TestPipeStageNoteRule(t *testing.T) {
	if n := pipeStageNote("make | tee log | head", 0, []int{2, 0, 0}); !strings.Contains(n, "2 → 0 → 0") {
		t.Errorf("a hidden failure must be named, got %q", n)
	}
	if n := pipeStageNote("ls | head", 0, []int{0, 0}); n != "" {
		t.Errorf("all-clean needs no note, got %q", n)
	}
	if n := pipeStageNote("make | head", 1, []int{2, 1}); n != "" {
		t.Errorf("a pipeline that already reports failure hides nothing, got %q", n)
	}
	if n := pipeStageNote("ls", 0, []int{0}); n != "" {
		t.Errorf("a single command is not a pipeline, got %q", n)
	}
	if n := pipeStageNote("ls | head", 0, nil); n != "" {
		t.Errorf("no capture means no claim, got %q", n)
	}
}

// A stage killed by SIGPIPE did not fail: `make … | head -50` puts exit 141 on the write side every
// time, because head exited after fifty lines — head doing exactly what it was asked to do.
// Observed live: an agent glancing at the start of a build was told "the work at the head of the
// pipe FAILED", and the build had not failed at all. A record that asserts a failure that did not
// happen is worse than one that says nothing.
func TestSigpipeIsNotAPipelineFailure(t *testing.T) {
	if got := pipeStageNote("make | head -50", 0, []int{141, 0}); got != "" {
		t.Errorf("a SIGPIPE'd stage must not be reported as a failure, got %q", got)
	}
	// A real failure upstream still says so, including when a later stage was SIGPIPE'd.
	if pipeStageNote("make | head", 0, []int{2, 0}) == "" {
		t.Error("a genuinely failing stage must still be reported")
	}
	if pipeStageNote("make | tee log | head", 0, []int{2, 141, 0}) == "" {
		t.Error("a real failure must be reported even when a later stage was SIGPIPE'd")
	}
	if got := pipeStageNote("ls | head", 0, []int{0, 0}); got != "" {
		t.Errorf("a clean pipeline has nothing to add, got %q", got)
	}
}
