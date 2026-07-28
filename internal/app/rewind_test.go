package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Rewind removes the last user turn; the prior turn's history remains.
func TestRewind(t *testing.T) {
	llm := &usageLLM{text: "reply"}
	store, _ := jsonl.New(t.TempDir())
	a := New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	// Two turns.
	submitSync(t, a, sid, "first")
	submitSync(t, a, sid, "second")

	before, _, _ := a.SessionState(context.Background(), sid)
	if countUser(before) != 2 {
		t.Fatalf("expected 2 user turns, got %d", countUser(before))
	}

	if _, err := a.Rewind(context.Background(), sid, 1); err != nil {
		t.Fatalf("rewind: %v", err)
	}
	after, _, _ := a.SessionState(context.Background(), sid)
	if countUser(after) != 1 {
		t.Errorf("after rewind expected 1 user turn, got %d", countUser(after))
	}
}

// Rewind on a fresh session (no prompts) errors gracefully.
func TestRewindNothing(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &usageLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if _, err := a.Rewind(context.Background(), sid, 1); err == nil {
		t.Error("rewind with no prompts should error")
	}
}

func countUser(msgs []session.Message) int {
	n := 0
	for _, m := range msgs {
		if m.Role == session.RoleUser {
			n++
		}
	}
	return n
}

// submitSync submits a prompt and waits for that turn to finish (subscribing
// from the current end so it only sees the new turn's events).
//
// The Actor is not decoration. Every production caller of Submit stamps ActorUser (tui, cli, eval),
// and the loop's finish-boundary recovery is built on that: hasUnansweredPrompt and userPromptEntries
// both count ONLY ActorUser prompts. A prompt submitted without one is invisible to them — so when
// startRun refuses it (st.cancel is still set while the previous turn's goroutine retires), nothing
// downstream ever picks it up and the turn simply never runs. That was a real 1-in-N hang here: the
// log ended `seq=4 turn.finished / seq=5 prompt.submitted` with nothing after, and the test waited
// forever on a turn.finished that had no goroutine left to emit it.
func submitSync(t *testing.T, a *App, sid session.SessionID, text string) {
	t.Helper()
	_, lastSeq, _ := a.SessionState(context.Background(), sid)
	ch, cancel, err := a.Subscribe(context.Background(), sid, lastSeq)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: text}},
		Actor: event.Actor{Kind: event.ActorUser, ID: "test"}})
	for e := range ch {
		if e.Type == "turn.finished" || e.Type == "error" {
			return
		}
	}
}

// Rewind must clear ALL turn-derived caches that feed the plan/council panels — not only todos/criteria/
// estSteps — so a truncated turn's deliverable checks, ledger, and frozen contract don't render over the
// restored (older) state: they belonged to the rewound-away prompt. (Regression guard for the class where
// a new state field is added to one reset path but missed on another.)
func TestRewindClearsDerivedCaches(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &usageLLM{text: "reply"}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	submitSync(t, a, sid, "do the thing")

	// Seed the turn-derived caches as a completed plan-audit turn would.
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.contractFrozen = true
	st.contractText = "contract"
	st.minedNote = "note"
	a.mu.Unlock()

	if _, err := a.Rewind(context.Background(), sid, 1); err != nil {
		t.Fatalf("rewind: %v", err)
	}

	a.mu.Lock()
	st = a.stateLocked(sid)
	bad := st.contractFrozen || st.contractText != "" || st.minedNote != ""
	a.mu.Unlock()
	if bad {
		t.Error("Rewind must clear contractFrozen/contractText/minedNote")
	}
}
