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

// A round in which no member voted: one returned no decision, two returned nothing. This is the
// shape a slow local model produced as the whole panel (2026-09-27).
func unjudgedRound() council.Deliberation {
	vs := []council.Verdict{
		{Member: "Melchior", Decision: council.Abstain},
		{Member: "Balthasar", Decision: council.Abstain, Silent: true},
		{Member: "Casper", Decision: council.Abstain, Silent: true},
	}
	_, b := council.Tally(vs, council.RuleMajority)
	return council.Deliberation{Round: 1, Decision: council.Continue, Verdicts: vs, Breakdown: b}
}

// Nobody voted, so nobody rejected: the agent is told the work was not judged — not that it was
// turned away with nothing to address — and the turn still does not end on no verdict.
func TestAnUnjudgedDeclarationIsNotCalledARejection(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{unjudgedRound(), {Round: 1, Decision: council.Done}}}
	llm := workingLLM(toolStep("council", `{"complete":true}`), toolStep("council", `{"complete":true}`), textStep("done"))
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "sum the column"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	var said string
	for _, e := range waitForTerminal(t, a, sid) {
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "could not judge") {
			said = string(e.Data)
		}
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "does NOT accept") {
			t.Errorf("an unjudged round was delivered as a rejection:\n%s", e.Data)
		}
	}
	if !strings.Contains(said, "NOT a rejection") || !strings.Contains(said, "2 gave no answer") {
		t.Errorf("the agent was not told the round went unjudged:\n%s", said)
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.calls != 2 {
		t.Errorf("the turn should go on to a second declaration after an unjudged one; council calls = %d", fc.calls)
	}
}

// A council that never answers does not hold the turn open forever — and the landing says what
// happened, not that the council rejected anything.
func TestACouncilThatNeverAnswersLandsTheTurnSayingSo(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{unjudgedRound(), unjudgedRound(), unjudgedRound(), unjudgedRound()}}
	llm := workingLLM(toolStep("council", `{"complete":true}`), toolStep("council", `{"complete":true}`),
		toolStep("council", `{"complete":true}`), textStep("not verified"), toolStep("council", `{"complete":true}`))
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "sum the column"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	var fin event.TurnFinishedData
	for _, e := range waitForTerminal(t, a, sid) {
		if e.Type == event.TypeTurnFinished {
			_ = json.Unmarshal(e.Data, &fin)
		}
	}
	if !fin.Unverified || !strings.Contains(fin.Reason, "could not judge 3 declarations") {
		t.Errorf("the landing does not say the council could not judge: %+v", fin)
	}
	if strings.Contains(fin.Reason, "rejected") {
		t.Errorf("the landing says the council rejected something it never judged: %q", fin.Reason)
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.calls != 3 {
		t.Errorf("council calls = %d, want the landing on the third unjudged declaration", fc.calls)
	}
}
