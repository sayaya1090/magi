package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// contractFirstEnabled defaults ON; explicit off values disable it.
func TestContractFirstFlag(t *testing.T) {
	t.Setenv("MAGI_CONTRACT_FIRST", "")
	if !contractFirstEnabled() {
		t.Error("default must be ON")
	}
	for _, off := range []string{"0", "off", "false", "no"} {
		t.Setenv("MAGI_CONTRACT_FIRST", off)
		if contractFirstEnabled() {
			t.Errorf("%q must disable", off)
		}
	}
}

func TestRenderContract(t *testing.T) {
	out := renderContract(
		[]string{"the server answers SetVal", ""},
		[]council.DeliverableCheck{
			{Deliverable: "grpc round-trip", Command: "python3 client.py", Expect: "val=42"},
			{Command: ""}, // no command → skipped
		},
	)
	for _, want := range []string{"Acceptance criteria", "answers SetVal", "Executable checks", "python3 client.py", "expect: val=42"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "- \n") { // the empty criterion must be dropped, not rendered as a bare bullet
		t.Errorf("empty criterion rendered:\n%s", out)
	}
}

// runContractGate authors+reviews the contract, stores the criteria, FREEZES them (so the later
// plan-audit cannot overwrite), stashes the checks, and exposes the contract to the planner.
func TestRunContractGateFreezesContract(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{
		Decision: council.Done,
		Verdicts: []council.Verdict{{Member: "Melchior", Decision: council.Done}},
		Criteria: []string{"the server answers SetVal and returns the stored value"},
		Checks:   []council.DeliverableCheck{{Deliverable: "grpc round-trip", Command: "python3 client.py", Expect: "val=42"}},
	}}}
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	s := a.sessionInfo(ctx, sid)

	a.runContractGate(ctx, s, "build a kv-store grpc server")

	if got := a.cachedCriteria(sid); !strings.Contains(got, "SetVal") {
		t.Fatalf("contract criteria not stored: %q", got)
	}
	a.mu.Lock()
	frozen := a.stateLocked(sid).contractFrozen
	stashed := a.stateLocked(sid).contractChecks
	a.mu.Unlock()
	if !frozen {
		t.Fatal("contract should be frozen after the gate")
	}
	if len(stashed) != 1 || stashed[0].Expect != "val=42" {
		t.Fatalf("contract checks not stashed: %+v", stashed)
	}
	if p := a.contractForPlanner(sid); !strings.Contains(p, "val=42") || !strings.Contains(p, "SetVal") {
		t.Fatalf("planner contract missing items: %q", p)
	}

	// The later plan-audit must NOT overwrite the frozen, reviewed criteria.
	a.storePlanCriteria(ctx, s, []string{"a weaker criterion the plan-audit derived"})
	if got := a.cachedCriteria(sid); strings.Contains(got, "weaker") {
		t.Fatalf("frozen criteria were overwritten by plan-audit: %q", got)
	}
}

// assignContractChecksToSteps labels the frozen contract's (initially step-less) checks with the
// plan step that produces each, so the per-step delegate gate can match them.
func TestAssignContractChecksToSteps(t *testing.T) {
	t.Setenv("MAGI_STEP_VERIFY", "1")
	t.Setenv("MAGI_CHECK_VALIDATE", "0") // skip the validate side-call; store the assigned set as-is
	assigned := `[{"step":"2","deliverable":"grpc round-trip","command":"python3 client.py","expect":"val=42"}]`
	llm := &recLLM{reply: func(req string) string {
		if strings.Contains(req, "assign each executable deliverable CHECK") {
			return assigned
		}
		return ""
	}}
	a := newOrchApp(t, llm, Config{Permission: "allow", MaxSteps: 60})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	st := a.stateLocked(s.ID)
	st.meta = s
	st.contractFrozen = true
	st.deliverableChecks = []council.DeliverableCheck{{Deliverable: "grpc round-trip", Command: "python3 client.py", Expect: "val=42"}}
	a.mu.Unlock()

	a.assignContractChecksToSteps(context.Background(), s, []planStep{{Title: "scaffold"}, {Title: "implement server"}})

	got := a.cachedChecks(s.ID)
	if len(got) != 1 || got[0].Step != "2" {
		t.Fatalf("contract check should be assigned to step 2, got %+v", got)
	}
}

// A CRITICAL revision in round 1 drives a CONSOLIDATION that APPLIES the feedback (not a re-merge of
// member proposals, which only grows the contract) — the strengthened contract wins in round 2.
func TestRunContractGateRefinesOnCritical(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{ // round 1: critical revision, seeds an initial (weak) contract
			Decision: council.Continue,
			Verdicts: []council.Verdict{{Member: "Balthasar", Decision: council.Continue,
				Severity: council.SeverityCritical, Feedback: "check only proves import, never runs the behavior"}},
			Criteria: []string{"the module imports"},
			Checks:   []council.DeliverableCheck{{Command: "python3 -c 'import x'"}},
		},
		{ // round 2: approves the consolidated contract
			Decision: council.Done,
			Verdicts: []council.Verdict{{Member: "Balthasar", Decision: council.Done}},
		},
	}}
	var consolidateInput string
	llm := &recLLM{reply: func(req string) string {
		if strings.Contains(req, "revise a task's acceptance contract by APPLYING") { // the consolidation side-call
			consolidateInput = req
			return `{"criteria":["running the module on the example produces the stated output"],"checks":[{"deliverable":"runs","command":"python3 run.py","expect":"ok"}]}`
		}
		return "" // elicit-draft returns nothing → round 1 seeds from the members
	}}
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, llm, nil, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	s := a.sessionInfo(ctx, sid)

	a.runContractGate(ctx, s, "implement the module")

	if got := a.cachedCriteria(sid); !strings.Contains(got, "produces the stated output") {
		t.Fatalf("final contract should be the CONSOLIDATED (feedback-applied) one: %q", got)
	}
	if !strings.Contains(consolidateInput, "only proves import") {
		t.Fatalf("consolidation must receive the round-1 feedback to apply: %q", consolidateInput)
	}
}
