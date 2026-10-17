package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// A file that was written and then complained about is not a write that failed.
//
// A post-edit hook's output is attached to the result and marks it an error, on purpose: that is
// what makes the agent read it instead of moving on. Every screen drew its outcome from the same
// field, so the row said the write had FAILED over a file that was on disk — reported live, with
// the model treating the call as done and both windows disagreeing with the filesystem.
//
// Driven through executeTool because that is where the two facts are decided together; asserting
// on the flag alone would pass with the producer never setting it.
func TestAWriteThatWasLintedSaysTheWorkHappened(t *testing.T) {
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	// A hook that always has something to say about a write, which is the shape of a formatter or
	// a linter: it does not stop the edit, it comments on it.
	hook := HookSpec{Event: "PostToolUse", Match: "write", Command: "echo tabs, not spaces; exit 2"}
	a := closeAfter(t, New(store, nil, builtin.Default(), bus.New(), platform.New(),
		Config{Permission: "allow", Hooks: []HookSpec{hook}}))
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	args, _ := json.Marshal(map[string]string{"path": "hello.py", "content": "print(\"hello world\")\n"})
	a.executeTool(ctx, a.sessionInfo(ctx, sid), AgentSpec{Name: "coder"}, 0,
		event.Actor{Kind: event.ActorAgent, ID: "coder"},
		&session.ToolCall{CallID: "c1", Name: "write", Args: args}, newRunGuard(nil), "")

	// The file is the fact everything else has to agree with.
	if _, serr := os.Stat(filepath.Join(wd, "hello.py")); serr != nil {
		t.Fatalf("the write did not land: %v", serr)
	}

	var res *session.ToolResult
	for _, m := range mustRead(t, a, sid) {
		var d event.PartAppendedData
		if m.Type != event.TypePartAppended || json.Unmarshal(m.Data, &d) != nil {
			continue
		}
		if d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil {
			res = d.Part.ToolResult
		}
	}
	if res == nil {
		t.Fatal("the call produced no result")
	}
	if !strings.Contains(string(res.Content), "wrote") {
		t.Errorf("the result does not say the file was written: %s", res.Content)
	}
	if !res.IsError {
		t.Error("the hook's output no longer reaches the agent as something it must act on")
	}
	if !res.Advisory {
		t.Error("nothing says the work happened, so every screen will draw this as a failure")
	}
}

func TestBackgroundExitIsPersistedAfterLaunchResult(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	a.executeTool(ctx, a.sessionInfo(ctx, sid), AgentSpec{Name: "coder"}, 0, event.Actor{Kind: event.ActorAgent, ID: "coder"}, &session.ToolCall{CallID: "background-test", Name: "bash", Args: json.RawMessage(`{"command":"exit 7","background":true}`)}, newRunGuard(nil), "")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events := mustRead(t, a, sid)
		var resultSeq int64
		results := 0
		for _, e := range events {
			if e.Type == event.TypePartAppended {
				var d event.PartAppendedData
				_ = json.Unmarshal(e.Data, &d)
				if d.Part.ToolResult != nil {
					results++
					if !d.Part.ToolResult.Background {
						t.Fatal("background launch metadata missing")
					}
					resultSeq = e.Seq
				}
			}
			if e.Type == event.TypeToolBackground {
				var d struct {
					CallID string `json:"callId"`
					State  string `json:"state"`
					Exit   int    `json:"exit"`
				}
				_ = json.Unmarshal(e.Data, &d)
				if resultSeq == 0 || e.Seq <= resultSeq || d.CallID != "background-test" || d.State != "failed" || d.Exit != 7 || results != 1 {
					t.Fatalf("bad lifecycle: %+v result seq=%d count=%d", d, resultSeq, results)
				}
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("background exit never persisted")
}

func TestBackgroundReconciliationDoesNotInventCompletionOrInvalidateOwnedJobs(t *testing.T) {
	a := newTestApp(t)
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	a.appendPart(ctx, sid, event.Actor{Kind: event.ActorAgent, ID: "coder"}, "bg-message", session.RoleTool, session.Part{ID: "bg-result", Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: "orphan", Background: true, Content: json.RawMessage(`"started"`)}})
	a.backgroundOwned = map[string]bool{string(sid) + "/orphan": true}
	a.reconcileBackground(ctx, sid)
	for _, e := range mustRead(t, a, sid) {
		if e.Type == event.TypeToolBackground {
			t.Fatal("live job invalidated")
		}
	}
	delete(a.backgroundOwned, string(sid)+"/orphan")
	a.reconcileBackground(ctx, sid)
	a.reconcileBackground(ctx, sid)
	count := 0
	for _, e := range mustRead(t, a, sid) {
		if e.Type == event.TypeToolBackground {
			count++
			if !strings.Contains(string(e.Data), `"state":"unknown"`) {
				t.Fatalf("invented outcome: %s", e.Data)
			}
		}
	}
	if count != 1 {
		t.Fatalf("unknown facts=%d want 1", count)
	}
}
