package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// stepGateOutcome is the verdict of the deterministic per-step deliverable gate.
type stepGateOutcome int

const (
	// gateInactive: the gate did not decide — flag off, no checks stored, or no
	// platform to run them (tests). The caller proceeds to the normal council path.
	gateInactive stepGateOutcome = iota
	// gateFailRetry: at least one check failed and the one-shot failure nudge was
	// injected this call. The caller returns loopContinue to let the agent react.
	gateFailRetry
	// gatePass: every stored check passed. The executable contract the council froze
	// at plan time is satisfied by execution now, so the caller finishes VERIFIED
	// and skips the open-ended termination council (no new deliverable demands).
	gatePass
)

// stepCheckOutputCap bounds each failing check's output folded into the nudge, so a
// chatty command can't blow up the continuation prompt (mirrors councilSignalCap sizing).
const stepCheckOutputCap = 1200

// runStepGate runs the plan-audit council's per-step executable deliverable checks at
// the solo loop's finish boundary and reports what the caller should do. It is the
// deterministic half of the contract: the council decided WHAT to verify and HOW at
// plan time; here we simply execute those commands and believe the result.
//
//   - Every check passes → gatePass (checks off the matching todos, emits a decided fact).
//   - Some check fails, not yet nudged → inject the failing commands' output once and
//     return gateFailRetry (fire-once via ts.stepNudged, so a genuinely-stuck run falls
//     through to the council next round instead of looping forever).
//   - Some check fails, already nudged → gateInactive (hand off to the council).
//   - Flag off / no checks / no platform → gateInactive (baseline untouched).
//
// Passing checks always check off their todos, even on a mixed pass/fail round, so the
// panel reflects real progress.
func (a *App) runStepGate(ctx context.Context, s session.Session, ts *turnState) (stepGateOutcome, string) {
	if !stepVerifyEnabled() || a.plat == nil {
		return gateInactive, ""
	}
	checks := a.cachedChecks(s.ID)
	if len(checks) == 0 {
		return gateInactive, ""
	}

	// Run each check; group results by the plan step it belongs to so a step's todo is
	// checked off only when ALL of that step's deliverables pass (a step may have several).
	type result struct {
		check council.DeliverableCheck
		out   string
		pass  bool
	}
	results := make([]result, 0, len(checks))
	stepPass := map[string]bool{} // step key → all-passed-so-far
	stepSeen := map[string]bool{}
	anyFail := false
	for _, c := range checks {
		key := strings.TrimSpace(c.Step)
		// A check already recorded ✓ this run (per-step at completion, or an earlier gate pass) is
		// TRUSTED — do not re-run it. The terminal gate is a reconciliation of what is not yet
		// verified, not a batched re-verify of everything at the finish; re-running is exactly the
		// "run all the checks at once at the end" the per-step recording replaced. Counts as its
		// step's pass without another command (the completion ✓ is authoritative for the run).
		if a.checkAlreadyGreen(s.ID, c) {
			if !stepSeen[key] {
				stepSeen[key] = true
				stepPass[key] = true
			}
			continue
		}
		ok, code, out := a.runCheckRecord(ctx, s.ID, s.Workdir, c)
		if code == -1 { // platform vanished mid-run: can't verify → don't decide
			return gateInactive, ""
		}
		// The CHECK could not run — not found (127), or not executable / refused by the read-only
		// guard (126). Either way this says nothing about the deliverable, so don't count it as a
		// failure that reworks correct work — skip it; the agent/worker's equivalent-substitution
		// evidence and the council settle the goal instead of churning on a broken check.
		if checkUnrunnable(code) {
			continue
		}
		results = append(results, result{check: c, out: out, pass: ok})
		if !stepSeen[key] {
			stepSeen[key] = true
			stepPass[key] = true
		}
		if !ok {
			anyFail = true
			stepPass[key] = false
		}
	}

	// Check off every step whose deliverables all passed — the deterministic solo-step
	// completion signal that todowrite-driven panels lack (see todos.go).
	a.checkOffPassedSteps(ctx, s.ID, stepPass)

	// All-pass NO LONGER skips the termination council. Skipping was the reason this gate shipped
	// OFF: a weak plan-audit authors TRIVIAL checks ("file exists"), they all pass, the council is
	// skipped, and a false done lands. Instead the ledger is EVIDENCE the council always judges on —
	// real executed check results, not the agent's "I'm done" narration — so trivial passes still
	// face the council's completeness lens, and a genuine failing check is a hard signal it must honor.
	if !anyFail {
		return gateInactive, ""
	}

	// Build the failing-check ledger for the council (real command/expected/actual — not prose).
	var ledger strings.Builder
	ledger.WriteString("deliverable checks FAILED by execution (the agent's claim of completion is unverified):")
	for _, r := range results {
		if r.pass {
			continue
		}
		fmt.Fprintf(&ledger, "\n- step %q — %s: `%s`", r.check.Step, r.check.Deliverable, checkWhat(r.check))
		if r.check.Expect != "" {
			fmt.Fprintf(&ledger, " expected %q", r.check.Expect)
		}
		fmt.Fprintf(&ledger, " → actual: %s", clipLine(strings.TrimSpace(r.out), 200))
	}

	if ts.stepNudged {
		return gateInactive, ledger.String() // already nudged once → let the council judge on the ledger
	}
	// Inject the failing checks (command + expected + output tail) ONCE as a system continuation
	// so the agent knows exactly what to fix. Only fires on a REAL failure (no context pollution).
	var b strings.Builder
	b.WriteString("Deliverable check failed — the plan's expected outputs are not yet satisfied. Fix these, do not stop:\n")
	for _, r := range results {
		if r.pass {
			continue
		}
		fmt.Fprintf(&b, "\n• step %q — %s\n  checked: %s\n", r.check.Step, r.check.Deliverable, checkWhat(r.check))
		if r.check.Expect != "" {
			fmt.Fprintf(&b, "  expected output to match: %s\n", r.check.Expect)
		}
		fmt.Fprintf(&b, "  actual output:\n%s\n", clipLine(strings.TrimSpace(r.out), stepCheckOutputCap))
	}
	b.WriteString("\nIf a check genuinely CANNOT be satisfied, do not silently declare done — state plainly in " +
		"your final report which requirement is unmet and WHY (an honest blocked/failed status).")
	a.appendPromptText(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "council"}, b.String())
	ts.stepNudged = true
	return gateFailRetry, ledger.String()
}

