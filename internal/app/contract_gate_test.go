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

// A CRITICAL revision in round 1 drives one refining round; the round-2 (strengthened) contract
// wins, and the refinement carried the round-1 concern back into the council.
func TestRunContractGateRefinesOnCritical(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{
			Decision: council.Continue,
			Verdicts: []council.Verdict{{Member: "Balthasar", Decision: council.Continue,
				Severity: council.SeverityCritical, Feedback: "check only proves import, never runs the behavior"}},
			Criteria: []string{"the module imports"},
			Checks:   []council.DeliverableCheck{{Command: "python3 -c 'import x'"}},
		},
		{
			Decision: council.Done,
			Verdicts: []council.Verdict{{Member: "Balthasar", Decision: council.Done}},
			Criteria: []string{"running the module on the example produces the stated output"},
			Checks:   []council.DeliverableCheck{{Command: "python3 run.py", Expect: "ok"}},
		},
	}}
	ctx := context.Background()
	a, sid, _ := newWorkflowApp(t, nil, nil, Config{Permission: "allow", Council: fc, CouncilMaxRounds: 3})
	s := a.sessionInfo(ctx, sid)

	a.runContractGate(ctx, s, "implement the module")

	if fc.calls != 2 {
		t.Fatalf("expected 2 contract rounds (critical then done), got %d", fc.calls)
	}
	if got := a.cachedCriteria(sid); !strings.Contains(got, "produces the stated output") {
		t.Fatalf("final contract should be the strengthened round-2 one: %q", got)
	}
	if !strings.Contains(fc.lastReq.Plan, "only proves import") {
		t.Fatalf("refining round did not carry the round-1 concern: %q", fc.lastReq.Plan)
	}
}
