package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A turn whose council never approves (round-cap deadlock) must land its
// turn.finished with Unverified=true and a reason — the TUI reads that field to
// distinguish an abandoned/unverified turn from a genuine done. Regression: the
// deadlock recorded an UNVERIFIED council note but emitted a clean turn.finished
// (Unverified=false), so the UI painted an abandoned task as a confident "done".
func TestDeadlockFinishMarkedUnverified(t *testing.T) {
	// Council that always says "keep working" → it never approves.
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Decision: council.Continue, Feedback: "the review is not complete yet"},
	}}
	// tool work (so the gate applies) then two finishes: round 1 → Continue,
	// round 2 → round-cap deadlock finish.
	llm := workingLLM(textStep("answer 1"), textStep("answer 2"))
	a, wd := newApp(t, llm, Config{Council: fc, CouncilMaxRounds: 1, Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "review the whole project"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForTerminal(t, a, sid)

	// The council recorded the finish as unverified...
	unverifiedNote := false
	for _, e := range got {
		if e.Type == event.TypeCouncilDecided && strings.Contains(string(e.Data), "UNVERIFIED") {
			unverifiedNote = true
		}
	}
	if !unverifiedNote {
		t.Fatal("setup: expected a council UNVERIFIED deadlock note")
	}

	// ...and the turn.finished the TUI reads carries it.
	var tf *event.TurnFinishedData
	for _, e := range got {
		if e.Type == event.TypeTurnFinished {
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) == nil {
				tf = &d
			}
		}
	}
	if tf == nil {
		t.Fatal("no turn.finished emitted")
	}
	if !tf.Unverified {
		t.Fatal("deadlock finish must set turn.finished Unverified=true so the UI does not show a clean done")
	}
	if strings.TrimSpace(tf.Reason) == "" {
		t.Error("an unverified finish should carry a human-readable reason")
	}
}

// A turn the council genuinely approves finishes verified: Unverified=false, no
// reason — the common case must not be mislabeled by the propagation above.
func TestApprovedFinishNotUnverified(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	llm := workingLLM(textStep("the answer"))
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the thing"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForTerminal(t, a, sid)

	for _, e := range got {
		if e.Type == event.TypeTurnFinished {
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) == nil && d.Unverified {
				t.Fatalf("a council-approved finish must be verified, got Unverified=true reason=%q", d.Reason)
			}
		}
	}
}
