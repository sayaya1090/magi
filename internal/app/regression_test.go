package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
)

// TestVolatileContextHoldsPlan: the per-step plan goes into volatileContext (the ephemeral
// trailing message), NOT the system prompt — so the system prompt stays cache-stable.
func TestVolatileContextHoldsPlan(t *testing.T) {
	a := &App{todos: map[session.SessionID][]session.Todo{
		"s1": {{Content: "implement X", Status: "in_progress"}},
	}}
	s := session.Session{ID: "s1"}
	out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 0, 0, 0)
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
	out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 6, 40, 0)
	if !strings.Contains(out, "# Step budget") || !strings.Contains(out, "step 7 of at most 40") {
		t.Fatalf("budget block should show the current step and ceiling, got %q", out)
	}
	if !strings.Contains(out, "hard ceiling") || !strings.Contains(out, "not a target") {
		t.Fatalf("budget block should frame the ceiling as a limit, not a quota, got %q", out)
	}
	// Narration is free, steps are not: the block must forbid spending a step on a
	// status-echo (measured waste: prove-plus-comm burned its last steps on them).
	if !strings.Contains(out, "only narrates") {
		t.Fatalf("budget block should forbid narration-only tool calls, got %q", out)
	}
	// Tiny phase budgets (e.g. summarize=3) skip the block entirely.
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 0, 3, 0); strings.Contains(out, "# Step budget") {
		t.Fatalf("tiny budgets should not emit a step-budget block, got %q", out)
	}
}

// TestVolatileContextElapsed: the self-measured wall clock appears only once it crosses a
// minute (sub-minute is noise), and it is stated as our own stopwatch — no external info.
func TestVolatileContextElapsed(t *testing.T) {
	a := &App{todos: map[session.SessionID][]session.Todo{}}
	s := session.Session{ID: "s1"}
	// Under a minute: nothing.
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 6, 40, 30*time.Second); strings.Contains(out, "wall-clock") {
		t.Fatalf("sub-minute elapsed should not be shown, got %q", out)
	}
	// Over a minute: shown.
	out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 6, 40, 11*time.Minute)
	if !strings.Contains(out, "working for 11m") || !strings.Contains(out, "wall-clock") {
		t.Fatalf("elapsed line should report self-measured wall clock, got %q", out)
	}
}

// TestVolatileContextTimeBudget: --time-budget is off by default (no line); when set it is
// stated as user guidance, and once elapsed passes it the line flips to EXCEEDED.
func TestVolatileContextTimeBudget(t *testing.T) {
	s := session.Session{ID: "s1"}
	off := &App{todos: map[session.SessionID][]session.Todo{}}
	if out := off.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 6, 40, 5*time.Minute); strings.Contains(out, "asked for this to finish") {
		t.Fatalf("time budget off by default should emit no budget line, got %q", out)
	}
	on := &App{todos: map[session.SessionID][]session.Todo{}, cfg: Config{TimeBudget: 30 * time.Minute}}
	if out := on.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 6, 40, 10*time.Minute); !strings.Contains(out, "within 30m") || !strings.Contains(out, "remaining") {
		t.Fatalf("time budget should state remaining, got %q", out)
	}
	if out := on.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 6, 40, 40*time.Minute); !strings.Contains(out, "EXCEEDED") {
		t.Fatalf("elapsed past the budget should read EXCEEDED, got %q", out)
	}
}

