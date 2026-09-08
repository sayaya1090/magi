package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// An irreversible command is not judged against a task nobody could read.
//
// The gate asks one question — does the TASK require this, now, in this form — so the task is the
// only thing the answer can be measured against. It came from a store read whose error was
// discarded, and Read answers (nil, err) when the log cannot be read. Measured before the fix:
// with the log unreadable the gate asked a council to judge `rm -rf /server` against the empty
// string and, on assent, LET IT RUN.
//
// The rule was already written a few lines below, for the case where the council cannot be
// reached: running anyway would make the gate decorative. A gate whose question has no subject is
// decorative in the same way.
func TestAnIrreversibleCommandIsNotJudgedAgainstAnUnreadableTask(t *testing.T) {
	ctx := context.Background()
	// An ASSENTING council, deliberately: with a refusing one the command is stopped either way
	// and this would pass without the fix.
	fc := &fakeCouncil{advice: "Yes. That is the ordinary way to do what was asked."}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow, a.cfg.Interactive = false, false
	if err := a.appendPrompt(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "clear the build output"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	}); err != nil {
		t.Fatal(err)
	}
	s := a.sessionInfo(ctx, sid)
	s.Workdir = t.TempDir()
	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: []byte(`{"command":"rm -rf /server"}`)}
	loop := event.Actor{Kind: event.ActorSystem, ID: "loop"}

	// Healthy: the council is asked, it is told what the task is, and its assent lets the command
	// through. Without this half, "always refuse" passes the assertions below.
	if stopped := a.gateIrreversible(ctx, s, loop, tc, nil, "m1"); stopped {
		t.Fatal("an assenting council must let the command through")
	}
	if len(fc.adviseReqs) != 1 {
		t.Fatalf("want exactly one advice ask, got %d", len(fc.adviseReqs))
	}
	if got := fc.adviseReqs[0].Task; got != "clear the build output" {
		t.Fatalf("the gate judged scope against %q", got)
	}

	fc.adviseReqs = nil
	a.store = unreadableStore{Store: a.store, err: errors.New("the log is on a disk that went away")}

	if stopped := a.gateIrreversible(ctx, s, loop, tc, nil, "m2"); !stopped {
		t.Error("`rm -rf /server` ran with no task to judge it against")
	}
	if n := len(fc.adviseReqs); n != 0 {
		t.Errorf("the council was asked to judge scope against %q", fc.adviseReqs[0].Task)
	}
	// And the agent is told why, in the same shape the unreachable-council refusal uses, so it
	// can do the recoverable version instead of retrying blind.
	evs, rerr := a.store.(unreadableStore).Store.Read(ctx, sid, 0)
	if rerr != nil {
		t.Fatal(rerr)
	}
	var said string
	for _, e := range evs {
		var d event.PartAppendedData
		if e.Type != event.TypePartAppended || json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		if d.Part.ToolResult != nil && d.Part.ToolResult.CallID == "c1" && d.Part.ToolResult.IsError {
			var body string
			_ = json.Unmarshal(d.Part.ToolResult.Content, &body)
			said = body
		}
	}
	if !strings.Contains(said, "cannot be undone") || !strings.Contains(said, "could not be read") {
		t.Errorf("the refusal does not say what happened: %q", said)
	}
}
