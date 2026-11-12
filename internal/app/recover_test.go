package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

var crashActor = event.Actor{Kind: event.ActorUser, ID: "test"}

// crashedLog writes what a process killed mid-turn leaves on disk: a prompt, the model's tool calls,
// and — for the calls named in started — the durable mark that the tool began. No results, no
// turn.finished. Then it returns a FRESH App over the same store, the way the next start sees it.
func crashedLog(t *testing.T, started ...string) (*App, *jsonl.Store, session.SessionID, string) {
	t.Helper()
	return crashedLogAs(t, false, started...)
}

// crashedLogAs is crashedLog with the writer's version chosen: a legacy log comes from a magi that
// never wrote tool.started, so the absence of one says nothing about whether a call ran.
func crashedLogAs(t *testing.T, legacy bool, started ...string) (*App, *jsonl.Store, session.SessionID, string) {
	t.Helper()
	ctx := context.Background()
	dir, wd := t.TempDir(), t.TempDir()
	store, _ := jsonl.New(dir)
	old := New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := old.CreateSession(ctx, command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	if !legacy { // an earlier turn of a start-recording magi
		sb, _ := json.Marshal(event.ToolStartedData{CallID: "c_before", Tool: "glob"})
		if err := old.appendFact(ctx, sid, event.TypeToolStarted, crashActor, sb); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_user", Parts: []session.Part{{Kind: session.PartText, Text: "do it"}}})
	if err := old.appendFact(ctx, sid, event.TypePromptSubmitted, crashActor, b); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"c_write", "c_read"} {
		old.appendPart(ctx, sid, crashActor, "m_asst", session.RoleAssistant, session.Part{ID: "p_" + c, Kind: session.PartToolCall,
			ToolCall: &session.ToolCall{CallID: c, Name: strings.TrimPrefix(c, "c_"), Args: json.RawMessage(`{}`)}})
	}
	for _, c := range started {
		sb, _ := json.Marshal(event.ToolStartedData{CallID: c, Tool: strings.TrimPrefix(c, "c_")})
		_ = old.appendFact(ctx, sid, event.TypeToolStarted, crashActor, sb)
	}
	fresh := closeAfter(t, New(store, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	return fresh, store, sid, wd
}

func resultsByCall(t *testing.T, evs []event.Event) map[string]session.ToolResult {
	out := map[string]session.ToolResult{}
	for _, e := range evs {
		var d event.PartAppendedData
		if e.Type == event.TypePartAppended && json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartToolResult && d.Part.ToolResult != nil {
			out[d.Part.ToolResult.CallID] = *d.Part.ToolResult
		}
	}
	return out
}

// A call that started and left no result may have changed the world: recovery pairs it with a
// result that SAYS so, never runs it again, and closes the turn as needing the person — not as
// something a "continue" may silently pick up. Run twice, it writes nothing the second time.
// SPEC F-LOOP-INTERRUPT loop-int-3.
func TestRecoveryClosesACrashedTurnAndHandsUncertainCallsToThePerson(t *testing.T) {
	ctx := context.Background()
	a, store, sid, wd := crashedLog(t, "c_write")

	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].SessionID != sid || got[0].Resumable {
		t.Fatalf("want one non-resumable recovery for %s, got %+v", sid, got)
	}
	if strings.Join(got[0].Uncertain, ",") != "c_write" || strings.Join(got[0].NotRun, ",") != "c_read" {
		t.Fatalf("uncertain=%v notRun=%v, want [c_write] and [c_read]", got[0].Uncertain, got[0].NotRun)
	}
	evs, _ := store.Read(ctx, sid, 0)
	res := resultsByCall(t, evs)
	if w, ok := res["c_write"]; !ok || !w.IsError || !strings.Contains(string(w.Content), "may have run") {
		t.Fatalf("the started call must be paired with an uncertain result: %+v ok=%v", w, ok)
	}
	if r, ok := res["c_read"]; !ok || !r.IsError || !strings.Contains(string(r.Content), "never started") {
		t.Fatalf("the unstarted call must be paired with a not-run result: %+v ok=%v", r, ok)
	}
	var ed event.ErrorData
	if !lastData(evs, event.TypeError, &ed) || ed.Code != "interrupted" || ed.Resumable {
		t.Fatalf("the turn should close as interrupted and NOT resumable: %+v", ed)
	}
	if _, open := a.UnfinishedTurnOf(ctx, sid); open {
		t.Fatal("after recovery the turn must no longer read as still running")
	}
	if again := a.RecoverInterrupted(ctx, wd); len(again) != 0 {
		t.Fatalf("recovery must be idempotent; second pass recovered %+v", again)
	}
	if evs2, _ := store.Read(ctx, sid, 0); len(evs2) != len(evs) {
		t.Fatalf("second recovery wrote %d more events", len(evs2)-len(evs))
	}
}

// With nothing started, nothing can have happened: the turn closes as resumable, and a follow-up
// ("continue") picks it up from the last settled point.
func TestRecoveryOfATurnWithNoStartedToolIsResumable(t *testing.T) {
	ctx := context.Background()
	a, _, sid, wd := crashedLog(t)
	got := a.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || !got[0].Resumable || len(got[0].Uncertain) != 0 {
		t.Fatalf("want one resumable recovery, got %+v", got)
	}
	if _, ok := a.ResumableTurnOf(ctx, sid); !ok {
		t.Fatal("a recovered turn with nothing uncertain should read as resumable")
	}
}

// A turn the person cancelled stays cancelled: recovery does not touch it.
func TestRecoveryLeavesACancelledTurnAlone(t *testing.T) {
	ctx := context.Background()
	a, store, sid, wd := crashedLog(t)
	b, _ := json.Marshal(event.PromptAbandonedData{MsgID: "m_user"})
	if err := a.appendFact(ctx, sid, event.TypePromptAbandoned, crashActor, b); err != nil {
		t.Fatal(err)
	}
	before, _ := store.Read(ctx, sid, 0)
	if got := a.RecoverInterrupted(ctx, wd); len(got) != 0 {
		t.Fatalf("a cancelled turn must not be recovered: %+v", got)
	}
	if after, _ := store.Read(ctx, sid, 0); len(after) != len(before) {
		t.Fatalf("recovery wrote to a cancelled turn: %d -> %d", len(before), len(after))
	}
}