// TestVolatileContextEmpty: no todos / experience / RAG → empty (nothing to inject, so no
// trailing message is added and the prefix is maximally cacheable).
func TestVolatileContextEmpty(t *testing.T) {
	a := &App{todos: map[session.SessionID][]session.Todo{}}
	s := session.Session{ID: "s1"}
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 0, 0, 0); out != "" {
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
	if w, _ := g.noteEdit(path, "orig", "fixed"); w != "" {
		t.Fatalf("first edit should not warn, got %q", w)
	}
	// Second edit: back to "orig" — undoing the fix.
	w, _ := g.noteEdit(path, "fixed", "orig")
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
	if w, _ := g.noteEdit(path, "v2", "v1"); w == "" {
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
		if w, _ := g.noteEdit(path, before, after); w != "" {
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
	if w, _ := g.noteEdit(path, "fixed", "fixed"); w != "" {
		t.Fatalf("idempotent rewrite should not warn, got %q", w)
	}
}

// TestNoteEditWarnsOncePerFile: an oscillating agent is warned at most once per file
// per turn, so a repeated nudge can't itself drive more thrashing.
func TestNoteEditWarnsOncePerFile(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	g.noteEdit(path, "orig", "fixed")
	if w, _ := g.noteEdit(path, "fixed", "orig"); w == "" {
		t.Fatal("first revert should warn")
	}
	if w, _ := g.noteEdit(path, "orig", "fixed"); w != "" {
		t.Fatalf("second oscillation should NOT warn again, got %q", w)
	}
	if w, _ := g.noteEdit(path, "fixed", "orig"); w != "" {
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

// TestStallForceStop: once every stall nudge is spent AND another full no-progress
// window passes with still no mutation, stuck() reports "stall" — the backstop that
// keeps an agent ignoring all redirection from wandering to the (now 240) ceiling.
// A real mutation at any point resets the window and disarms it.
func TestStallForceStop(t *testing.T) {
	g := newRunGuard()
	for i := 1; i <= maxStallNudges; i++ {
		g.sinceProgress = noProgressNudge * i
		if g.shouldNudge() != "stalled" {
			t.Fatalf("stall nudge %d should fire", i)
		}
		if g.stuck() != "" {
			t.Fatalf("must not force-stop while nudges are still being tried (after nudge %d)", i)
		}
	}
	// Nudges exhausted, but not yet a further full window → still not stopped.
	g.sinceProgress = noProgressNudge*maxStallNudges + 1
	if g.stuck() != "" {
		t.Fatal("force-stop needs a full ignored window after the last nudge")
	}
	g.sinceProgress = noProgressNudge * (maxStallNudges + 1)
	if g.stuck() != "stall" {
		t.Fatal("expected the stall force-stop after the post-nudge window elapsed")
	}
	// Real progress disarms it.
	g.mutated("out.txt", "sig")
	if g.stuck() != "" {
		t.Fatal("a real mutation must disarm the stall stop")
	}
}

// TestRegressiveEditWithholdsProgress: an implement↔revert oscillation must not keep resetting
// the no-progress counter. A forward edit is progress (resets sinceProgress); a revert to a state
// the file already held this turn is churn — mutated() resets, then noteEdit's regressed flag
// drives retractProgress() to restore the climb. Without this, the oscillation zeroes the counter
// on every swing and the stall force-stop never accumulates (the implement→revert timeout seen in
// self-verification #01, where council never even convened before the wall-clock killed the run).
func TestRegressiveEditWithholdsProgress(t *testing.T) {
	g := newRunGuard()
	const path = "calc.go"
	// edit replays one oscillation swing exactly as the loop body does: count the tool call, record
	// the mutation (which resets progress), then run the content check and retract on a self-revert.
	edit := func(before, after, sig string) {
		g.check("edit", json.RawMessage(`{}`)) // one tool call → sinceProgress++
		reset := g.mutated(path, sig)
		if _, regressed := g.noteEdit(path, before, after); regressed && reset {
			g.retractProgress()
		}
	}
	// A forward edit is genuine progress → the no-progress counter resets.
	g.sinceProgress = 9
	edit("orig", "stub", "sig-stub")
	if g.sinceProgress != 0 {
		t.Fatalf("a forward edit is progress and should reset the counter, got %d", g.sinceProgress)
	}
	// Reverting to the original is churn, not progress: the counter must climb, not reset to 0.
	before := g.sinceProgress
	edit("stub", "orig", "sig-orig")
	if g.sinceProgress <= before {
		t.Fatalf("a self-revert must not reset progress: sinceProgress %d ≤ %d", g.sinceProgress, before)
	}
	// And it keeps climbing monotonically across a long oscillation, well past a stall window, so
	// the force-stop (see TestStallForceStop) can finally accumulate instead of being reset forever.
	for i := 0; i < noProgressNudge*2; i++ {
		b, a, s := "stub", "orig", "sig-orig"
		if i%2 == 0 {
			b, a, s = "orig", "stub", "sig-stub"
		}
		prev := g.sinceProgress
		edit(b, a, s)
		if g.sinceProgress <= prev {
			t.Fatalf("swing %d: oscillation must keep climbing, got %d ≤ %d", i, g.sinceProgress, prev)
		}
	}
	if g.sinceProgress < noProgressNudge {
		t.Fatalf("after a long oscillation the counter should be past a stall window, got %d", g.sinceProgress)
	}
}

// TestBashWriteCountsAsProgress: a bash command that writes a file bumps the mutation
// epoch (progress), while re-running the identical write does not — the tool-agnostic
// twin of write/edit's epoch rule, so bash-heavy tasks don't misfire stall nudges.
func TestBashWriteCountsAsProgress(t *testing.T) {
	g := newRunGuard()
	g.sinceProgress = noProgressNudge - 1
	if !g.noteBashWrite("echo hi > out.txt") {
		t.Fatal("a redirect write should be recorded")
	}
	if g.sinceProgress != 0 {
		t.Fatalf("a bash write is progress: sinceProgress should reset, got %d", g.sinceProgress)
	}
	// The identical command again is NOT progress (idempotent rewrite loop).
	g.sinceProgress = 5
	g.noteBashWrite("echo hi > out.txt")
	if g.sinceProgress != 5 {
		t.Fatalf("an identical rewrite must not count as progress, got sinceProgress=%d", g.sinceProgress)
	}
	// A read-only command is neither recorded nor progress.
	if g.noteBashWrite("grep foo src/") {
		t.Fatal("read-only commands must not be recorded as writes")
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
	if w, _ := g.noteEdit("b.go", "fixB", "origB"); w == "" {
		t.Fatal("b.go revert should warn independently of a.go")
	}
	if w, _ := g.noteEdit("a.go", "fixA", "fixA2"); w != "" {
		t.Fatalf("a.go forward edit should not warn, got %q", w)
	}
}

// The planner's step estimate rides the budget block as pacing advice — and only
// as advice: no estimate, no extra sentence; the hard ceiling wording is unchanged.
func TestVolatileContextStepEstimate(t *testing.T) {
	a := &App{todos: map[session.SessionID][]session.Todo{}, estSteps: map[session.SessionID]int{}}
	s := session.Session{ID: "s1"}
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 0, 240, 0); strings.Contains(out, "estimated at roughly") {
		t.Fatalf("no estimate should add no advisory line, got %q", out)
	}
	a.storeStepEstimate("s1", 12)
	out := a.volatileContext(context.Background(), s, AgentSpec{}, false, nil, 0, 240, 0)
	if !strings.Contains(out, "estimated at roughly 12 step(s)") || !strings.Contains(out, "not a") {
		t.Fatalf("advisory estimate missing: %q", out)
	}
	if !strings.Contains(out, "hard ceiling") {
		t.Fatalf("the hard ceiling wording must survive: %q", out)
	}
	// Garbage estimates are refused at the store.
	a.storeStepEstimate("s1", -3)
	if a.stepEstimate("s1") != 12 {
		t.Fatal("negative estimate should not overwrite")
	}
	a.storeStepEstimate("s1", 999999)
	if a.stepEstimate("s1") != 12 {
		t.Fatal("absurd estimate should not overwrite")
	}
}
