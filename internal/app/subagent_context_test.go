package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Every per-item explorer is handed the overall goal; the scout's DISCOVERY explorer — the one
// that decides which items exist at all, and so what the whole fan-out will and will never look
// at — was handed only the planner's "discover" phrase. That phrase is written to be scoped by
// the task ("the source files in scope"), so without the task it is scoped by nothing.
func TestScoutDiscoveryExplorerIsOriented(t *testing.T) {
	got := scoutListPrompt("harden the retry path in the HTTP client", "the source files in scope")
	if !strings.Contains(got, "harden the retry path") {
		t.Errorf("the discovery explorer must be told what the list is FOR:\n%s", got)
	}
	if !strings.Contains(got, "List the source files in scope") {
		t.Errorf("the discover phrase must survive verbatim:\n%s", got)
	}
	// The output contract is what makes the reply parseable (parseList) — the goal must lead,
	// not displace it, and the "FIRST line must already be an item" rule must still be last so
	// it is not read as applying to the goal line.
	if i, j := strings.Index(got, "harden the retry"), strings.Index(got, "ONLY the items"); i > j {
		t.Errorf("the goal must LEAD the request, not trail the output contract:\n%s", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "or closing remark.") {
		t.Errorf("the output contract must stay last:\n%s", got)
	}
	// Step context off (the A/B baseline) → byte-identical to the pre-orientation prompt.
	bare := scoutListPrompt("", "docs/*.md")
	if strings.HasPrefix(bare, "Overall goal") || !strings.HasPrefix(bare, "List docs/*.md.") {
		t.Errorf("no goal means no orientation line at all:\n%s", bare)
	}
	if scoutListPrompt("   ", "docs/*.md") != bare {
		t.Error("a blank goal must be treated as no goal")
	}
}

// A refine child is CLONED from the parent, which is not the same as being told: the acceptance
// checklist lives in the parent's stored checks (the child's own set is empty until its own audit
// fills it with checks for its own sub-plan), the ledger's exact paths stop arriving once the
// shared refine session stops re-cloning, and the council's unresolved concern is an ActorSystem
// message — which cloneConversation drops outright. runStepGate judges the step against all three.
func TestRefineChildGetsWhatTheCloneCannotCarry(t *testing.T) {
	checklist := workerChecklist([]council.DeliverableCheck{
		{Step: "2", Deliverable: "the log file", Source: "build.log", Assert: "non-empty"},
	}, 1)
	if checklist == "" {
		t.Fatal("precondition: step 2's check must render a checklist")
	}
	ledger := renderLedger([]ledgerEntry{{Step: "write the parser", Facts: "internal/parse/lex.go"}})
	concern := concernBrief("nothing captures the build output")

	got := refineContext(checklist, ledger, concern)
	for _, want := range []string{"Acceptance checklist", "build.log", "internal/parse/lex.go", "nothing captures the build output"} {
		if !strings.Contains(got, want) {
			t.Errorf("refine context missing %q:\n%s", want, got)
		}
	}
	// Order matches the delegate worker's, so the two hand-offs cannot drift into describing the
	// same step differently.
	ci, li, ni := strings.Index(got, "Acceptance checklist"), strings.Index(got, "internal/parse/lex.go"), strings.Index(got, "nothing captures")
	if !(ci < li && li < ni) {
		t.Errorf("blocks must keep the delegate order checklist→ledger→concern:\n%s", got)
	}
	// It appends to a prompt, so it must lead with its own separation and add nothing when
	// there is nothing to add — a bare refine hand-off must stay byte-identical.
	if !strings.HasPrefix(got, "\n\n") {
		t.Errorf("blocks must separate themselves from the prompt they follow: %q", got[:8])
	}
	if refineContext("", "   ", "") != "" {
		t.Error("nothing to say must append nothing")
	}
	if refineContext() != "" {
		t.Error("no blocks at all must append nothing")
	}
}

// TestStepBudgetMatchesWhatTheAgentCanDo: the budget block's stop condition and landing move
// are the two places it tells an agent what "done" looks like, and both were written for an
// agent that writes code — "the primary deliverable is done and verified", "land the smallest
// change". A read-only spec cannot land a change, so that instruction cannot be obeyed, only
// worked around: it is the same push that had the repository explorer drafting a fix instead of
// reporting the path it had found. The ceiling and the anti-padding rule are shared; the verbs
// are not.
func TestStepBudgetMatchesWhatTheAgentCanDo(t *testing.T) {
	a := &App{}
	s := session.Session{ID: "s1"}
	readOnly := AgentSpec{Name: "specmine", Tools: specMineExploreTools}
	if specCanAct(readOnly) {
		t.Fatal("fixture is not read-only — the spec-mine allowlist must grant no write/edit/bash")
	}
	ro := a.volatileContext(context.Background(), s, readOnly, true, nil, nil, 6, 40, 0)
	acting := a.volatileContext(context.Background(), s, AgentSpec{Tools: []string{"read", "write", "bash"}}, true, nil, nil, 6, 40, 0)

	// Shared: both are paced by the same ceiling, and neither may pad to it.
	for _, out := range []string{ro, acting} {
		for _, want := range []string{"# Step budget", "step 7 of at most 40", "hard ceiling", "not a target", "only narrates"} {
			if !strings.Contains(out, want) {
				t.Fatalf("every agent's budget block should carry %q, got %q", want, out)
			}
		}
	}
	// The read-only block must not name an action its allowlist refuses.
	for _, unwanted := range []string{"land the smallest change", "actions that change or genuinely verify", "done and verified"} {
		if strings.Contains(ro, unwanted) {
			t.Errorf("a read-only agent was told to %q — it has no tool that can: %q", unwanted, ro)
		}
	}
	// …and it must still be told when to stop and how to land, in terms it can reach.
	for _, want := range []string{"facts you were asked to report", "report what you have already found", "a fact you do not have yet"} {
		if !strings.Contains(ro, want) {
			t.Errorf("a read-only agent's budget block should say %q, got %q", want, ro)
		}
	}
	// An acting agent is unchanged (this is a role split, not a rewrite for everyone).
	for _, want := range []string{"land the smallest change that satisfies the core requirement", "the task's primary deliverable is done and verified"} {
		if !strings.Contains(acting, want) {
			t.Errorf("an acting agent's budget block should still say %q, got %q", want, acting)
		}
	}
}

// TestSpecMineBriefKeepsTheRequestSomebodyElses: the explorer is handed the user's request so it
// knows what to look for, and a request is written in imperatives. Under a bare "TASK" header
// that reads as its own assignment — the plan below it was disclaimed, the request never was —
// and the agent goes to work on the request instead of mining facts for it.
func TestSpecMineBriefKeepsTheRequestSomebodyElses(t *testing.T) {
	brief := specMineBrief("make the parser accept negative numbers", "1. edit the lexer", "cmd/\ninternal/")

	// The request is present verbatim (it is what the explorer looks up), but disclaimed.
	if !strings.Contains(brief, "make the parser accept negative numbers") {
		t.Fatal("the request must reach the explorer verbatim — it is what the search is for")
	}
	if strings.Contains(brief, "── TASK\n") {
		t.Errorf("the request must not be headed as this agent's task: %q", brief)
	}
	req := strings.Index(brief, "── THE REQUEST")
	if req < 0 || !strings.Contains(brief[req:strings.Index(brief, "make the parser")], "SOMEONE ELSE") {
		t.Errorf("the request's own header must say whose it is: %q", brief)
	}
	if !strings.Contains(brief, "do NOT carry it out") {
		t.Errorf("the plan must keep its disclaimer: %q", brief)
	}
	// The instruction the model acts on comes last, and it names a deliverable this agent can
	// actually produce with read tools.
	job := strings.Index(brief, "── YOUR JOB")
	if job < 0 || job < strings.Index(brief, "── THEIR PLAN") {
		t.Fatalf("the job must come after the context it is about: %q", brief)
	}
	if !strings.Contains(brief[job:], "IS your deliverable") || !strings.Contains(brief[job:], "not a fix") {
		t.Errorf("the job must name its deliverable and rule out doing the work: %q", brief[job:])
	}
	if !strings.Contains(brief, "1. edit the lexer") || !strings.Contains(brief, "internal/") {
		t.Errorf("plan and repo map must survive: %q", brief)
	}
}
