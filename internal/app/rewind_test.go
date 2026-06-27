package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
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
func submitSync(t *testing.T, a *App, sid session.SessionID, text string) {
	t.Helper()
	_, lastSeq, _ := a.SessionState(context.Background(), sid)
	ch, cancel, err := a.Subscribe(context.Background(), sid, lastSeq)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	a.Submit(context.Background(), command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
	for e := range ch {
		if e.Type == "turn.finished" || e.Type == "error" {
			return
		}
	}
}
