package app

import (
	"context"
	"strings"
	"testing"
)

// stepContractEnabled defaults ON; explicit off values disable the recovery step contract.
func TestStepContractFlag(t *testing.T) {
	t.Setenv("MAGI_STEP_CONTRACT", "")
	if !stepContractEnabled() {
		t.Error("default must be ON")
	}
	for _, off := range []string{"0", "off", "false", "no"} {
		t.Setenv("MAGI_STEP_CONTRACT", off)
		if stepContractEnabled() {
			t.Errorf("%q must disable", off)
		}
	}
}

// The stuck-recovery re-plan now authors per-step deliverable checks and GATES each unit on its
// own check: a unit whose check FAILS stays pending (its work is not accepted), while a unit whose
// check passes completes — the same contract every other plan step gets, extended to the recovery
// re-plan (the "on re-plan too, always define+check the step's checklist" requirement).
func TestDriveStuckTodosRecoveryGatesUnitChecks(t *testing.T) {
	t.Setenv("MAGI_STUCK_DECOMPOSE", "1")
	t.Setenv("MAGI_STEP_CONTRACT", "1")
	t.Setenv("MAGI_STEP_VERIFY", "1")
	t.Setenv("MAGI_CHECK_COVERAGE", "1")
	t.Setenv("MAGI_CHECK_VALIDATE", "1")

	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	checksJSON := `[{"step":"1","deliverable":"A out","command":"checkA"},{"step":"2","deliverable":"B out","command":"checkB"}]`
	llm := &recLLM{reply: func(req string) string {
		switch {
		case strings.Contains(req, "decompose THIS exact task"):
			return plan
		case strings.Contains(req, "FILLING GAPS"): // coverage authoring
			return checksJSON
		case strings.Contains(req, "review the executable deliverable"): // check-audit
			return checksJSON
		default: // every spawned unit lands (non-empty)
			return "landed"
		}
	}}
	a := newOrchApp(t, llm, Config{
		Permission: "allow", MaxAgents: 100, MaxDepth: 5, MaxPlanDepth: 2, MaxSteps: 120,
		Agents: map[string]AgentSpec{
			plannerAgent: {Name: "planner", System: "plan"},
			"coder":      {Name: "coder", System: "code", Tools: []string{"read", "write", "edit", "bash"}},
		},
	})
	plat := &scriptPlatform{codes: []int{1, 0}} // unit A's check FAILS, unit B's PASSES
	a.plat = plat
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.stateLocked(s.ID).meta = s
	a.mu.Unlock()

	a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "big stuck task", "reason", 0)

	if got := len(a.cachedChecks(s.ID)); got != 2 {
		t.Fatalf("recovery must author per-step checks for its plan, got %d", got)
	}
	td := a.Todos(s.ID)
	if len(td) != 2 {
		t.Fatalf("solo recovery replaces the list with 2 units, got %d", len(td))
	}
	if td[0].Status != "pending" {
		t.Errorf("unit A whose deliverable check FAILED must stay pending, got %q", td[0].Status)
	}
	if td[1].Status != "completed" {
		t.Errorf("unit B whose deliverable check PASSED must complete, got %q", td[1].Status)
	}
}
