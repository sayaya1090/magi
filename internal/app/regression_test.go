package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// TestVolatileContextHoldsPlan: the per-step plan goes into volatileContext (the ephemeral
// trailing message), NOT the system prompt — so the system prompt stays cache-stable.
func TestVolatileContextHoldsPlan(t *testing.T) {
	a := &App{todos: map[session.SessionID][]session.Todo{
		"s1": {{Content: "implement X", Status: "in_progress"}},
	}}
	s := session.Session{ID: "s1"}
	out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 0, 0)
	if !strings.Contains(out, "# Current plan (TODOs)") || !strings.Contains(out, "implement X") {
		t.Fatalf("volatileContext should carry the plan, got %q", out)
	}
}

// TestVolatileContextStepBudget: with a real budget (maxSteps>=8) the ephemeral context
// carries the current step / ceiling and frames the ceiling as a limit, not a quota — so
// the model paces itself without padding to the max.
func TestVolatileContextStepBudget(t *testing.T) {
	a := &App{todos: map[session.SessionID][]session.Todo{}}
	s := session.Session{ID: "s1"}
	out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 6, 40)
	if !strings.Contains(out, "# Step budget") || !strings.Contains(out, "step 7 of at most 40") {
		t.Fatalf("budget block should show the current step and ceiling, got %q", out)
	}
	if !strings.Contains(out, "hard ceiling") || !strings.Contains(out, "not a target") {
		t.Fatalf("budget block should frame the ceiling as a limit, not a quota, got %q", out)
	}
	// Tiny phase budgets (e.g. summarize=3) skip the block entirely.
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 0, 3); strings.Contains(out, "# Step budget") {
		t.Fatalf("tiny budgets should not emit a step-budget block, got %q", out)
	}
}

// TestVolatileContextEmpty: no todos / experience / RAG → empty (nothing to inject, so no
// trailing message is added and the prefix is maximally cacheable).
func TestVolatileContextEmpty(t *testing.T) {
	a := &App{todos: map[session.SessionID][]session.Todo{}}
	s := session.Session{ID: "s1"}
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 0, 0); out != "" {
		t.Fatalf("expected empty volatile context, got %q", out)
	}
}

// TestSystemForStableAgentOrder: the "Available agents" block must render in a stable
// (sorted) order so the system prompt is byte-identical across steps — otherwise Go's
// randomized map iteration would mutate it every step and defeat the backend prefix cache.
func TestSystemForStableAgentOrder(t *testing.T) {
	a := &App{cfg: Config{Agents: map[string]AgentSpec{
		"zeta": {System: "z"}, "alpha": {System: "a"}, "mid": {System: "m"},
	}}}
	dir := t.TempDir()
	first := a.systemFor(AgentSpec{System: "base"}, dir, false)
	for i := 0; i < 30; i++ { // many iterations to surface map-order randomization
		if got := a.systemFor(AgentSpec{System: "base"}, dir, false); got != first {
			t.Fatalf("systemFor not stable across calls:\n--- first ---\n%s\n--- got ---\n%s", first, got)
		}
	}
	ai, mi, zi := strings.Index(first, "\n- alpha:"), strings.Index(first, "\n- mid:"), strings.Index(first, "\n- zeta:")
	if !(ai >= 0 && ai < mi && mi < zi) {
		t.Fatalf("agents not in sorted order (alpha<mid<zeta): %d %d %d", ai, mi, zi)
	}
}

// TestNoteEditRevertToBaseline: editing a file and then restoring its pre-turn content
// is a self-revert and must be flagged.
func TestNoteEditRevertToBaseline(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	// First edit: baseline "orig" → "fixed". The `before` seeds the baseline.
	if w := g.noteEdit(path, "orig", "fixed"); w != "" {
		t.Fatalf("first edit should not warn, got %q", w)
	}
	// Second edit: back to "orig" — undoing the fix.
	w := g.noteEdit(path, "fixed", "orig")
	if w == "" {
		t.Fatal("reverting to the pre-turn baseline should warn")
	}
}

// TestNoteEditOscillation: returning to an earlier (non-baseline) state also flags.
func TestNoteEditOscillation(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	g.noteEdit(path, "orig", "v1") // baseline orig, now v1
	g.noteEdit(path, "v1", "v2")   // now v2
	if w := g.noteEdit(path, "v2", "v1"); w == "" {
		t.Fatal("returning to an earlier edit state should warn")
	}
}

// TestNoteEditForwardProgress: distinct new states never warn.
func TestNoteEditForwardProgress(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	for i, after := range []string{"v1", "v2", "v3", "v4"} {
		before := "orig"
		if i > 0 {
			before = "vX" // ignored after first call (path already tracked)
		}
		if w := g.noteEdit(path, before, after); w != "" {
			t.Fatalf("forward edit %q should not warn, got %q", after, w)
		}
	}
}

// TestNoteEditIdempotent: writing identical content (no change since last state) is the
// loop guard's domain, not a regression — it must not warn.
func TestNoteEditIdempotent(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	g.noteEdit(path, "orig", "fixed")
	if w := g.noteEdit(path, "fixed", "fixed"); w != "" {
		t.Fatalf("idempotent rewrite should not warn, got %q", w)
	}
}

