package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type recordingObserver struct {
	mu       sync.Mutex
	users    []string
	finishes []string
	outcomes []string
}

func (r *recordingObserver) UserMessage(sid, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users = append(r.users, text)
}

func (r *recordingObserver) TurnFinished(sid string, o TurnObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishes = append(r.finishes, o.FinalText)
	r.outcomes = append(r.outcomes, o.Outcome)
}

func (r *recordingObserver) snapshot() (users, finishes []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.users...), append([]string(nil), r.finishes...)
}

// A submitted user prompt reaches Observer.UserMessage, and the finished turn
// reaches Observer.TurnFinished with the assistant's final text. System-actor
// injections never reach UserMessage.
func TestObserverReceivesConversationMilestones(t *testing.T) {
	obs := &recordingObserver{}
	a, wd := newApp(t, workingLLM(), Config{Observer: obs, Permission: "allow"})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}

	if err := a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "hello there"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		users, finishes := obs.snapshot()
		if len(users) > 0 && len(finishes) > 0 {
			if users[0] != "hello there" {
				t.Fatalf("UserMessage text = %q", users[0])
			}
			if strings.TrimSpace(finishes[0]) == "" {
				t.Fatalf("TurnFinished should carry the assistant's final text")
			}
			obs.mu.Lock()
			outcome := obs.outcomes[0]
			obs.mu.Unlock()
			// workingLLM does a read tool call, so this turn did real work but no
			// council ran in this harness → "ungated", never a fabricated
			// "verified" and never a silent "done" that hides the missing gate.
			if outcome != "ungated" {
				t.Fatalf("outcome = %q, want %q (tool-using turn, no council in harness)", outcome, "ungated")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	users, finishes := obs.snapshot()
	t.Fatalf("observer never saw the turn: users=%v finishes=%v", users, finishes)
}

// waitOutcome submits a prompt and returns the first observed outcome, or fails.
func waitOutcome(t *testing.T, a *App, obs *recordingObserver, sid session.SessionID, prompt string) string {
	t.Helper()
	ctx := context.Background()
	if err := a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: prompt}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "u"},
	}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		obs.mu.Lock()
		n := len(obs.outcomes)
		var got string
		if n > 0 {
			got = obs.outcomes[0]
		}
		obs.mu.Unlock()
		if n > 0 {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("observer never saw a finished turn")
	return ""
}

// With no council configured, a turn that used a tool must not be reported as a
// silent "done": the missing verification gate is made observable as "ungated"
// so a self-improvement observer never records an unconfirmed completion as a
// success. A purely conversational turn (no tools) stays "done".
func TestObserverUngatedOnToolTurnWithoutCouncil(t *testing.T) {
	obs := &recordingObserver{}
	a, wd := newApp(t, workingLLM(), Config{Observer: obs, Permission: "allow"})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitOutcome(t, a, obs, sid, "do some work"); got != "ungated" {
		t.Fatalf("tool-using turn without council: outcome = %q, want %q", got, "ungated")
	}
}

func TestObserverDoneOnConversationalTurn(t *testing.T) {
	obs := &recordingObserver{}
	// No tool call at all — a plain reply.
	a, wd := newApp(t, &fakeLLM{steps: [][]port.ProviderEvent{textStep("hi back")}}, Config{Observer: obs, Permission: "allow"})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitOutcome(t, a, obs, sid, "hello"); got != "done" {
		t.Fatalf("conversational turn: outcome = %q, want %q", got, "done")
	}
}
