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
			// No council configured in this harness → a plain finish, never a
			// fabricated "verified".
			if outcome != "done" {
				t.Fatalf("outcome = %q, want %q (no council in harness)", outcome, "done")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	users, finishes := obs.snapshot()
	t.Fatalf("observer never saw the turn: users=%v finishes=%v", users, finishes)
}