// TestNoteEditWarnsOncePerFile: an oscillating agent is warned at most once per file
// per turn, so a repeated nudge can't itself drive more thrashing.
func TestNoteEditWarnsOncePerFile(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	g.noteEdit(path, "orig", "fixed")
	if w := g.noteEdit(path, "fixed", "orig"); w == "" {
		t.Fatal("first revert should warn")
	}
	if w := g.noteEdit(path, "orig", "fixed"); w != "" {
		t.Fatalf("second oscillation should NOT warn again, got %q", w)
	}
	if w := g.noteEdit(path, "fixed", "orig"); w != "" {
		t.Fatalf("third oscillation should NOT warn again, got %q", w)
	}
}

// TestShouldNudge: the corrective nudge fires once, only at/after the threshold and below
// the force-stop budget, and never again.
func TestShouldNudge(t *testing.T) {
	g := newRunGuard()
	if g.shouldNudge() != "" {
		t.Fatal("should not nudge with zero blocked repeats")
	}
	g.blocked = nudgeThreshold - 1
	if g.shouldNudge() != "" {
		t.Fatal("should not nudge below threshold")
	}
	g.blocked = nudgeThreshold
	if g.shouldNudge() != "blocked" {
		t.Fatal("should nudge (blocked) at threshold")
	}
	g.blocked = blockedBudget // still past threshold
	if g.shouldNudge() != "" {
		t.Fatal("nudge must fire at most once per run")
	}
}

// TestShouldNudgeStalled: the no-progress nudge fires when varied calls make no real
// progress (sinceProgress past noProgressNudge) even though nothing is a blocked repeat.
// Unlike the blocked nudge it RE-ARMS — it fires again after each further noProgressNudge
// window with no mutation — but only every window (not every step) and only up to
// maxStallNudges, then goes quiet.
func TestShouldNudgeStalled(t *testing.T) {
	g := newRunGuard()
	g.sinceProgress = noProgressNudge - 1
	if g.shouldNudge() != "" {
		t.Fatal("should not nudge below the no-progress threshold")
	}
	g.sinceProgress = noProgressNudge
	if g.shouldNudge() != "stalled" {
		t.Fatal("should nudge (stalled) at the no-progress threshold")
	}
	// One more call past the last nudge, but not yet a full window later → quiet.
	g.sinceProgress = noProgressNudge + 1
	if g.shouldNudge() != "" {
		t.Fatal("should not re-nudge until a full noProgressNudge window later")
	}
	// A full further window with still no mutation → re-arm and fire again.
	fires := 1
	for i := 2; i <= maxStallNudges; i++ {
		g.sinceProgress = noProgressNudge * i
		if g.shouldNudge() != "stalled" {
			t.Fatalf("should re-nudge (stalled) at window %d", i)
		}
		fires++
	}
	if fires != maxStallNudges {
		t.Fatalf("expected %d stalled nudges, got %d", maxStallNudges, fires)
	}
	// Past the cap: no more, however long the stall runs.
	g.sinceProgress = noProgressNudge * (maxStallNudges + 5)
	if g.shouldNudge() != "" {
		t.Fatal("stalled nudge must stop after maxStallNudges")
	}
}

// TestShouldNudgeStalledReArmsAfterMutation: a real mutation resets both the count and the
// stall window, so a later stall gets a fresh nudge (and the per-run cap is not consumed by
// windows that were separated by genuine progress within the same firing).
func TestShouldNudgeStalledReArmsAfterMutation(t *testing.T) {
	g := newRunGuard()
	g.sinceProgress = noProgressNudge
	if g.shouldNudge() != "stalled" {
		t.Fatal("should nudge (stalled) at the threshold")
	}
	g.mutated("out.txt", "sig1") // real progress → sinceProgress and lastStallAt reset
	if g.lastStallAt != 0 {
		t.Fatalf("mutation should reset the stall window, got lastStallAt=%d", g.lastStallAt)
	}
	g.sinceProgress = noProgressNudge // a fresh stall after progress
	if g.shouldNudge() != "stalled" {
		t.Fatal("a stall after a real mutation should nudge again")
	}
}

// TestSinceProgressResetOnMutation: a real file mutation restarts the no-progress count,
// so re-running a command after a genuine edit is not counted as a stall.
func TestSinceProgressResetOnMutation(t *testing.T) {
	g := newRunGuard()
	for i := 0; i < noProgressNudge; i++ {
		g.check("bash", json.RawMessage(`{"command":"echo `+strconv.Itoa(i)+`"}`))
	}
	g.mutated("out.txt", "sig1")
	if g.sinceProgress != 0 {
		t.Fatalf("mutation should reset sinceProgress, got %d", g.sinceProgress)
	}
	if g.shouldNudge() != "" {
		t.Fatal("should not nudge right after a real mutation reset the count")
	}
}

// TestNoteEditPerFile: histories are independent per path.
func TestNoteEditPerFile(t *testing.T) {
	g := newRunGuard()
	g.noteEdit("a.go", "origA", "fixA")
	g.noteEdit("b.go", "origB", "fixB")
	if w := g.noteEdit("b.go", "fixB", "origB"); w == "" {
		t.Fatal("b.go revert should warn independently of a.go")
	}
	if w := g.noteEdit("a.go", "fixA", "fixA2"); w != "" {
		t.Fatalf("a.go forward edit should not warn, got %q", w)
	}
}
