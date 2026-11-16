package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// cutCalls writes a turn killed while the named tools were running, each with arguments {}, and
// returns a fresh App over it after recovery has paired each with a "may have run" result.
func cutCalls(t *testing.T, names ...string) (*App, session.SessionID) {
	t.Helper()
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	wd := t.TempDir()
	old := New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := old.CreateSession(ctx, command.CreateSession{Workdir: wd})
	b, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_user", Parts: []session.Part{{Kind: session.PartText, Text: "do it"}}})
	if err := old.appendFact(ctx, sid, event.TypePromptSubmitted, crashActor, b); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		old.appendPart(ctx, sid, crashActor, "m_asst", session.RoleAssistant, session.Part{ID: "p_" + n, Kind: session.PartToolCall,
			ToolCall: &session.ToolCall{CallID: "c_cut_" + n, Name: n, Args: json.RawMessage(`{}`)}})
		sb, _ := json.Marshal(event.ToolStartedData{CallID: "c_cut_" + n, Tool: n})
		if err := old.appendFact(ctx, sid, event.TypeToolStarted, crashActor, sb); err != nil {
			t.Fatal(err)
		}
	}
	a := closeAfter(t, New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	a.RecoverInterrupted(ctx, wd)
	return a, sid
}

// Some tools change nothing outside the conversation: the council reads the record and judges,
// ask_user asks, todowrite and label rewrite the session's own plan and labels. Running one again
// does nothing twice, so a restart that cut one is not a reason to hold the next — and the council's
// arguments never change ({"complete":true,...}), so holding it held EVERY later council in that
// conversation (observed 2026-11-14..17: 35 three-minute waits in allow mode). A tool that acts on
// the world (bash here) is still held.
func TestRepeatingAToolWithNoOutsideEffectIsNotHeld(t *testing.T) {
	exempt := []string{"council", "ask_user", "todowrite", "label"}
	a, sid := cutCalls(t, append(append([]string{}, exempt...), "bash")...)
	ctx := context.Background()
	for _, n := range exempt {
		if why := a.uncertainRepeat(ctx, sid, callOf(n, `{}`)); why != "" {
			t.Errorf("%s repeats a cut call but has no outside effect; it must not be held: %q", n, why)
		}
		if a.gatePermission(ctx, sid, personActor, callOf(n, `{}`), "m_"+n) {
			t.Errorf("%s was stopped at the permission gate", n)
		}
	}
	if why := a.uncertainRepeat(ctx, sid, callOf("bash", `{}`)); why == "" {
		t.Error("a repeated bash may do its work twice; it must still be held")
	}
}
