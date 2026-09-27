package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// appAnswerable builds the app with or without the tool a person answers through — the registry a
// TUI or an attachable daemon gets, against the one a -p run with nobody to reach gets.
func appAnswerable(t *testing.T, llm port.LLMProvider, cfg Config, answerable bool) (*App, string) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := builtin.Default()
	builtin.RegisterOrchestration(reg, !answerable)
	a := New(store, llm, reg, bus.New(), nil, cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	return a, t.TempDir()
}

func needsPersonRound(n int) council.Deliberation {
	d := council.Deliberation{Round: 1, Decision: council.Continue}
	for i, m := range []string{"Melchior", "Balthasar", "Casper"} {
		v := council.Verdict{Member: m, Decision: council.Continue, Feedback: "please provide invoices.csv"}
		if i < n {
			v.NeedsPerson = "invoices.csv is not in the workspace — where is it, or should the answer be that it does not exist?"
		}
		d.Verdicts = append(d.Verdicts, v)
	}
	return d
}

// runOnce declares twice (the second only reached if the first did not end the turn) and returns
// the council calls made and the turn's finish.
func runOnce(t *testing.T, round council.Deliberation, answerable bool) (int, event.TurnFinishedData, string) {
	t.Helper()
	fc := &fakeCouncil{delibs: []council.Deliberation{round, {Round: 1, Decision: council.Done}}}
	llm := workingLLM(toolStep("council", `{"complete":true}`), textStep("where is invoices.csv?"),
		toolStep("council", `{"complete":true}`), textStep("done"))
	a, wd := appAnswerable(t, llm, Config{Council: fc, Permission: "allow"}, answerable)
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "count the rows of invoices.csv"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	var fin event.TurnFinishedData
	var said string
	for _, e := range waitForTerminal(t, a, sid) {
		if e.Type == event.TypeTurnFinished {
			_ = json.Unmarshal(e.Data, &fin)
		}
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "The council") {
			said = string(e.Data)
		}
	}
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.calls, fin, said
}

// When most members say the gap is one only the person can fill, and a person can be asked, the
// turn ends with the question instead of another rejection. Measured live (2026-09-27): members
// wrote "please provide the file" three rounds running, words for the person delivered to the
// agent, until the cap landed the turn.
func TestWhatOnlyThePersonCanSupplyEndsTheTurnWithAQuestion(t *testing.T) {
	calls, fin, said := runOnce(t, needsPersonRound(2), true)
	if calls != 1 {
		t.Errorf("the turn went on to another declaration (%d council calls) instead of asking", calls)
	}
	if !fin.Unverified || !strings.Contains(fin.Reason, "can only come from the person") {
		t.Errorf("the finish does not say it is waiting on the person: %+v", fin)
	}
	if !strings.Contains(said, "Your turn ends here") || !strings.Contains(said, "where is it") {
		t.Errorf("the agent was not told to end by asking for what is needed:\n%s", said)
	}
}

// The words that ended the turn are on the record, per member — a finish that says "can only come
// from the person" has to show whose reading that was.
func TestWhoSaidItNeedsThePersonIsRecorded(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{needsPersonRound(2)}}
	llm := workingLLM(toolStep("council", `{"complete":true}`), textStep("where is invoices.csv?"))
	a, wd := appAnswerable(t, llm, Config{Council: fc, Permission: "allow"}, true)
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "count the rows of invoices.csv"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	waitForTerminal(t, a, sid)
	evs, err := a.store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	said := 0
	for _, e := range evs {
		var v event.CouncilVerdictData
		if e.Type == event.TypeCouncilVerdict && json.Unmarshal(e.Data, &v) == nil && v.NeedsPerson != "" {
			said++
		}
	}
	if said != 2 {
		t.Errorf("%d recorded verdicts carry needs_person; the two members who said it should", said)
	}
}

// Nobody to ask — a -p run — keeps the old path: the rejection stands, and the agent declares again.
func TestWithNobodyToAskTheRejectionStands(t *testing.T) {
	calls, fin, _ := runOnce(t, needsPersonRound(3), false)
	if calls != 2 {
		t.Errorf("with nobody to ask, the rejection should stand and the agent declare again; council calls = %d", calls)
	}
	if strings.Contains(fin.Reason, "can only come from the person") {
		t.Errorf("a run nobody can reach ended on a question to the person: %+v", fin)
	}
}

// One member alone is not the council saying so.
func TestOneMemberSayingItNeedsThePersonIsNotEnough(t *testing.T) {
	if calls, _, _ := runOnce(t, needsPersonRound(1), true); calls != 2 {
		t.Errorf("one member's needs_person ended the turn; council calls = %d", calls)
	}
}
