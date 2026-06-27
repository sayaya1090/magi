package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// waitIdle blocks until the session's run goroutine has fully exited, so a
// follow-up (fork/diff) or t.TempDir cleanup can't race a still-flushing store
// write. turn.finished is published before the goroutine's final teardown, so
// waitForTerminal alone is not enough.
func waitIdle(t *testing.T, a *App, sid session.SessionID) {
	t.Helper()
	for i := 0; i < 2000; i++ {
		a.mu.Lock()
		running := a.cancels[sid] != nil
		a.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("run goroutine did not become idle")
}

func startSession(t *testing.T, a *App, wd string) session.SessionID {
	t.Helper()
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "u"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return sid
}

func runOn(t *testing.T, a *App, sid session.SessionID, text string) {
	t.Helper()
	if err := a.Submit(context.Background(), command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	waitForTerminal(t, a, sid)
	waitIdle(t, a, sid) // ensure the run goroutine finished writing before we fork/diff
}

func TestForkCopiesHistoryIndependently(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx := context.Background()
	sid := startSession(t, a, wd)
	runOn(t, a, sid, "first task")

	origEvs, _ := a.store.Read(ctx, sid, 0)
	fork, err := a.Fork(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if fork == sid {
		t.Fatal("fork must have a new session id")
	}
	forkEvs, _ := a.store.Read(ctx, fork, 0)
	if len(forkEvs) != len(origEvs) {
		t.Fatalf("fork has %d events, origin %d", len(forkEvs), len(origEvs))
	}
	if forkEvs[0].Type != event.TypeSessionCreated {
		t.Fatal("fork must start with session.created")
	}
	for _, e := range forkEvs {
		if e.SessionID != fork {
			t.Fatalf("fork event carries sid %q, want %q", e.SessionID, fork)
		}
	}

	// Diverging the fork must not touch the origin.
	runOn(t, a, fork, "second task on the branch")
	o2, _ := a.store.Read(ctx, sid, 0)
	if len(o2) != len(origEvs) {
		t.Fatalf("origin changed after the fork diverged: %d → %d", len(origEvs), len(o2))
	}
}

func TestSessionDiffShowsDivergence(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx := context.Background()
	sid := startSession(t, a, wd)
	runOn(t, a, sid, "task")

	fork, err := a.Fork(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Identical right after fork.
	same, _ := a.SessionDiff(ctx, sid, fork)
	if strings.Contains(same, "≠") {
		t.Fatalf("fresh fork should match its origin:\n%s", same)
	}
	// Diverge the fork; the diff must now flag the turn-count difference.
	runOn(t, a, fork, "extra turn")
	diff, _ := a.SessionDiff(ctx, sid, fork)
	if !strings.Contains(diff, "≠") || !strings.Contains(diff, "turns") {
		t.Fatalf("diff should flag divergence:\n%s", diff)
	}
}
