package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A cloned child's only ActorUser prompt is the inherited-context banner, whose text says it is NOT
// the task. Asking this session what it was asked to do must yield the seed the dispatcher gave it,
// never the banner — live, the banner reached a plan-audit council as "# Current request to plan
// for", all three members abstained saying there was no task, and the gate approved on zero votes.
func TestLastUserPromptSkipsTheInheritedBanner(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &usageLLM{text: "x"}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	appendPrompt := func(text string, actor event.Actor) {
		t.Helper()
		d, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: "m_" + text[:3], Parts: []session.Part{{Kind: session.PartText, Text: text}}})
		if aerr := a.appendFact(ctx, sid, event.TypePromptSubmitted, actor, d); aerr != nil {
			t.Fatal(aerr)
		}
	}
	const unit = "Now carry out ONLY THIS ONE unit, then stop:\nBuild the compiler following HACKING.adoc"
	appendPrompt(inheritedContextHeader, event.Actor{Kind: event.ActorUser, ID: "cli"})
	appendPrompt(unit, event.Actor{Kind: event.ActorAgent, ID: "default"})
	appendPrompt("# Investigation findings (from the explorer subagents you just dispatched)",
		event.Actor{Kind: event.ActorSystem, ID: "planner"})

	got := a.lastUserPrompt(ctx, sid)
	if got == inheritedContextHeader {
		t.Fatal("the banner says it is background only — it must never be read as the request")
	}
	if got != unit {
		t.Errorf("a dispatched child's request is its seed unit\n got: %q\nwant: %q", got, unit)
	}

	// A top-level session is unaffected: its genuine user prompt still wins, and a later system
	// injection must not displace it.
	store2, _ := jsonl.New(t.TempDir())
	b := New(store2, &usageLLM{text: "x"}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	top, err := b.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_top",
		Parts: []session.Part{{Kind: session.PartText, Text: "fix the GC crash"}}})
	if aerr := b.appendFact(ctx, top, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "cli"}, d); aerr != nil {
		t.Fatal(aerr)
	}
	sd, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_sys",
		Parts: []session.Part{{Kind: session.PartText, Text: "# Investigation findings"}}})
	if aerr := b.appendFact(ctx, top, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "planner"}, sd); aerr != nil {
		t.Fatal(aerr)
	}
	if got := b.lastUserPrompt(ctx, top); got != "fix the GC crash" {
		t.Errorf("a top-level request must be unchanged, got %q", got)
	}
}

// The acceptance contract belongs to the RUN. A child re-planning its unit never runs the contract
// gate itself (top-level only, on purpose), so asking for the contract in that child returned "" and
// its audit council authored checks with nothing to anchor them.
func TestContractReachesAChildThatNeverRanTheGate(t *testing.T) {
	a := newOrchApp(t, &gateLLM{text: "x"}, Config{Permission: "allow", MaxAgents: 10})
	parent := parentSession(t.TempDir())
	child := parentSession(t.TempDir())
	child.Parent = parent.ID
	a.mu.Lock()
	a.stateLocked(parent.ID).meta = parent
	a.stateLocked(child.ID).meta = child
	a.stateLocked(parent.ID).contractFrozen = true
	a.stateLocked(parent.ID).contractText = "Acceptance criteria (each must hold):\n- the compiler bootstraps"
	a.mu.Unlock()

	if got := a.contractForPlanner(child.ID); got != a.contractForPlanner(parent.ID) {
		t.Errorf("a child must plan against the run's contract, got %q", got)
	}
	// An unfrozen parent still yields nothing — this resolves a contract, it does not invent one.
	a.mu.Lock()
	a.stateLocked(parent.ID).contractFrozen = false
	a.mu.Unlock()
	if got := a.contractForPlanner(child.ID); got != "" {
		t.Errorf("no frozen contract anywhere must stay empty, got %q", got)
	}
}

// A severity-gated approval and an all-abstain approval take the same branch and used to be
// recorded identically — 0 done / 0 continue / 3 abstain, decision "done", empty note. The record
// has to say which one it was.
func TestNoVoteApprovalSaysSo(t *testing.T) {
	real := council.Breakdown{Done: 2, Continue: 1, Voters: 3}
	if got := withNoVoteNote("", real); got != "" {
		t.Errorf("a plan members actually voted on needs no caveat, got %q", got)
	}
	if got := withNoVoteNote("plan approved with advisory notes (non-blocking)", real); got != "plan approved with advisory notes (non-blocking)" {
		t.Errorf("an existing note must survive untouched, got %q", got)
	}
	// An all-zero breakdown is no tally at all, not an abstention — it must not be annotated.
	if got := withNoVoteNote("", council.Breakdown{}); got != "" {
		t.Errorf("an empty tally carries no claim about agreement, got %q", got)
	}
	none := council.Breakdown{Abstain: 3, Voters: 0}
	got := withNoVoteNote("", none)
	for _, want := range []string{"no member voted", "3 abstained", "not the same as agreement"} {
		if !strings.Contains(got, want) {
			t.Errorf("an all-abstain approval must say so: %q missing from %q", want, got)
		}
	}
	if both := withNoVoteNote("plan approved with advisory notes (non-blocking)", none); !strings.Contains(both, "advisory") || !strings.Contains(both, "no member voted") {
		t.Errorf("both facts must survive together, got %q", both)
	}
}

// The cap bounds ONE level and drops what overruns it. Both the planner that spends the budget and
// the council that demands another step have to be told — live, members repeatedly asked for an
// extra step on a plan with no room for one, and the revision "complied" by folding two actions
// into an existing step.
func TestStepBudgetIsStated(t *testing.T) {
	note := stepBudgetNote()
	if !strings.Contains(note, fmt.Sprintf("at most %d ordered steps", maxPlanSteps)) {
		t.Errorf("the note must state the real cap (%d), got %q", maxPlanSteps, note)
	}
	for _, want := range []string{"DROPPED", "per LEVEL", "fresh budget", "never fold two distinct actions"} {
		if !strings.Contains(note, want) {
			t.Errorf("the way out of the cap must be stated: %q missing from %q", want, note)
		}
	}
}