// stepCheckRecordCap bounds the command output persisted with a check result. Enough to see what
// the command actually printed (the pair output+expect is what makes a verdict re-derivable), far
// short of a build log — the fact is written on every check of every gate cycle.
const stepCheckRecordCap = 600

// clipCheckOutput trims and bounds a check's captured output for persistence, keeping the HEAD:
// a check's verdict is decided by what the command printed, and the discriminating line is at the
// start for the short outputs checks are supposed to produce.
func clipCheckOutput(out string) string {
	return clipLine(strings.TrimSpace(out), stepCheckRecordCap)
}

// emitStepCheck records one check's deterministic result as its own reviewable fact, so the
// contract's execution is observable (parity with the plan-audit criteria artifact). It is a
// TypeStepCheck, NOT a council decision: a single check has no round or tally, and rendering it
// as a council-round outcome ("round 0: finished (no consensus) — 0 done / 0 continue") was
// misleading. The UI renders it as a clean ✓/✗ line from the structured fields.
func (a *App) emitStepCheck(ctx context.Context, sid session.SessionID, c council.DeliverableCheck, code int, pass bool, out string) {
	a.recordCheckResult(sid, c, pass)
	dd, _ := json.Marshal(event.StepCheckData{
		Step:        strings.TrimSpace(c.Step),
		Deliverable: strings.TrimSpace(c.Deliverable),
		Command:     strings.TrimSpace(c.Command),
		Code:        code,
		Pass:        pass,
		Output:      clipCheckOutput(out),
		Expect:      strings.TrimSpace(c.Expect),
		Source:      strings.TrimSpace(c.Source),
		Assert:      strings.TrimSpace(c.Assert),
	})
	a.appendFact(ctx, sid, event.TypeStepCheck, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
}

// checkIdent identifies WHAT a check verifies, independent of which step it hangs off: the command
// text for a shell check, and the assertion+source pair for a typed one. Every map in the run that
// used to key on the bare command needs this, because a typed check HAS no command — keyed on the
// empty string, every typed check in a set collapses onto one entry, and whatever that entry
// decides (already green, survived the audit, has an `expect` to restore) is silently applied to
// all the others. Returns "" for a check that says nothing at all; callers drop those.
func checkIdent(c council.DeliverableCheck) string {
	if cmd := strings.TrimSpace(c.Command); cmd != "" {
		return cmd
	}
	if as := strings.TrimSpace(c.Assert); as != "" {
		return as + "\x00" + strings.TrimSpace(c.Source)
	}
	return ""
}

// checkKey identifies a deliverable check by its step label + what it actually verifies, stable
// across the run (the same check runs the same thing each gate cycle). Keys the per-check pass state.
func checkKey(c council.DeliverableCheck) string {
	return strings.TrimSpace(c.Step) + "\x00" + checkIdent(c)
}

// checkWhat renders what a check actually RUNS, for the ledger lines and for the description a
// check with no `deliverable` falls back to. A typed check has no command to print, and a bare
// "step 3 failed" with an empty backtick pair told a re-plan nothing about what was unmet.
func checkWhat(c council.DeliverableCheck) string {
	as := strings.TrimSpace(c.Assert)
	if as == "" {
		return strings.TrimSpace(c.Command)
	}
	if src := strings.TrimSpace(c.Source); src != "" {
		return src + ": " + as
	}
	return as
}

// checkAlreadyGreen reports whether a check has already been recorded ✓ this run — used to make
// per-step recording idempotent so a step whose gate (verifyStepChecks) already passed it is not
// re-run at the completion hook.
func (a *App) checkAlreadyGreen(sid session.SessionID, c council.DeliverableCheck) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return false
	}
	return st.passedChecks[checkKey(c)]
}

