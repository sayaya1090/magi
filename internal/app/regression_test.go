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
	a := &App{states: map[session.SessionID]*sessionState{
		"s1": {todos: []session.Todo{{Content: "implement X", Status: "in_progress"}}},
	}}
	s := session.Session{ID: "s1"}
	out := a.volatileContext(context.Background(), s, AgentSpec{}, nil, nil, 0, 0, 0)
	if !strings.Contains(out, "# Current plan (TODOs)") || !strings.Contains(out, "implement X") {
		t.Fatalf("volatileContext should carry the plan, got %q", out)
	}
}

// TestVolatileContextElapsed: the self-measured wall clock appears only once it crosses a
// minute (sub-minute is noise), and it is stated as our own stopwatch — no external info.
func TestVolatileContextElapsed(t *testing.T) {
	a := &App{}
	s := session.Session{ID: "s1"}
	// Under a minute: nothing.
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, nil, nil, 6, 40, 30*time.Second); strings.Contains(out, "wall-clock") {
		t.Fatalf("sub-minute elapsed should not be shown, got %q", out)
	}
	// Over a minute: shown.
	out := a.volatileContext(context.Background(), s, AgentSpec{}, nil, nil, 6, 40, 11*time.Minute)
	if !strings.Contains(out, "working for 11m") || !strings.Contains(out, "wall-clock") {
		t.Fatalf("elapsed line should report self-measured wall clock, got %q", out)
	}
}

// TestVolatileContextTimeBudget: --time-budget is off by default (no line); when set it is
// stated as user guidance, and once elapsed passes it the line flips to EXCEEDED.
func TestVolatileContextTimeBudget(t *testing.T) {
	s := session.Session{ID: "s1"}
	off := &App{}
	if out := off.volatileContext(context.Background(), s, AgentSpec{}, nil, nil, 6, 40, 5*time.Minute); strings.Contains(out, "asked for this to finish") {
		t.Fatalf("time budget off by default should emit no budget line, got %q", out)
	}
	on := &App{cfg: Config{TimeBudget: 30 * time.Minute}}
	if out := on.volatileContext(context.Background(), s, AgentSpec{}, nil, nil, 6, 40, 10*time.Minute); !strings.Contains(out, "within 30m") || !strings.Contains(out, "remaining") {
		t.Fatalf("time budget should state remaining, got %q", out)
	}
	if out := on.volatileContext(context.Background(), s, AgentSpec{}, nil, nil, 6, 40, 40*time.Minute); !strings.Contains(out, "EXCEEDED") {
		t.Fatalf("elapsed past the budget should read EXCEEDED, got %q", out)
	}
}

// TestVolatileContextEmpty: no todos / experience / RAG → empty (nothing to inject, so no
// trailing message is added and the prefix is maximally cacheable).
func TestVolatileContextEmpty(t *testing.T) {
	a := &App{}
	s := session.Session{ID: "s1"}
	if out := a.volatileContext(context.Background(), s, AgentSpec{}, nil, nil, 0, 0, 0); out != "" {
		t.Fatalf("expected empty volatile context, got %q", out)
	}
}

// TestSystemForIsByteStableAcrossSteps: the system prompt must be byte-identical from one step to
// the next, or the backend's prefix (KV) cache is defeated on every request. It used to be at risk
// from Go's randomized map iteration, which reordered the roster block rendered into it; that block
// is gone with delegation, and the property it protected is what this holds.
func TestSystemForIsByteStableAcrossSteps(t *testing.T) {
	a := &App{cfg: Config{Agents: map[string]AgentSpec{
		"zeta": {System: "z"}, "alpha": {System: "a"}, "mid": {System: "m"},
	}}}
	dir := t.TempDir()
	first := a.systemFor(AgentSpec{System: "base"}, dir)
	for i := 0; i < 30; i++ { // many iterations to surface map-order randomization
		if got := a.systemFor(AgentSpec{System: "base"}, dir); got != first {
			t.Fatalf("systemFor not stable across calls:\n--- first ---\n%s\n--- got ---\n%s", first, got)
		}
	}
	// A configured roster reaches no prompt: nothing can be delegated to, so naming agents would
	// advertise a capability the model does not have.
	for _, name := range []string{"alpha", "mid", "zeta"} {
		if strings.Contains(first, "\n- "+name+":") {
			t.Errorf("the system prompt still advertises agent %q:\n%s", name, first)
		}
	}
}

