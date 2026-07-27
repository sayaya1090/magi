package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

var bgIDPat = regexp.MustCompile(`bg_\d+`)

// TestBackgroundBuildIsJudgedByItsExitNotItsStart drives REAL background commands through
// executeTool, because the defect was in the seam and not in the guard: a background job's start
// and its result arrive as two different tool calls, and only the start was being read.
//
// The start's success says one thing — a process now exists. It was being recorded as the command
// CONVERGING, so a build was booked as passing before it had compiled anything, while the real
// exit came back later through bash_output and was recorded nowhere at all. The counter that
// exists to notice "the same build, N edits later, still failing" was therefore blind to every
// background build in both directions.
func TestBackgroundBuildIsJudgedByItsExitNotItsStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell semantics")
	}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, nil, builtin.Default(), bus.New(), platform.New(), Config{Permission: "allow"})
	wd := t.TempDir()
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	s := a.sessionInfo(ctx, sid)
	actor := event.Actor{Kind: event.ActorAgent, ID: "coder"}
	guard := newRunGuard()

	call := func(name string, args map[string]any) string {
		t.Helper()
		raw, _ := json.Marshal(args)
		a.executeTool(ctx, s, AgentSpec{Name: "coder"}, 0, actor, &session.ToolCall{
			CallID: "c_" + newID(), Name: name, Args: raw,
		}, guard)
		return lastToolResult(t, a, sid)
	}
	// Poll until the job has finished and its outcome has been claimed — a poll that lands while
	// the job is still running claims nothing, exactly as in a real run.
	pollUntilExited := func(id string) {
		t.Helper()
		for i := 0; i < 200; i++ {
			if out := call("bash_output", map[string]any{"id": id}); regexp.MustCompile(`exited \d+`).MatchString(out) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("background job %s never reported an exit", id)
	}

	// A deliverable must exist before a failure is deliverable churn (epoch 0 is pre-deliverable).
	call("bash", map[string]any{"command": "echo hi > f.c"})
	if guard.mutationEpoch() == 0 {
		t.Fatal("precondition: a bash write must bump the mutation epoch")
	}

	// The "build": one command text whose outcome the test controls through a state file, so the
	// SAME churn key can be made to fail and then genuinely pass.
	if err := os.WriteFile(filepath.Join(wd, "b.sh"), []byte("exit $(cat state)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "state"), []byte("3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	const build = "sh b.sh" // a program run, not a shell inspection
	out := call("bash", map[string]any{"command": build, "background": true})
	id := bgIDPat.FindString(out)
	if id == "" {
		t.Fatalf("no background id in start result: %q", out)
	}
	if n := guard.exerciseChurnMax(); n != 0 {
		t.Fatalf("starting a command recorded an outcome it cannot have yet: churn = %d", n)
	}
	pollUntilExited(id)
	if n := guard.exerciseChurnMax(); n != 1 {
		t.Fatalf("a background build that exited non-zero must be recorded as failing, churn = %d", n)
	}

	// Polling the same finished job again must not count its one failure a second time.
	call("bash_output", map[string]any{"id": id})
	call("bash_output", map[string]any{"id": id})
	if n := guard.exerciseChurnMax(); n != 1 {
		t.Fatalf("re-polling a finished job inflated its count to %d", n)
	}

	// The same build, re-run in the background under a masking tail: the job's own exit is now the
	// trailing echo's 0, which says nothing about the build — so it must not clear what climbed.
	call("bash", map[string]any{"command": "printf x >> f.c"}) // a new edit, so a fail could climb
	masked := build + `; echo "exit=$?"`
	out = call("bash", map[string]any{"command": masked, "background": true, "verify": false})
	mid := bgIDPat.FindString(out)
	if mid == "" || mid == id {
		t.Fatalf("expected a second background job, got %q", out)
	}
	pollUntilExited(mid)
	if n := guard.exerciseChurnMax(); n != 1 {
		t.Fatalf("a masked exit 0 changed the churn count to %d, want the climbed 1 kept", n)
	}

	// The same build failing again after another edit climbs — that is the churn this exists for.
	call("bash", map[string]any{"command": "printf y >> f.c"})
	out = call("bash", map[string]any{"command": build, "background": true}) // same text = same churn key
	pollUntilExited(bgIDPat.FindString(out))
	if n := guard.exerciseChurnMax(); n != 2 {
		t.Fatalf("a second failure across a new edit must climb, churn = %d", n)
	}

	// The control: the same background command, now really succeeding, clears its own count. The
	// fix must not read as "background commands stopped counting".
	if err := os.WriteFile(filepath.Join(wd, "state"), []byte("0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	call("bash", map[string]any{"command": "printf z >> f.c"})
	out = call("bash", map[string]any{"command": build, "background": true})
	pollUntilExited(bgIDPat.FindString(out))
	if n := guard.exerciseChurnMax(); n != 0 {
		t.Fatalf("a background command that really exited 0 must clear its count, churn = %d", n)
	}
}

// lastToolResult returns the text of the most recent tool result appended to the session.
func lastToolResult(t *testing.T, a *App, sid session.SessionID) string {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil || d.Part.Kind != session.PartToolResult || d.Part.ToolResult == nil {
			continue
		}
		var s string
		_ = json.Unmarshal(d.Part.ToolResult.Content, &s)
		out = s
	}
	return out
}