// runCheckRecord runs one deliverable check, emits its result event (recording the ✓/✗ pass state so
// the panel and the trust-green gate can read it), and returns the outcome. code -1 = the platform
// vanished (cannot verify), code 127 = the check command is unexecutable here; in BOTH cases nothing
// is emitted and pass is false, so a broken check never records a false ✗. Callers layer their own
// trust-green skip and -1/127 policy around it — centralizing the run+Passes+emit body keeps that
// contract identical across the per-step recorders and the terminal gate.
func (a *App) runCheckRecord(ctx context.Context, sid session.SessionID, workdir string, c council.DeliverableCheck) (pass bool, code int, out string) {
	out, code = a.runCheck(ctx, sid, workdir, c)
	if code == -1 || checkUnrunnable(code) {
		return false, code, out
	}
	pass = c.Passes(out, code)
	a.emitStepCheck(ctx, sid, c, code, pass, out)
	return pass, code, out
}

// recordStepChecks runs and records the deliverable checks that belong to plan step stepIdx, the
// moment that step is marked completed — the per-step, path-agnostic half of the completion panel.
// verifyStepChecks records a delegate step's checks when it GATES the step, but the scout and refine
// completion paths never run it, so their steps advanced unrecorded and every ✓ came from the terminal
// runStepGate all at once ("0/N until the end"). Driving it from the single completion hook
// (completeThrough) fills the panel as each step lands, on every execution path. Idempotent: a check
// already ✓ (delegate's verifyStepChecks) is skipped, so this never double-runs a passed check; an
// unexecutable (127) or unverifiable (-1) result is not recorded — the terminal gate and the worker's
// substitution evidence settle those. It only RECORDS for the panel; it does not gate control flow
// (completeThrough is a display/state signal, not the step gate).
func (a *App) recordStepChecks(ctx context.Context, sid session.SessionID, stepIdx int) {
	if !stepVerifyEnabled() || a.plat == nil {
		return
	}
	mine := matchStepChecks(a.cachedChecks(sid), stepIdx)
	if len(mine) == 0 {
		return
	}
	s := a.sessionInfo(ctx, sid)
	for _, c := range mine {
		if a.checkAlreadyGreen(sid, c) {
			continue // already ✓ (e.g. the delegate step gate ran it) — don't re-run
		}
		a.runCheckRecord(ctx, sid, s.Workdir, c) // emits ✓/✗; an unexecutable/unverifiable check self-skips
	}
}

