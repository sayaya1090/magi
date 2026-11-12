package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A shutdown that cuts a call and then cannot save its "may have run" answer must not close the
// turn. It used to: the answer was written best-effort, the failure dropped, and the interruption
// and turn.finished followed — so the next start saw a finished turn, skipped it, and the call was
// left without any result for good. Left open, the next start pairs it as a call that may have run.
func TestAShutdownThatCannotSaveTheCutCallsAnswerLeavesTheTurnForRecovery(t *testing.T) {
	ctx := context.Background()
	store, _ := jsonl.New(t.TempDir())
	wd := t.TempDir()
	rs := &refusingStore{Store: store, refuse: func(e event.Event) bool {
		var d event.PartAppendedData
		return e.Type == event.TypePartAppended && json.Unmarshal(e.Data, &d) == nil &&
			d.Part.ToolResult != nil && d.Part.ToolResult.CallID == "c_bash"
	}}
	llm := &scriptLLM{steps: [][]port.ProviderEvent{asideCall("bash", `{"command":"echo ran >> marker; sleep 30"}`)}}
	a := New(rs, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "unregistered"}})
	a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(wd, "marker")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the tool never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if left := a.Shutdown(5 * time.Second); len(left) != 0 {
		t.Fatalf("left %v", left)
	}
	evs, _ := store.Read(ctx, sid, 0)
	if n := countType(evs, event.TypeTurnFinished); n != 0 {
		t.Fatalf("the turn was closed over a call whose answer was not saved: types=%v", typesOf(evs))
	}

	rs.allow()
	fresh := closeAfter(t, New(rs, &stuckLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"}))
	got := fresh.RecoverInterrupted(ctx, wd)
	if len(got) != 1 || got[0].Err != nil || got[0].Resumable || len(got[0].Uncertain) != 1 || got[0].Uncertain[0] != "c_bash" {
		t.Fatalf("the next start should hand c_bash to the person as may-have-run; got %+v", got)
	}
	evs, _ = store.Read(ctx, sid, 0)
	n := 0
	for _, e := range evs {
		var d event.PartAppendedData
		if e.Type == event.TypePartAppended && json.Unmarshal(e.Data, &d) == nil && d.Part.ToolResult != nil && d.Part.ToolResult.CallID == "c_bash" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("c_bash has %d results, want exactly 1", n)
	}
	if b, _ := os.ReadFile(filepath.Join(wd, "marker")); strings.Count(string(b), "ran") != 1 {
		t.Fatalf("the tool ran %d times, want once", strings.Count(string(b), "ran"))
	}
}
