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
	if strings.Contains(body, "FAILED") {
		t.Errorf("the note states the statuses; reading them is the model's job:\n%s", body)
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

// The failing pipeline's own exit still governs: a pipeline whose LAST stage is the one that failed
// reads as an error exactly as before, and gets no note — its status says everything the stages do.
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
		t.Errorf("nothing is hidden when the last stage is the one that failed:\n%s", body)
	}
}

// …but a failure in an EARLIER stage has no status of its own to appear in, whether the last stage
// exited 0 or not. Observed live (kv-store-grpc, 2026-07-30): `ps aux | grep server.py | grep -v
// grep` came back `exit 1` with no note, byte-identical in status to a grep that simply matched
// nothing — while magi held 127 → 1 → 1 and dropped it.
func TestAnEarlierStageFailureIsNamedEvenWhenThePipelineFails(t *testing.T) {
	if !strings.HasSuffix(unixShell(), "bash") {
		t.Skip("this machine has no bash")
	}
	env := port.ToolEnv{Workdir: t.TempDir(), ScratchTmp: t.TempDir()}
	// stderr is discarded exactly as the live command's `2>/dev/null` sibling did, so the note is
	// the ONLY place the 127 can still be found.
	raw, _ := json.Marshal(map[string]any{"command": `nosuchprog aux 2>/dev/null | grep server | grep -v grep`})
	res, _ := (Bash{}).Execute(context.Background(), raw, env)
	body := resultText(t, res)
	if !strings.Contains(body, "exit 1") {
		t.Fatalf("the pipeline's own status still passes through:\n%s", body)
	}
	if !strings.Contains(body, "127 → 1 → 1") {
		t.Errorf("the stage that failed must be named:\n%s", body)
	}

	// The benign twin — the same exit 1, and nothing hidden behind it — stays silent, or the note
	// would fire on every `cmd | grep` that found nothing.
	raw, _ = json.Marshal(map[string]any{"command": `echo hi | grep nomatch`})
	res, _ = (Bash{}).Execute(context.Background(), raw, env)
	if body := resultText(t, res); strings.Contains(body, "the LAST stage's") {
		t.Errorf("a grep that matched nothing hides nothing:\n%s", body)
	}
}

// pipeStageNote decides on the statuses alone, so its rule is pinned without a shell.
func TestPipeStageNoteRule(t *testing.T) {
	if n := pipeStageNote(0, []int{2, 0, 0}); !strings.Contains(n, "2 → 0 → 0") {
		t.Errorf("a hidden failure must be named, got %q", n)
	}
	if n := pipeStageNote(0, []int{0, 0}); n != "" {
		t.Errorf("all-clean needs no note, got %q", n)
	}
	// The gate is the fact, not the exit: an earlier stage's failure is hidden behind ANY reported
	// status, because the one reported belongs to the last stage.
	if n := pipeStageNote(1, []int{2, 1}); !strings.Contains(n, "2 → 1") {
		t.Errorf("an earlier stage's failure is hidden behind a nonzero exit too, got %q", n)
	}
	if n := pipeStageNote(1, []int{0, 1}); n != "" {
		t.Errorf("the last stage failing on its own is the whole story, got %q", n)
	}
	if n := pipeStageNote(0, []int{0}); n != "" {
		t.Errorf("a single command is not a pipeline, got %q", n)
	}
	if n := pipeStageNote(0, nil); n != "" {
		t.Errorf("no capture means no claim, got %q", n)
	}
}