// recordCheckResult stores one check's verify result so the plan panel can render a green ✓ for a
// passing check. The ✓ is STICKY: once a check has passed, a later flaky re-run (e.g. a server
// restarted between finish-gate passes) must NOT flicker the completed mark back to an empty box —
// the completion is real progress, and the council/step gate judge on the LIVE results, not this
// display state. So a pass always sets ✓, and a fail is recorded only while the check has NOT yet
// passed. Turn-scoped: cleared with deliverableChecks in resetForNewTopLevel.
func (a *App) recordCheckResult(sid session.SessionID, c council.DeliverableCheck, pass bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return
	}
	if st.passedChecks == nil {
		st.passedChecks = map[string]bool{}
	}
	key := checkKey(c)
	if pass {
		st.passedChecks[key] = true // passed → ✓, and it stays ✓ for the run
	} else if !st.passedChecks[key] {
		st.passedChecks[key] = false // not passed yet → reflect the failure; never downgrade a prior ✓
	}
}

// applyCheckSubs rewrites the stored deliverable checks from a worker's review-approved substitutions,
// so the FIX PERSISTS for the rest of the run: every later gate — including the terminal one — runs the
// command that actually works here instead of skipping the broken original. For each sub it finds the
// step's check whose command matches the sub's Original (exact match when a step has several checks),
// rewrites its Command/Expect to the working equivalent, and falls back to the step's sole check or a
// new appended check when Original does not match. The worker already ran the equivalent and its review
// council approved it, so the rewritten check is recorded ✓ without re-running (trusted) — the terminal
// trust-green gate then honors it rather than re-litigating an already-agreed substitution.
func (a *App) applyCheckSubs(ctx context.Context, sid session.SessionID, subs []port.CheckSub) {
	if len(subs) == 0 {
		return
	}
	a.mu.Lock()
	st, ok := a.stateIf(sid)
	if !ok {
		a.mu.Unlock()
		return
	}
	checks := append([]council.DeliverableCheck(nil), st.deliverableChecks...)
	var rewritten []council.DeliverableCheck
	for _, sub := range subs {
		cmd := strings.TrimSpace(sub.Command)
		if cmd == "" {
			continue
		}
		step := strings.TrimSpace(sub.Step)
		orig := strings.TrimSpace(sub.Original)
		expect := strings.TrimSpace(sub.Expect)
		// Prefer an exact (step, original-command) match; else the step's sole check; else append.
		target := -1
		var stepIdxs []int
		for i := range checks {
			if strings.TrimSpace(checks[i].Step) != step {
				continue
			}
			stepIdxs = append(stepIdxs, i)
			if orig != "" && strings.TrimSpace(checks[i].Command) == orig {
				target = i
			}
		}
		if target < 0 && len(stepIdxs) == 1 {
			target = stepIdxs[0]
		}
		if target >= 0 {
			checks[target].Command = cmd
			checks[target].Expect = expect
			rewritten = append(rewritten, checks[target])
		} else {
			nc := council.DeliverableCheck{Step: step, Deliverable: "substituted check", Command: cmd, Expect: expect}
			checks = append(checks, nc)
			rewritten = append(rewritten, nc)
		}
	}
	st.deliverableChecks = checks
	a.mu.Unlock()
	// Record each rewritten check ✓ (the worker verified it and the review council approved it — trust
	// without re-running) and emit its event so the panel shows the substituted check as green.
	for _, c := range rewritten {
		// Approved substitution: the worker ran it and the review council accepted its evidence, so
		// there is no local output to carry — the verdict's provenance is the review, not a re-run.
		a.emitStepCheck(ctx, sid, c, 0, true, "")
	}
}

