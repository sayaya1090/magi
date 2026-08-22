package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// An UNVERIFIED landing is not an acceptance, and the plan must not be resolved as though it were.
//
// Observed on schemelike-metacircular-eval (2026-08-22): the agent wrote a three-step plan, said it
// would write the evaluator, called nothing, and the cap landed the turn with an empty workspace.
// The event right after that turn.finished — the one carrying Unverified=true — marked all three
// steps "completed". The record contradicted itself two lines apart, and the panel showed a
// finished plan for work that never started.
func TestUnverifiedLandingCancelsThePlanItNeverFinished(t *testing.T) {
	// One tool call to make this a working turn (the declaration gate skips conversational ones),
	// and it is a todowrite so a plan exists — as it did in the field. Every later step defaults to
	// text with no tool call, which is the failure being reproduced.
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("todowrite", `{"todos":[{"content":"write eval.scm","status":"in_progress"},`+
			`{"content":"verify it","status":"pending"}]}`),
	}}
	fc := &fakeCouncil{}
	a, wd := newApp(t, llm, Config{Permission: "allow", Council: fc})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "write a metacircular evaluator"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForTerminal(t, a, sid)

	// It landed undeclared: no council was ever asked to accept the work.
	landed := false
	for _, e := range got {
		if e.Type != event.TypeTurnFinished {
			continue
		}
		var d event.TurnFinishedData
		if json.Unmarshal(e.Data, &d) == nil && d.Unverified {
			landed = true
		}
	}
	if !landed {
		t.Fatalf("expected an UNVERIFIED landing to reproduce the defect, got %v", typesOf(got))
	}
	if fc.calls != 0 {
		t.Fatalf("no council should have been convened, got %d", fc.calls)
	}

	// So the plan resolves as a non-acceptance: what was left undone reads as left undone.
	for _, td := range a.Todos(sid) {
		if td.Status == "completed" {
			t.Errorf("an unverified landing marked %q completed — no council judged it satisfied", td.Content)
		}
	}
}
