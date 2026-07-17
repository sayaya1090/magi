package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// stuckDecomposeEnabled defaults OFF (2026-07-16 regression bisect: recovery fired
// mid-build on a repeat block and burned the wall clock); an explicit on-value
// re-enables the decomposing recovery.
func TestStuckDecomposeEnabledDefault(t *testing.T) {
	if stuckDecomposeEnabled() {
		t.Fatal("default must be OFF")
	}
	for _, v := range []string{"1", "on", "true", "yes", "ON"} {
		t.Setenv("MAGI_STUCK_DECOMPOSE", v)
		if !stuckDecomposeEnabled() {
			t.Errorf("%q must enable", v)
		}
	}
	for _, v := range []string{"0", "off", "false", "no", "whatever"} {
		t.Setenv("MAGI_STUCK_DECOMPOSE", v)
		if stuckDecomposeEnabled() {
			t.Errorf("%q must leave it OFF", v)
		}
	}
}

// stuckUnitPrompt scopes a child to ONE unit, tells it (context already cloned) not to
// re-read, and carries the block reason on every unit — any unit may be the one that
// touches the fixation point.
func TestStuckUnitPrompt(t *testing.T) {
	st := planStep{Title: "edit init.lua", Task: "replace the hardcoded window with the API value"}
	p := stuckUnitPrompt(st, "kept re-reading the same file")
	if !strings.Contains(p, "replace the hardcoded window") {
		t.Error("must contain the unit task")
	}
	if !strings.Contains(p, "do NOT re-read") {
		t.Error("must tell the context-cloned child not to re-read")
	}
	if !strings.Contains(p, "kept re-reading the same file") {
		t.Error("every unit must carry the block reason")
	}
	// Falls back to the title when a unit carries no explicit task text.
	if got := stuckUnitPrompt(planStep{Title: "just a title"}, ""); !strings.Contains(got, "just a title") {
		t.Errorf("empty-task unit should fall back to the title: %q", got)
	}
}

// stuckUnitBudget hands a unit a quarter of the whole-task budget with a floor of 8.
func TestStuckUnitBudget(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{120, 30}, {240, 60}, {8, 8}, {0, 8}, {40, 10}} {
		if got := stuckUnitBudget(tc.in); got != tc.want {
			t.Errorf("stuckUnitBudget(%d) = %d, want %d", tc.in, got, tc.want)
		}
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
// full-context child; every landed unit checks off its todo and the driver returns landed.
func TestDriveStuckTodosDrivesUnits(t *testing.T) {
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	a, s := stuckDriverApp(t, plan, func(req string) string { return "did the unit" })

	landed, attempted := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "big stuck task", "kept re-reading", 0)
	if !landed || !attempted {
		t.Fatalf("driving 2 landed units must return (true, true), got (%v, %v)", landed, attempted)
	}
	td := a.Todos(s.ID)
	if len(td) != 2 {
		t.Fatalf("expected 2 todos, got %d", len(td))
	}
	if td[0].Status != "completed" || td[1].Status != "completed" {
		t.Errorf("both landed units should be completed, got %q / %q", td[0].Status, td[1].Status)
	}
}

// A landed unit's session is reused for the next unit (refine chaining): unit B's request
// must contain unit A's PROMPT text — the unit prompt lives only in the child session, so
// seeing it proves B continued A's session rather than starting from a fresh parent clone.
func TestDriveStuckTodosChainsLandedSessions(t *testing.T) {
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	var unitB string
	a, s := stuckDriverApp(t, plan, func(req string) string {
		if strings.Contains(req, "do B") {
			unitB = req
		}
		return "landed"
	})
	landed, _ := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "task", "reason", 0)
	if !landed {
		t.Fatal("both units should land")
	}
	// Chained, unit B's request holds TWO unit prompts (A's and B's); a fresh parent clone
	// would hold only B's — the unit prompt never lands in the parent conversation.
	if strings.Count(unitB, "carry out ONLY THIS ONE unit") < 2 {
		t.Error("unit B must run in unit A's session (its request should contain unit A's unit prompt)")
	}
}

