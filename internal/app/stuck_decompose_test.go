package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// stuckDecomposeEnabled defaults ON (2026-07-21): the decomposing recovery rescues the
// search/read-loop fixations (fix-ocaml-gc, heap-crash) the whole-task re-hand-off cannot.
// The 2026-07-16 mid-build regression that forced it off is now closed at the source
// (isPollTool counts bash_output/wait_for polls as an environment wait), so only an explicit
// off-value restores the whole-task baseline.
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
	for _, v := range []string{"1", "on", "true", "yes", "whatever", ""} {
		t.Setenv("MAGI_STUCK_DECOMPOSE", v)
		if !stuckDecomposeEnabled() {
			t.Errorf("%q must leave it ON", v)
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

// When an OUTER delegated plan is in progress — a step has spawned a child session whose sub-work
// renders in the tree — recovery units are APPENDED below the existing todos, never clobbering the
// outer plan's visible progress.
func TestDriveStuckTodosPreservesOuterTodos(t *testing.T) {
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	a, s := stuckDriverApp(t, plan, func(req string) string { return "landed" })
	a.SetTodos(s.ID, []session.Todo{
		{Content: "outer step 1", Status: "completed"},
		{Content: "outer step 2", Status: "in_progress"},
	})
	// A real delegated child under outer step 2 that LANDED progress (a completed sub-todo) makes this
	// an outer-plan case worth preserving (not solo) → append below it.
	step := 1
	a.mu.Lock()
	a.states["outer_child"] = &sessionState{meta: session.Session{ID: "outer_child", Parent: s.ID, ParentStep: &step}}
	a.mu.Unlock()
	a.SetTodos("outer_child", []session.Todo{{Content: "sub-step it finished", Status: "completed"}})

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

// A step whose delegated worker DIED without landing anything (its child has only pending todos,
// no completed) is NOT a live outer plan to preserve: recovery drops the dead worker's stale
// sub-steps and REPLACES the list with the fresh plan, instead of stacking the new steps under the
// dead ones.
func TestDriveStuckTodosDeadChildReplaces(t *testing.T) {
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	a, s := stuckDriverApp(t, plan, func(req string) string { return "landed" })
	a.SetTodos(s.ID, []session.Todo{
		{Content: "orig step 1", Status: "completed"},
		{Content: "orig step 2", Status: "in_progress"},
	})
	// A child under step 2 whose worker died: it has ONLY a pending todo — no landed (completed) work.
	step := 1
	a.mu.Lock()
	a.states["dead_child"] = &sessionState{meta: session.Session{ID: "dead_child", Parent: s.ID, ParentStep: &step}}
	a.mu.Unlock()
	a.SetTodos("dead_child", []session.Todo{{Content: "sub-step it never finished", Status: "pending"}})

	landed, _ := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "task", "reason", 0)
	if !landed {
		t.Fatal("units should land")
	}
	td := a.Todos(s.ID)
	if len(td) != 2 {
		t.Fatalf("a dead worker's step must be REPLACED (not appended under): expected just the 2 units, got %d", len(td))
	}
	if td[0].Content != "unit A" || td[1].Content != "unit B" {
		t.Errorf("replaced list must be the recovery units, got %q / %q", td[0].Content, td[1].Content)
	}
}

// On the SOLO path — no step has spawned a child session, so the existing todos are just this same
// whole task's own, now-superseded plan the main agent ran inline — recovery REPLACES the list
// wholesale so the panel shows one plan, not the original stacked above a duplicate decomposition.
func TestDriveStuckTodosSoloReplacesTodos(t *testing.T) {
	plan := `{"steps":[
		{"title":"unit A","strategy":"solo","task":"do A"},
		{"title":"unit B","strategy":"solo","task":"do B"}]}`
	a, s := stuckDriverApp(t, plan, func(req string) string { return "landed" })
	// The same whole task's original plan, partially done — but NO delegated child under any step.
	a.SetTodos(s.ID, []session.Todo{
		{Content: "read the code", Status: "completed"},
		{Content: "fix and verify", Status: "in_progress"},
	})

	landed, _ := a.driveStuckTodos(context.Background(), s, a.cfg.Agents["coder"], "task", "reason", 0)
	if !landed {
		t.Fatal("units should land")
	}
	td := a.Todos(s.ID)
	if len(td) != 2 {
		t.Fatalf("solo recovery must REPLACE (not append): expected just the 2 units, got %d", len(td))
	}
	if td[0].Content != "unit A" || td[1].Content != "unit B" {
		t.Errorf("replaced list must be the recovery units, got %q / %q", td[0].Content, td[1].Content)
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

// The diverge clause reaches the planner's system prompt only when MAGI_DIVERGE is on:
// under uncertainty the plan opens with competing hypotheses and cheap kill-probes
// instead of committing everything to the first idea (the local-refinement lock).
func TestDivergeClauseGated(t *testing.T) {
	if !divergeEnabled() {
		t.Fatal("default must be ON")
	}
	t.Setenv("MAGI_DIVERGE", "0")
	if divergeEnabled() {
		t.Fatal("=0 must disable")
	}
	if !strings.Contains(divergeClause, "DISTINCT candidate explanations") ||
		!strings.Contains(divergeClause, "CONFIRM or KILL") {
		t.Errorf("clause must demand competing hypotheses with kill-probes: %q", divergeClause)
	}
}