// recordPendingStepChecks runs and records every not-yet-✓ deliverable check, then checks off any
// plan step whose checks now all pass — the INCREMENTAL, turn-boundary counterpart to the per-step
// completeThrough hook. A mixed plan's SOLO steps are done by the MAIN agent in its own turns
// (executeSteps skips them, so completeThrough never fires for them); without this their ✓ would land
// only at the terminal runStepGate, all at once. Recording only: no gating, no nudge (the termination
// gate still owns those). Mirrors runStepGate's grouping (a step passes only when ALL its checks pass;
// 127/-1 checks are skipped, not counted as failures) and is idempotent — an already-✓ check is not
// re-run. Callers gate it on a mutation having occurred (a check can only newly-pass if state changed),
// so a read-only turn costs nothing.
func (a *App) recordPendingStepChecks(ctx context.Context, sid session.SessionID) {
	if !stepVerifyEnabled() || a.plat == nil {
		return
	}
	checks := a.cachedChecks(sid)
	if len(checks) == 0 {
		return
	}
	s := a.sessionInfo(ctx, sid)
	stepPass := map[string]bool{}
	stepSeen := map[string]bool{}
	for _, c := range checks {
		key := strings.TrimSpace(c.Step)
		// No todo-status frontier here: a SOLO step stays "pending" the whole time the main agent works
		// it (only scout/parallel/delegate call advanceTo → in_progress), so skipping "pending" checks
		// would skip exactly the solo work this recorder exists to capture — the per-step ✓ would never
		// land until the terminal gate, regressing the incremental recording. The green-skip below already
		// avoids re-running a passed check; a not-yet-passed check re-running each mutating turn is the
		// inherent cost of detecting when its step lands.
		pass := a.checkAlreadyGreen(sid, c) // already ✓ counts as a passed run; don't re-run it
		if !pass {
			var code int
			pass, code, _ = a.runCheckRecord(ctx, sid, s.Workdir, c)
			if code == -1 || checkUnrunnable(code) {
				continue // unverifiable (platform gone) or unexecutable check — skip, like runStepGate
			}
		}
		if !stepSeen[key] {
			stepSeen[key] = true
			stepPass[key] = true
		}
		if !pass {
			stepPass[key] = false
		}
	}
	a.checkOffPassedSteps(ctx, s.ID, stepPass)
}

