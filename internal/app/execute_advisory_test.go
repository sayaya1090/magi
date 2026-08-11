package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
		&session.ToolCall{CallID: "c1", Name: "write", Args: args}, newRunGuard(), "")

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
