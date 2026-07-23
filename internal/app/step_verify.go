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
		out, code := a.runVerifyCmd(ctx, s.Workdir, c.Command)
		if code == -1 { // platform vanished mid-run: can't verify → don't decide
			return gateInactive, ""
		}
		ok := c.Passes(out, code)
		results = append(results, result{check: c, out: out, pass: ok})
		key := strings.TrimSpace(c.Step)
		if !stepSeen[key] {
			stepSeen[key] = true
			stepPass[key] = true
		}
		if !ok {
			anyFail = true
			stepPass[key] = false
		}
		a.emitStepCheck(ctx, s.ID, c, code, ok)
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
		fmt.Fprintf(&ledger, "\n- step %q — %s: `%s`", r.check.Step, r.check.Deliverable, r.check.Command)
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
		fmt.Fprintf(&b, "\n• step %q — %s\n  command: %s\n", r.check.Step, r.check.Deliverable, r.check.Command)
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

// emitStepCheck records one check's deterministic result as its own reviewable fact, so the
// contract's execution is observable (parity with the plan-audit criteria artifact). It is a
// TypeStepCheck, NOT a council decision: a single check has no round or tally, and rendering it
// as a council-round outcome ("round 0: finished (no consensus) — 0 done / 0 continue") was
// misleading. The UI renders it as a clean ✓/✗ line from the structured fields.
func (a *App) emitStepCheck(ctx context.Context, sid session.SessionID, c council.DeliverableCheck, code int, pass bool) {
	a.recordCheckResult(sid, c, pass)
	dd, _ := json.Marshal(event.StepCheckData{
		Step:        strings.TrimSpace(c.Step),
		Deliverable: strings.TrimSpace(c.Deliverable),
		Command:     strings.TrimSpace(c.Command),
		Code:        code,
		Pass:        pass,
	})
	a.appendFact(ctx, sid, event.TypeStepCheck, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
}

// checkKey identifies a deliverable check by its step label + command, stable across the run
// (the same check runs the same command each gate cycle). Keys the per-check pass state.
func checkKey(c council.DeliverableCheck) string {
	return strings.TrimSpace(c.Step) + "\x00" + strings.TrimSpace(c.Command)
}

// recordCheckResult stores one check's latest verify result so the plan panel can render a
// green ✓ for a passing check (and revert it if a later run fails). Turn-scoped: cleared with
// deliverableChecks in resetForNewTopLevel.
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
	st.passedChecks[checkKey(c)] = pass
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
			fmt.Fprintf(&b, "• step %s — %s (verify: %s)\n", step, deliv, c.Command)
		case deliv != "":
			fmt.Fprintf(&b, "• %s (verify: %s)\n", deliv, c.Command)
		default:
			fmt.Fprintf(&b, "• verify: %s\n", c.Command)
		}
	}
	return b.String()
}
