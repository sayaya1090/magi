package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// stuckDecomposeEnabled defaults ON and only an explicit off-value flips it, so the
// decomposing recovery is the default and MAGI_STUCK_DECOMPOSE=off restores the baseline.
func TestStuckDecomposeEnabledDefault(t *testing.T) {
	if !stuckDecomposeEnabled() {
		t.Fatal("default must be ON")
	}
	for _, v := range []string{"0", "off", "false", "no", "OFF"} {
		t.Setenv("MAGI_STUCK_DECOMPOSE", v)
		if stuckDecomposeEnabled() {
			t.Errorf("%q must disable", v)
		}
	}
	for _, v := range []string{"1", "on", "true", "whatever"} {
		t.Setenv("MAGI_STUCK_DECOMPOSE", v)
		if !stuckDecomposeEnabled() {
			t.Errorf("%q must leave it ON", v)
		}
	}
}

// stuckUnitPrompt scopes a child to ONE unit, tells it (context already cloned) not to
// re-read, and carries the block reason only on the first unit.
func TestStuckUnitPrompt(t *testing.T) {
	st := planStep{Title: "edit init.lua", Task: "replace the hardcoded window with the API value"}
	first := stuckUnitPrompt(st, "kept re-reading the same file", true)
	if !strings.Contains(first, "replace the hardcoded window") {
		t.Error("must contain the unit task")
	}
	if !strings.Contains(first, "do NOT re-read") {
		t.Error("must tell the context-cloned child not to re-read")
	}
	if !strings.Contains(first, "kept re-reading the same file") {
		t.Error("first unit must carry the block reason")
	}
	rest := stuckUnitPrompt(st, "kept re-reading the same file", false)
	if strings.Contains(rest, "kept re-reading the same file") {
		t.Error("non-first units must NOT repeat the block reason")
	}
	// Falls back to the title when a unit carries no explicit task text.
	if got := stuckUnitPrompt(planStep{Title: "just a title"}, "", false); !strings.Contains(got, "just a title") {
		t.Errorf("empty-task unit should fall back to the title: %q", got)
	}
}

// resetRepeat clears the blocked-repeat counter so stuck() no longer reports "repeat"
// (resetStall alone leaves blocked set and would re-halt immediately).
func TestResetRepeatClearsBlocked(t *testing.T) {
	g := newRunGuard()
	args := []byte(`{"x":1}`)
	for g.blocked < blockedBudget {
		g.check("bash", args)
	}
	if g.stuck() != "repeat" {
		t.Fatalf("precondition: expected stuck()==repeat, got %q", g.stuck())
	}
	// resetStall does NOT clear blocked → still stuck as repeat.
	g.resetStall()
	if g.stuck() != "repeat" {
		t.Fatal("resetStall must leave the blocked counter intact (still repeat)")
	}
	// resetRepeat clears it → no longer stuck.
	g.resetRepeat()
	if k := g.stuck(); k != "" {
		t.Errorf("resetRepeat must clear the repeat stall, got %q", k)
	}
}

// stuckDriverApp builds an App whose fake provider answers the planner call (identified by the
// decompose anchor) with a fixed plan JSON and every spawned child with fail(req) — "" fails that
// unit, non-empty lands it.
func stuckDriverApp(t *testing.T, plan string, fail func(req string) string) (*App, session.Session) {
	t.Helper()
	llm := &recLLM{reply: func(req string) string {
		if strings.Contains(req, "decompose THIS exact task") {
			return plan
		}
		return fail(req)
	}}
	a := newOrchApp(t, llm, Config{
		Permission: "allow", MaxAgents: 100, MaxDepth: 5, MaxPlanDepth: 2, MaxSteps: 120,
		Agents: map[string]AgentSpec{
			plannerAgent: {Name: "planner", System: "plan"},
			"coder":      {Name: "coder", System: "code", Tools: []string{"read", "write", "edit", "bash"}},
		},
	})
	s := parentSession(t.TempDir())
	a.mu.Lock()
	a.stateLocked(s.ID).meta = s
	a.mu.Unlock()
	return a, s
}

// driveStuckTodos decomposes the stuck task into TODOs and drives each unit in its own
// full-context child; every landed unit checks off its todo and the driver returns true.
func TestDriveStuckTodosDrivesUnits(t *testing.T) {
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	a, s := stuckDriverApp(t, plan, func(req string) string { return "did the unit" })

	ok := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "big stuck task", "kept re-reading", 0)
	if !ok {
		t.Fatal("driving 2 landed units must return true")
	}
	td := a.Todos(s.ID)
	if len(td) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(td))
	}
	if td[0].Status != "completed" || td[1].Status != "completed" {
		t.Errorf("both landed units should be completed, got %q / %q", td[0].Status, td[1].Status)
	}
}

// A unit that fails is reverted to pending and the driver keeps going; a later landed unit
// still returns true. The failed unit is NOT silently marked completed (no back-fill).
func TestDriveStuckTodosMidUnitFailureContinues(t *testing.T) {
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"},
		{"title":"unit C","strategy":"solo","task":"do C"}]}`
	// Unit B fails (empty), A and C land.
	a, s := stuckDriverApp(t, plan, func(req string) string {
		if strings.Contains(req, "do B") {
			return ""
		}
		return "landed"
	})

	ok := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "big stuck task", "reason", 0)
	if !ok {
		t.Fatal("at least one landed unit must return true even though a middle unit failed")
	}
	td := a.Todos(s.ID)
	if td[0].Status != "completed" || td[2].Status != "completed" {
		t.Errorf("units A and C should be completed, got %q / %q", td[0].Status, td[2].Status)
	}
	if td[1].Status != "pending" {
		t.Errorf("failed unit B must be reverted to pending (not back-filled to completed), got %q", td[1].Status)
	}
}

// A plan the planner could not split into >=2 units yields false, so the caller falls back
// to the single whole-task re-spawn (decomposing into one unit is just the monolith again).
func TestDriveStuckTodosSingleUnitFallsBack(t *testing.T) {
	plan := `{"steps":[{"title":"only one","strategy":"solo","task":"do it all"}]}`
	a, s := stuckDriverApp(t, plan, func(req string) string { return "landed" })
	if a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "task", "reason", 0) {
		t.Error("a single-unit decomposition must return false (caller falls back to whole-task re-spawn)")
	}
}