// Recovery units are APPENDED below an existing plan's todos, never clobbering them: the
// stuck task is often one step of an outer plan whose progress must stay visible.
func TestDriveStuckTodosPreservesOuterTodos(t *testing.T) {
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	a, s := stuckDriverApp(t, plan, func(req string) string { return "landed" })
	a.SetTodos(s.ID, []session.Todo{
		{Content: "outer step 1", Status: "completed"},
		{Content: "outer step 2", Status: "in_progress"},
	})

	landed, _ := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "task", "reason", 0)
	if !landed {
		t.Fatal("units should land")
	}
	td := a.Todos(s.ID)
	if len(td) != 4 {
		t.Fatalf("expected outer 2 + units 2 = 4 todos, got %d", len(td))
	}
	if td[0].Content != "outer step 1" || td[0].Status != "completed" ||
		td[1].Content != "outer step 2" || td[1].Status != "in_progress" {
		t.Errorf("outer todos must be preserved untouched, got %+v", td[:2])
	}
	if td[2].Status != "completed" || td[3].Status != "completed" {
		t.Errorf("appended units should be completed, got %q / %q", td[2].Status, td[3].Status)
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

	landed, attempted := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "big stuck task", "reason", 0)
	if !landed || !attempted {
		t.Fatalf("at least one landed unit must return landed even though a middle unit failed, got (%v, %v)", landed, attempted)
	}
	td := a.Todos(s.ID)
	if td[0].Status != "completed" || td[2].Status != "completed" {
		t.Errorf("units A and C should be completed, got %q / %q", td[0].Status, td[2].Status)
	}
	if td[1].Status != "pending" {
		t.Errorf("failed unit B must be reverted to pending (not back-filled to completed), got %q", td[1].Status)
	}
}

// A plan the planner could not split into >=2 units yields (false, false), so the caller
// falls back to the single whole-task re-spawn (one unit is just the monolith again).
func TestDriveStuckTodosSingleUnitFallsBack(t *testing.T) {
	plan := `{"steps":[{"title":"only one","strategy":"solo","task":"do it all"}]}`
	a, s := stuckDriverApp(t, plan, func(req string) string { return "landed" })
	landed, attempted := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "task", "reason", 0)
	if landed || attempted {
		t.Errorf("a single-unit decomposition must return (false, false), got (%v, %v)", landed, attempted)
	}
}

// When the decomposition RAN and every unit failed, redecomposeStuck must NOT burn one more
// child on the whole-task monolith re-spawn — recovery reports failure and the caller
// force-stops. The monolith fallback still fires when decomposition wasn't possible.
func TestRedecomposeStuckAllUnitsFailedSkipsMonolith(t *testing.T) {
	t.Setenv("MAGI_STUCK_DECOMPOSE", "1") // exercises the decomposing path (default now OFF)
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	monolith := false
	a, s := stuckDriverApp(t, plan, func(req string) string {
		if strings.Contains(req, "do A") || strings.Contains(req, "do B") {
			return "" // every scoped unit fails
		}
		monolith = true // any non-unit child reaching the model = the fallback re-spawn
		return "landed"
	})
	if a.redecomposeStuck(context.Background(), s, a.cfg.Agents["coder"], "big stuck task", "reason", 0) {
		t.Error("recovery must report failure when every unit failed")
	}
	if monolith {
		t.Error("whole-task fallback must be skipped after an attempted decomposition failed every unit")
	}
}

// A NOVEL inspect-only command after a stalled nudge counts as responding to the
// redirect (the agent demonstrably changed direction), so the D18a convergence must
// not collapse the nudge budget; a REPEATED inspection or the off-flag keeps the
// exercising-only baseline. Deliverable-exercise accounting is untouched either way.
func TestStallNoveltyCountsNovelInspection(t *testing.T) {
	g := newRunGuard()
	g.progressSinceNudge = false
	// cat/ls are the true inspect-only class (grep/find are already exercising-class
	// and get novelty credit on the other branch).
	g.noteBashExec(`cat runtime/shared_heap.c`, true) // novel inspection
	if !g.progressSinceNudge {
		t.Fatal("a novel inspection must count as responding to the nudge")
	}
	if g.execSinceMut != 0 {
		t.Fatal("inspection must not count as exercising the deliverable")
	}

	g2 := newRunGuard()
	g2.noteBashExec(`cat runtime/shared_heap.c`, false) // seen before
	if g2.progressSinceNudge {
		t.Fatal("a repeated inspection is head-banging, not a response")
	}

	t.Setenv("MAGI_STALL_NOVELTY", "0")
	g3 := newRunGuard()
	g3.noteBashExec(`ls -la runtime`, true)
	if g3.progressSinceNudge {
		t.Fatal("off flag must restore the exercising-only baseline")
	}
}
