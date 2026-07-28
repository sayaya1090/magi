package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
)

// keep is produced by the MEMBER and consumed by whatever REWRITES afterwards, so a phase that
// forgets either half loses it silently — the request flag is invisible in the log, and a rewrite
// that dropped a blessed item still looks like a normal plan/contract. Each phase that can trigger
// a rewrite is pinned here.

func TestWithKeepAdviceIsAdditiveAndFlagged(t *testing.T) {
	vs := []council.Verdict{
		{Member: "Melchior", Lens: "correctness", Decision: council.Done, Keep: "the parser change is correct"},
		{Member: "Casper", Decision: council.Continue, Feedback: "add a test"},
	}
	got := withKeepAdvice("add a test", vs)
	for _, want := range []string{"add a test", "the parser change is correct", "advice, not a rule"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	// Off → byte-identical to the instruction alone, so the A/B arm is a real baseline.
	t.Setenv("MAGI_COUNCIL_KEEP", "0")
	if off := withKeepAdvice("add a test", vs); off != "add a test" {
		t.Errorf("with the flag off the instruction must be unchanged, got %q", off)
	}
	t.Setenv("MAGI_COUNCIL_KEEP", "1")
	// No member named anything → nothing appended (no empty header).
	if got := withKeepAdvice("add a test", []council.Verdict{{Decision: council.Done}}); got != "add a test" {
		t.Errorf("an empty keep must append nothing, got %q", got)
	}
	// keep alone is not an instruction: sending it by itself would read as "you are done".
	if got := withKeepAdvice("  ", vs); got != "" {
		t.Errorf("keep must never stand alone as the instruction, got %q", got)
	}
}

// The contract is revised by CONSOLIDATION — a REPLACE of the whole criteria list — so it is the
// phase most exposed to a rewrite dropping an approved condition. The consolidator must be told
// both what to fix and what to preserve.
func TestContractGateAsksForKeepAndCarriesItIntoConsolidation(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue,
			Criteria: []string{"the server answers requests"},
			Verdicts: []council.Verdict{
				{Member: "Casper", Lens: "completeness", Decision: council.Continue,
					Severity: council.SeverityCritical, Feedback: "reduce to the essential conditions"},
				// Deliberately unlike anything the fake consolidator echoes back, so a hit can only
				// have come from the keep and not from the contract text quoted in the prompt.
				{Member: "Melchior", Lens: "correctness", Decision: council.Done,
					Keep: "the round-trip condition is already right"},
			}},
		{Round: 2, Decision: council.Done, Verdicts: []council.Verdict{{Member: "Melchior", Decision: council.Done}}},
	}}
	llm := &recLLM{reply: func(string) string { return `{"criteria":["the stored value comes back"]}` }}
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, llm, nil, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	a.runContractGate(ctx, a.sessionInfo(ctx, sid), "build a store")

	if len(fc.reqs) == 0 {
		t.Fatal("the contract gate did not deliberate")
	}
	if !fc.reqs[0].Keep {
		t.Error("the contract gate must ask members for `keep` — consolidation REPLACES the criteria")
	}
	llm.mu.Lock()
	defer llm.mu.Unlock()
	var applied string
	for _, p := range llm.prompts {
		if strings.Contains(p, "reduce to the essential conditions") {
			applied = p
		}
	}
	if applied == "" {
		t.Fatalf("no consolidation prompt carried the feedback; prompts=%d", len(llm.prompts))
	}
	if !strings.Contains(applied, "the round-trip condition is already right") {
		t.Errorf("the consolidator was told what to fix but not what to preserve:\n%s", applied)
	}
}

// The plan audit's ADVISORY path is a rewrite too when absorb is on, and even when it is off the
// executor acts on the note — which is exactly when it might redo a part a lens had settled. An
// approving round must therefore carry keep as well, not only a blocking one.
func TestPlanAuditAdvisoryNoteCarriesKeep(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Round: 1, Decision: council.Done,
		Verdicts: []council.Verdict{
			{Member: "Casper", Lens: "completeness", Decision: council.Continue,
				Severity: council.SeverityWarn, Feedback: "consider capturing the build output"},
			{Member: "Melchior", Lens: "correctness", Decision: council.Done,
				Keep: "step 2 already locates the right source"},
		},
	}}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	txt := sessionText(t, a, s.ID)
	if !strings.Contains(txt, "consider capturing the build output") {
		t.Fatalf("the advisory note was not injected:\n%s", txt)
	}
	if !strings.Contains(txt, "step 2 already locates the right source") {
		t.Errorf("an approving round dropped the keep — the executor is told what to add but not what to leave alone:\n%s", txt)
	}
}

// The reason this whole mechanism could sit dead for five days is that the run log was byte-identical
// whether keep fired or not. Both halves have to be on the record: what the round ASKED for (else an
// empty keep is ambiguous between "nobody was asked" and "asked, none answered") and what each member
// ANSWERED. A gate that quietly stops asking must become visible in the artifact, not just in a test.
func TestCouncilFactsRecordKeepAskedAndAnswered(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Round: 1, Decision: council.Done,
		Verdicts: []council.Verdict{
			{Member: "Melchior", Lens: "correctness", Decision: council.Done,
				Keep: "step 2 already locates the right source"},
		},
	}}}
	a, wd := newApp(t, &fakeLLM{}, Config{Council: fc, Agents: map[string]AgentSpec{plannerAgent: {Name: "planner"}}})
	s, steps := planAuditFixture(t, a, wd)
	a.runPlanAuditGate(context.Background(), s, a.cfg.Agents[plannerAgent], "do A and B", steps, 0, 120)

	var asked, answered bool
	for _, e := range mustRead(t, a, s.ID) {
		switch e.Type {
		case event.TypeCouncilConvened:
			var d event.CouncilConvenedData
			if json.Unmarshal(e.Data, &d) == nil && d.Keep {
				asked = true
			}
		case event.TypeCouncilVerdict:
			var d event.CouncilVerdictData
			if json.Unmarshal(e.Data, &d) == nil && d.Keep == "step 2 already locates the right source" {
				answered = true
			}
		}
	}
	if !asked {
		t.Error("the convened fact must record that the round asked for keep — otherwise an empty keep is ambiguous")
	}
	if !answered {
		t.Error("the verdict fact must record the member's keep — otherwise the run log cannot show the mechanism fired")
	}
}