// Every agent (top-level and subagent) is told to emit markdown tables rather than
// hand-align columns: the markdown renderer aligns CJK/wide columns correctly, whereas
// space-padded ASCII tables misalign because padding counts runes, not display cells.
func TestSystemForCarriesOutputFormatGuide(t *testing.T) {
	a := newOrchApp(t, &recLLM{reply: func(string) string { return "" }}, Config{})
	dir := t.TempDir()
	got := a.systemFor(AgentSpec{System: "base"}, dir)
	if !strings.Contains(got, "markdown table") || !strings.Contains(got, "Do NOT hand-align") {
		t.Errorf("systemFor missing the output-formatting guide:\n%s", got)
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

// TestNoteEditReportsAndRegressesEverySwing: the regressed SIGNAL stays true on EVERY swing so the
// caller keeps withholding progress credit, and so does the human-facing report. The warning used to
// fire once per file, on the reasoning that a repeated nudge pushes a weak model to keep thrashing —
// but that was written while the guard could still stop the turn. It cannot, so a swing nobody
// mentions is one the agent never learns about (see TestEverySwingIsReportedNotJustTheFirst).
func TestNoteEditReportsAndRegressesEverySwing(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	if w, r := g.noteEdit(path, "orig", "A"); w != "" || r {
		t.Fatalf("forward edit: want (\"\", false), got (%q, %v)", w, r)
	}
	// First revert (back to the baseline): reports AND regresses.
	if w, r := g.noteEdit(path, "A", "orig"); w == "" || !r {
		t.Fatalf("first revert: want a warning + regressed=true, got (%q, %v)", w, r)
	}
	// Swing back to a state already seen: still a regression, and still reported — now with the count.
	w, r := g.noteEdit(path, "orig", "A")
	if !r || w == "" {
		t.Fatalf("second swing: want a report + regressed=true, got (%q, %v)", w, r)
	}
	if !strings.Contains(w, "2 times") {
		t.Errorf("a later swing must carry how many there have been, got %q", w)
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

// TestNoteEditIdempotent: writing identical content is not a REGRESSION — nothing moved either
// way. It is still reported: this deferred to "the loop guard's domain", and the loop guard could
// not reach it (its fingerprint carried a shared epoch that any other mutation reset), so 17 of one
// turn's 24 writes changed nothing and the agent was never told.
func TestNoteEditIdempotent(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	g.noteEdit(path, "orig", "fixed")
	w, regressed := g.noteEdit(path, "fixed", "fixed")
	if regressed {
		t.Fatal("an idempotent rewrite is not a regression")
	}
	if w == "" {
		t.Fatal("an idempotent rewrite must still be reported as changing nothing")
	}
}

// TestNoteEditEscalatesAcrossOscillation: the first swing reads as an aside a deliberate revert can
// ignore; each later one states the pattern and how big it has grown, because by then "I meant to do
// that" no longer explains it.
func TestNoteEditEscalatesAcrossOscillation(t *testing.T) {
	g := newRunGuard()
	const path = "a.go"
	g.noteEdit(path, "orig", "fixed")
	first, _ := g.noteEdit(path, "fixed", "orig")
	if !strings.Contains(first, "restored a content state") {
		t.Fatalf("first revert keeps the neutral aside, got %q", first)
	}
	second, _ := g.noteEdit(path, "orig", "fixed")
	if !strings.Contains(second, "2 times") {
		t.Fatalf("second oscillation must be reported with its count, got %q", second)
	}
	third, _ := g.noteEdit(path, "fixed", "orig")
	if !strings.Contains(third, "3 times") {
		t.Fatalf("third oscillation must keep counting, got %q", third)
	}
}

// TestShouldNudge: the corrective nudge for a BLOCKED repeat fires once, at/after the
// threshold, and never again. (Once was the right number when a force-stop was expected to
// land the run behind it; that force-stop is gone, and this kind has not been re-armed —
// see TestTheStalledNudgeKeepsSayingItWhileNothingChanges for the kind that was.)
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
	g.blocked = nudgeThreshold * 3 // still past threshold
	if g.shouldNudge() != "" {
		t.Fatal("nudge must fire at most once per run")
	}
}

// TestShouldNudgeStalled: the no-progress nudge fires when varied calls make no real
// progress (sinceProgress past noProgressNudge) even though nothing is a blocked repeat.
// Unlike the blocked nudge it RE-ARMS — it fires again after each further noProgressNudge
// window with no mutation — but only every window, not every step.
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
	// A full further window with still no mutation → re-arm and fire again, every window.
	for i := 2; i <= 10; i++ {
		g.sinceProgress = noProgressNudge * i
		if g.shouldNudge() != "stalled" {
			t.Fatalf("should re-nudge (stalled) at window %d", i)
		}
	}
}

// The stalled nudge re-arms for as long as nothing changes, with no budget.
//
// Two things used to stop it and both are gone. A "convergence" collapse exhausted the budget when
// a re-armed window produced no forward motion, on the reasoning that stuck() would land the
// honest stall sooner; stuck() is gone, so it handed off to nothing. Then the budget itself: three
// nudges, written when a force-stop was still expected to end the run. Measured live
// (large-scale-text-editing, 2026-08-01) after the collapse was removed but the cap remained — the
// third nudge landed on call 64 and the agent wrote byte-identical content 57 more times in
// silence.
//
// Speech is the only lever this project uses; rationing it bought nothing. Twenty windows here is
// not a new cap — it is more than any real run reaches, and it fails if a cap comes back.
func TestTheStalledNudgeKeepsSayingItWhileNothingChanges(t *testing.T) {
	g := newRunGuard()
	for i := 1; i <= 20; i++ {
		g.sinceProgress = noProgressNudge * i
		if got := g.shouldNudge(); got != "stalled" {
			t.Fatalf("window %d: got %q, want a nudge — going quiet hands off to nothing", i, got)
		}
	}
}

// A window with no motion still gets its nudge — that is the case the collapse used to swallow,
// and it is exactly the case a nudge is for.
func TestAWindowWithNoMotionStillGetsItsNudge(t *testing.T) {
	g := newRunGuard()
	g.sinceProgress = noProgressNudge
	if g.shouldNudge() != "stalled" {
		t.Fatal("the first nudge should fire")
	}
	g.sinceProgress = noProgressNudge * 2
	if g.progressSinceNudge {
		t.Fatal("precondition: nothing moved since the nudge")
	}
	if got := g.shouldNudge(); got != "stalled" {
		t.Errorf("a second ignored window must still be told, got %q", got)
	}
}

// A real mutation restarts the window, so it does not spend a nudge — an agent that edited a file
// in response to the redirect is not treated as stalled.
func TestAMutationRestartsTheStallWindow(t *testing.T) {
	g := newRunGuard()
	g.sinceProgress = noProgressNudge
	if g.shouldNudge() != "stalled" {
		t.Fatal("the first nudge should fire")
	}
	g.mutated("sol.py", "v2")
	if g.sinceProgress != 0 || g.lastStallAt != 0 {
		t.Fatalf("a mutation must restart the window, sinceProgress=%d lastStallAt=%d", g.sinceProgress, g.lastStallAt)
	}
	if got := g.shouldNudge(); got != "" {
		t.Errorf("right after a mutation there is nothing to nudge about, got %q", got)
	}
}

// TestNoteBashExecNovelty (D18a): a NOVEL (first-seen) non-inspect exercising command sets the
// progressSinceNudge motion flag; a repeat (novel=false) and any inspect-only command never set it.
func TestNoteBashExecNovelty(t *testing.T) {
	g := newRunGuard()
	g.noteBashExec("python x.py", true) // novel exercise → motion
	if !g.progressSinceNudge {
		t.Fatal("a novel exercise is motion")
	}
	// A repeat exercise (not novel) after clearing the flag must NOT re-set motion.
	g.progressSinceNudge = false
	g.noteBashExec("python x.py", false)
	if g.progressSinceNudge {
		t.Fatal("a repeat (non-novel) exercise must not set the motion flag")
	}
	// A NOVEL inspection now counts as responding to the redirect (MAGI_STALL_NOVELTY,
	// default ON) but is still not an exercise; a repeated inspection moves neither.
	g.noteBashExec("ls -la", true)
	if !g.progressSinceNudge {
		t.Fatal("a novel inspection answers the redirect and counts as motion")
	}
	g.progressSinceNudge = false
	g.noteBashExec("ls -la", false)
	if g.progressSinceNudge {
		t.Fatal("a repeated inspection is not motion")
	}
}

// TestResetStall: a structural recovery (redecomposeStuck handed the work to a fresh child)
// clears the no-progress/stall accounting so the parent gets a clean window to integrate and
// verify — otherwise the still-climbed sinceProgress would immediately re-trip the force-stop
// and abort the recovery. The mutation epoch and captured changeSet (the parent's own edits)
// must survive the reset.
func TestResetStall(t *testing.T) {
	g := newRunGuard()
	// Drive the stall accounting well into a stall: several nudges spent, window climbed.
	const windows = 3
	for i := 1; i <= windows; i++ {
		g.sinceProgress = noProgressNudge * i
		g.shouldNudge()
	}
	if g.stallNudges != windows || g.lastStallAt == 0 || g.sinceProgress == 0 {
		t.Fatalf("precondition: expected a stalled state, got nudges=%d lastAt=%d since=%d",
			g.stallNudges, g.lastStallAt, g.sinceProgress)
	}
	// Independent state that must be preserved across the reset.
	g.epoch = 5
	g.recordChange("out.txt", "before", "after")

	g.resetStall()

	if g.sinceProgress != 0 || g.lastStallAt != 0 || g.stallNudges != 0 {
		t.Errorf("resetStall must zero the stall accounting, got since=%d lastAt=%d nudges=%d",
			g.sinceProgress, g.lastStallAt, g.stallNudges)
	}
	if g.epoch != 5 {
		t.Errorf("resetStall must not touch the mutation epoch, got %d", g.epoch)
	}
	if cs := g.changeSet(); len(cs) != 1 || cs[0].path != "out.txt" {
		t.Errorf("resetStall must preserve the captured changeSet, got %+v", cs)
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
	// the stalled nudge can finally reach its threshold instead of being reset on every swing.
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

// TestNoteEditRegressedFlagAndIdempotent locks the `regressed` bool that the warning-string
// tests above (TestNoteEditRevertToBaseline and friends) leave alone, and the two edges they
// leave untested: a self-revert is regressed on EVERY swing (so the caller keeps withholding
// progress); an idempotent rewrite is neither; and the count is per-PATH, so a different file
// starts its own.
func TestNoteEditRegressedFlagAndIdempotent(t *testing.T) {
	g := newRunGuard()
	const path = "calc.go"

	// Forward edit orig→stub: new state, forward progress — no warning, not regressed.
	if w, reg := g.noteEdit(path, "orig", "stub"); w != "" || reg {
		t.Fatalf("forward edit: warn=%q regressed=%v; want \"\", false", w, reg)
	}
	// Revert stub→orig: back to a held state — regressed, and the FIRST warning fires.
	if w, reg := g.noteEdit(path, "stub", "orig"); w == "" || !reg {
		t.Fatalf("first self-revert: warn=%q regressed=%v; want non-empty, true", w, reg)
	}
	// Swing orig→stub again: still regressed (caller keeps withholding progress), and still
	// reported — the second report carries the count instead of repeating the first wording.
	if w, reg := g.noteEdit(path, "orig", "stub"); !reg || !strings.Contains(w, "2 times") {
		t.Fatalf("second swing: warn=%q regressed=%v; want the counted report, true", w, reg)
	}
	// Idempotent rewrite (after == current state): not a regression, but reported — it is the one
	// thing the agent cannot see from "wrote N bytes".
	if w, reg := g.noteEdit(path, "stub", "stub"); w == "" || reg {
		t.Fatalf("idempotent rewrite: warn=%q regressed=%v; want non-empty, false", w, reg)
	}
	// A DIFFERENT file starts its own count, so it gets the first-time wording (per-path).
	const other = "util.go"
	g.noteEdit(other, "A", "B")
	if w, reg := g.noteEdit(other, "B", "A"); w == "" || !reg {
		t.Fatalf("other file first revert: warn=%q regressed=%v; want non-empty, true", w, reg)
	}
}

// TestBashWriteCountsAsProgress: a bash command that writes a file bumps the mutation
// epoch (progress), while re-running the identical write does not — the tool-agnostic
// twin of write/edit's epoch rule, so bash-heavy tasks don't misfire stall nudges.
func TestBashWriteCountsAsProgress(t *testing.T) {
	g := newRunGuard()
	g.sinceProgress = noProgressNudge - 1
	if authored, _ := g.noteBashWrite("echo hi > out.txt"); !authored {
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
	if authored, _ := g.noteBashWrite("grep foo src/"); authored {
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
