package app

import (
	"context"
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
	out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil)
	if !strings.Contains(out, "# Current plan (TODOs)") || !strings.Contains(out, "implement X") {
		t.Fatalf("volatileContext should carry the plan, got %q", out)
	}
}

// TestVolatileContextEmpty: no todos / experience / RAG → empty (nothing to inject, so no
// trailing message is added and the prefix is maximally cacheable).
func TestVolatileContextEmpty(t *testing.T) {
	a := &App{todos: map[session.SessionID][]session.Todo{}}
	s := session.Session{ID: "s1"}
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil); out != "" {
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
	if g.shouldNudge() {
		t.Fatal("should not nudge with zero blocked repeats")
	}
	g.blocked = nudgeThreshold - 1
	if g.shouldNudge() {
		t.Fatal("should not nudge below threshold")
	}
	g.blocked = nudgeThreshold
	if !g.shouldNudge() {
		t.Fatal("should nudge at threshold")
	}
	g.blocked = blockedBudget // still past threshold
	if g.shouldNudge() {
		t.Fatal("nudge must fire at most once per run")
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