// checkOffPassedSteps marks the todo of each fully-passing plan step completed. It maps
// a check's free-form Step label to a todo via matchTodoIndex; unmatched steps simply
// don't move a todo (the check still gated the finish).
func (a *App) checkOffPassedSteps(ctx context.Context, sid session.SessionID, stepPass map[string]bool) {
	// Deterministic order for stable events.
	keys := make([]string, 0, len(stepPass))
	for k := range stepPass {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	td := a.Todos(sid)
	for _, k := range keys {
		if !stepPass[k] {
			continue
		}
		if i := matchTodoIndex(td, k); i >= 0 {
			a.setTodoStatusIf(ctx, sid, plannerActor, i, "pending", "completed")
			a.setTodoStatusIf(ctx, sid, plannerActor, i, "in_progress", "completed")
		}
	}
}

// annotateTodosWithDeliverables appends each step's expected deliverable to its todo
// so the plan panel shows what the step must produce. Best-effort: a todo already
// carrying a deliverable annotation, or one no check maps to, is left as-is. Called
// once when the plan-audit checks are stored (flag on only).
func (a *App) annotateTodosWithDeliverables(ctx context.Context, sid session.SessionID, checks []council.DeliverableCheck) {
	td := a.Todos(sid)
	if len(td) == 0 {
		return
	}
	// One deliverable phrase per todo (the first check that maps to it); a step with
	// several deliverables shows the first, keeping the line short.
	deliv := make([]string, len(td))
	for _, c := range checks {
		d := strings.TrimSpace(c.Deliverable)
		if d == "" {
			continue
		}
		if i := matchTodoIndex(td, strings.TrimSpace(c.Step)); i >= 0 && deliv[i] == "" {
			deliv[i] = d
		}
	}
	next := append([]session.Todo(nil), td...)
	changed := false
	for i := range next {
		if deliv[i] == "" || strings.Contains(next[i].Content, " — produces: ") {
			continue
		}
		next[i].Content = next[i].Content + " — produces: " + deliv[i]
		changed = true
	}
	if changed {
		a.putTodos(ctx, sid, plannerActor, next)
	}
}

// matchTodoIndex maps a check's free-form Step label to a todo index. It accepts an
// ordinal ("1".."N", optionally "1." or "step 1"), an exact (case-insensitive) title
// match, or a substring match either direction. Returns -1 when nothing matches.
func matchTodoIndex(td []session.Todo, step string) int {
	step = strings.TrimSpace(step)
	if step == "" || len(td) == 0 {
		return -1
	}
	// Ordinal: pull the first integer out of "3", "3.", "step 3", "3) do x".
	if n := leadingInt(step); n >= 1 && n <= len(td) {
		return n - 1
	}
	low := strings.ToLower(step)
	// Exact title match first (compare against the pre-annotation title).
	for i, t := range td {
		if strings.EqualFold(todoTitle(t.Content), step) {
			return i
		}
	}
	// Then containment either direction.
	for i, t := range td {
		title := strings.ToLower(todoTitle(t.Content))
		if title != "" && (strings.Contains(title, low) || strings.Contains(low, title)) {
			return i
		}
	}
	return -1
}

// todoTitle strips a " — produces: …" annotation so matching compares against the
// original step title.
func todoTitle(content string) string {
	if i := strings.Index(content, " — produces: "); i >= 0 {
		return strings.TrimSpace(content[:i])
	}
	return strings.TrimSpace(content)
}

// leadingInt extracts the first run of digits in s (after trimming leading non-digits
// like "step "), returning -1 when the string doesn't start a numeric reference. It
// only treats s as ordinal when digits appear before any letter, so a title like
// "add 2 files" is NOT read as ordinal 2.
func leadingInt(s string) int {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "step ")
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return -1
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return -1
	}
	return n
}

// frozenContractClause renders the plan-audit's executable deliverable checks as the
// acceptance contract carried into a CONTINUE injection. It binds the fallback council
// to what was frozen at planning: the review may only hold the turn open for THESE
// items and cannot invent new scope (§3 — all demands happen at the planning stage).
// Empty when no checks were frozen (flag off or read/analyze-only plan), so the
// baseline continuation is untouched.
func (a *App) frozenContractClause(checks []council.DeliverableCheck) string {
	if len(checks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nAcceptance contract (frozen at planning — this review may ONLY judge against these items; " +
		"it must NOT add new requirements, deliverables, or scope beyond them):\n")
	for _, c := range checks {
		step := strings.TrimSpace(c.Step)
		deliv := strings.TrimSpace(c.Deliverable)
		switch {
		case step != "" && deliv != "":
			fmt.Fprintf(&b, "• step %s — %s (verify: %s)\n", step, deliv, checkWhat(c))
		case deliv != "":
			fmt.Fprintf(&b, "• %s (verify: %s)\n", deliv, checkWhat(c))
		default:
			fmt.Fprintf(&b, "• verify: %s\n", checkWhat(c))
		}
	}
	return b.String()
}
