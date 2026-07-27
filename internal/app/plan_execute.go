package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Plan step execution, split out of planner.go: given a sanitized plan, executeSteps drives
// each step by its strategy through the write-step runners (delegate / refine), the shared
// refine-session bookkeeping (refineShare + record/report helpers), and redecomposeStuck's
// same-executor retry when a step stalls. Behavior unchanged; planning/parsing stay in
// planner.go, scouting/exploration in the explorer helpers.

// refineShare threads the shared-session state across a plan's refine phases: the first phase
// pins the child session it created and that session's executor here; later phases reuse both
// so they run in ONE session with a stable agent. Zero value = no shared session yet.
type refineShare struct {
	sid   session.SessionID
	agent string
}

// ledgerEntry is one row of the shared artifact ledger: the concrete deliverables (file paths,
// interfaces, endpoints) a completed step produced, so every LATER step reuses the exact locations
// instead of guessing or re-creating them. Accumulated across a plan and passed VERBATIM to each
// worker (bypassing the curator, which paraphrases) and shown in every right panel — the ledger is
// shared by everyone working the plan.
type ledgerEntry struct {
	Step  string // the producing step's title
	Facts string // its handoff — the paths/interfaces the next step builds on, verbatim
}

// handoffFacts pulls a completed step's HANDOFF section — the exact paths/interfaces the worker
// declared for the next step — out of its rendered finding (subReport.result writes HANDOFF as the
// LAST weighted section, so everything after its label is the handoff). Empty when none was filed.
func handoffFacts(finding string) string {
	const tag = "\nHANDOFF: "
	if i := strings.LastIndex(finding, tag); i >= 0 {
		return strings.TrimSpace(finding[i+len(tag):])
	}
	return ""
}

// childLanded reports whether a plan step's child session(s) produced KEPT progress — at least one
// completed todo. It decides whether a SOLO stuck re-plan APPENDS its new units below an existing
// step (preserving a delegated sub-plan that actually got somewhere) or REPLACES it: a worker that
// DIED without landing anything leaves only pending/failed (or no) todos, so its stale sub-steps
// must be dropped, not stacked under the fresh plan. A partially-landed worker keeps the part it
// finished. Only a still-relevant, progressing sub-plan counts as an outer plan to preserve.
func (a *App) childLanded(parent session.SessionID, step int) bool {
	for _, kid := range a.PlanChildren(parent, step) {
		for _, td := range a.Todos(kid) {
			if td.Status == "completed" {
				return true
			}
		}
	}
	return false
}

// appendLedger records a completed step's produced deliverables on the plan session's shared ledger.
// No-op on empty facts (nothing concrete to hand off).
func (a *App) appendLedger(sid session.SessionID, step, facts string) {
	if strings.TrimSpace(facts) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	st.stepLedger = append(st.stepLedger, ledgerEntry{Step: strings.TrimSpace(step), Facts: strings.TrimSpace(facts)})
}

// ledgerOf returns a session's OWN artifact ledger (the steps its plan has produced so far). Used to
// inject the ledger into the next worker and to render the top-level plan panel.
func (a *App) ledgerOf(sid session.SessionID) []ledgerEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.stepLedger
	}
	return nil
}

// sharedLedger returns the ledger a session SHARES with the plan it belongs to: a delegate child
// sees its PARENT's ledger (what its sibling steps produced — the shared context it was handed), a
// top-level session sees its own. Read by the TUI so every worker panel shows the same ledger.
func (a *App) sharedLedger(sid session.SessionID) []ledgerEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return nil
	}
	if st.meta.Parent != "" {
		if pst, ok := a.stateIf(st.meta.Parent); ok {
			return pst.stepLedger
		}
	}
	return st.stepLedger
}

// renderLedger formats the shared ledger as the VERBATIM block appended to a worker's prompt (after
// curation, so the exact paths survive the curator's paraphrase). Empty when the ledger is empty.
func renderLedger(entries []ledgerEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("── SHARED DELIVERABLES LEDGER (exact paths/interfaces earlier steps ALREADY produced — " +
		"reuse these VERBATIM; do NOT re-create, re-download, or guess where they are) ──")
	for _, e := range entries {
		line := clipLine(e.Facts, 400)
		if e.Step != "" {
			b.WriteString("\n• " + e.Step + ": " + line)
		} else {
			b.WriteString("\n• " + line)
		}
	}
	return b.String()
}

// concernBrief formats the plan council's UNRESOLVED critical concern as a VERBATIM block for a
// worker's prompt. The council raises these about a plan whose steps are then handed to workers, but
// injectCouncilAdvice appends the notes only to the session it reviewed — so the agent that actually
// carries out the step never hears what the council could not resolve about it. Framing matters as
// much as delivery here: a worker owns ONE step, and a concern about the plan as a whole must not
// read as an instruction to go do the other steps' work.
func concernBrief(concern string) string {
	concern = strings.TrimSpace(concern)
	if concern == "" || !workerConcernEnabled() {
		return ""
	}
	return "── PLAN REVIEW — UNRESOLVED CONCERN (the review council raised this about the plan and could " +
		"not resolve it; execution proceeded anyway) ──\n" + clipLine(concern, 1200) +
		"\n\nApply this to YOUR part only. If it bears on the step you were given, satisfy it as you do that " +
		"step. If it bears on someone else's step, it is not yours to carry out — do not widen your scope; " +
		"say so in your report instead."
}

// forceDelegateSteps rewrites every "solo" step into a "delegate" step routed to a worker, ONCE and
// up front (before the todos are registered and before executeSteps runs) rather than per-step at
// dispatch. This keeps the plan the user SEES honest: previously the rewrite happened inside
// executeSteps, so the rendered todos still read "[solo]" while execution silently routed the step to
// a worker. No-op when force-delegate is off or no worker is available — the steps stay "solo" and the
// main agent runs them inline. Idempotent: a step already "delegate" is left untouched.
func (a *App) forceDelegateSteps(steps []planStep) []planStep {
	if !forceDelegateEnabled() {
		return steps
	}
	names := a.delegatableAgents()
	if len(names) == 0 {
		return steps
	}
	// Spawn budget (plan-driven twin of the dispatch note): once this turn has already delegated past
	// the soft budget, STOP rewriting solo steps into delegates. Delegation is needed early to exceed a
	// single agent's context ceiling, but a re-plan after over-spawning that re-delegates everything
	// just fragments the context across more workers and rarely rescues a stuck task — so keep the
	// steps solo and let the main agent do the work directly. Same soft budget as dispatchBudgetNote.
	if !envOff("MAGI_SPAWN_BUDGET") {
		soft := a.cfg.MaxAgents / 4
		if soft < 4 {
			soft = 4
		}
		if int(a.spawnCount.Load()) >= soft {
			return steps
		}
	}
	for i := range steps {
		if steps[i].Strategy == "solo" {
			steps[i].Strategy = "delegate"
			if strings.TrimSpace(steps[i].Agent) == "" {
				steps[i].Agent = names[0]
			}
		}
	}
	return steps
}

// executeSteps runs each step by its strategy, accumulating explorer findings.
// Two per-turn budgets cap dispatch — one for read-only exploration, one for write
// steps (see maxPlanWriteSteps) — so a wide fan-out cannot starve the plan's own
// execution. A step that can't dispatch (solo, or a scout/parallel that yields
// nothing) degrades to "the main agent handles it" without aborting the procedure.
// Solo→delegate routing is already applied up front (forceDelegateSteps), so a
// "solo" step here means force-delegate is off or no worker exists — the main agent
// handles it inline.
func (a *App) executeSteps(ctx context.Context, s session.Session, goal string, steps []planStep, depth int) (findings string, delegated bool) {
	explore := maxPlanExplorers
	write := maxPlanWriteSteps
	wb := &write
	if !splitBudgetEnabled() {
		wb = &explore // A/B baseline: one shared pool, where read-only fan-out can consume the write steps' capacity
	}
	stepCtx := !stepContextDisabled() // A/B: off → delegate/fan-out run context-free (pre-brief baseline)
	var rshare refineShare            // shared-session state carried across this plan's refine phases
	var out []string
	// The next worker's brief needs these two APART: a finding it can build on reads very
	// differently from one whose step produced nothing. Split by the runner's own done flag rather
	// than by sniffing the finding text, so the two can never disagree about what landed.
	var produced, failed []string
	for i, st := range steps {
		if ctx.Err() != nil {
			break
		}
		// Write-capable steps (delegate, refine) are dispatched by the same caller glue — both
		// run inline in this sequential loop (never fanned out) so their writes can't race the
		// council's change capture (see allParallelSafe), and both re-plan at depth+1. They
		// differ only in the child's context and retry model: delegate hands off a self-contained
		// sub-task context-free; refine works an in-context sub-goal with the parent CLONED in.
		// The strategy selects the runner; the record-finding/OR-delegated glue is shared.
		if run := a.writeStepRunner(st.Strategy); run != nil {
			// Out of write budget: the rest of the plan is about to be executed by NOBODY. Say so
			// in the findings instead of breaking silently — the main agent inherits this work and
			// until it is told, the plan's tail simply disappears from the record.
			if *wb <= 0 {
				out = append(out, undispatchedFinding(steps[i:]))
				break
			}
			brief := ""
			if st.Strategy == "delegate" && stepCtx {
				brief = delegateBrief(goal, steps, i, produced, failed) // refine ignores this (it clones context)
			}
			if f, done := run(ctx, s, st, brief, i, depth, wb, &rshare); f != "" {
				out = append(out, f)
				if done {
					produced = append(produced, f)
				} else {
					failed = append(failed, f)
				}
				delegated = delegated || done
				// Record what this step produced on the SHARED ledger, so the next worker gets its exact
				// paths/interfaces verbatim (below) instead of the curator's paraphrase. Prefer the
				// worker's own HANDOFF; fall back to the result summary when it filed none.
				if done {
					facts := handoffFacts(f)
					if facts == "" {
						facts = clipLine(stripReportStatus(f), 200)
					}
					a.appendLedger(s.ID, st.Title, facts)
				}
				// Sequential re-plan: under force-delegate the steps were originally solo (dependent),
				// so a step that could NOT produce its result leaves the LATER steps without their
				// prerequisite. Stop here rather than run them on a missing input — the recorded FAILED
				// finding drives the finish gate to re-plan from what's actually done. (Independent
				// natural-delegate plans are not force-delegated, so they keep running the other parts.)
				if !done && forceDelegateEnabled() {
					break
				}
			}
			continue
		}
		// Hoisted above the dispatch: the scout's DISCOVERY explorer needs the same orientation
		// its per-item explorers get — it decides which items exist at all.
		fanGoal := ""
		if stepCtx {
			fanGoal = goal // orient read-only explorers with the overall goal (no sibling outputs — they produce none)
		}
		var groups []planGroup
		switch st.Strategy {
		case "parallel":
			groups = capGroups(st.Groups, &explore)
		case "scout":
			groups = a.scoutGroups(ctx, s, fanGoal, st, &explore, depth)
		default: // solo → main agent does it; nothing to dispatch
			continue
		}
		if len(groups) == 0 {
			continue // per-step degrade
		}
		a.advanceTo(ctx, s.ID, plannerActor, i) // moved on to step i: earlier steps ✓, step i running ◐
		if f := strings.TrimSpace(a.runExplorers(ctx, s, groups, fanGoal, depth)); f != "" {
			ef := stepFinding(st.Title, "", f)
			out = append(out, ef)
			produced = append(produced, ef) // a read-only step's finding IS its output

			a.completeThrough(ctx, s.ID, plannerActor, i) // step i done
		} else {
			a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending") // degraded → don't leave a stuck ◐
		}
	}
	return strings.Join(out, "\n\n"), delegated
}

// undispatchedFinding names the plan steps the turn ran out of write budget for. Every other
// way a step fails to dispatch leaves a trace — a degraded step keeps its todo pending, a failed
// one records a FAILED finding — but budget exhaustion used to just break the loop: the findings
// block stopped mid-plan, nothing said why, and the work belonged to no one. rest must be the
// steps from the undispatched one onward.
func undispatchedFinding(rest []planStep) string {
	var b strings.Builder
	for _, st := range rest {
		b.WriteString("\n• " + st.Title)
	}
	return stepFinding("Steps not dispatched", "this turn's sub-agent dispatch budget ran out",
		"NO sub-agent was asked to do these, and none is working on them now — nothing has been done "+
			"for them:"+b.String()+"\n\nThey are YOURS. Carry them out yourself in this session, in order, "+
			"and satisfy the acceptance checks labeled for those steps as you go.")
}

// stepFinding formats a step's recorded finding as a "### Title (status)" header followed by
// the body — the single shape every write-step and explorer result uses. status is the
// parenthetical tag ("refined", "delegated to coder", "refine FAILED …"); pass "" for a bare
// "### Title" header (the explorer/parallel case).
func stepFinding(title, status, body string) string {
	h := "### " + title
	if status != "" {
		h += " (" + status + ")"
	}
	return h + "\n" + body
}

// resolveWriteExecutor picks the write-capable agent to run a write-step (delegate or refine).
// A named, valid delegatable agent always wins. When the step named none (or a read-only/unknown
// one) and fallbackAny is set, it falls back to the first delegatable agent — this is refine's
// contract (its "agent" is OPTIONAL, since a refine step states a high-level GOAL, not who runs
// it; the CLONED context carries the sub-goal, not the executor identity). delegate passes
// fallbackAny=false: it requires a named executor. ok=false → no executor → degrade to solo.
func (a *App) resolveWriteExecutor(stAgent string, fallbackAny bool) (string, bool) {
	if name, ok := a.delegateAgentName(stAgent); ok {
		return name, true
	}
	if fallbackAny {
		if names := a.delegatableAgents(); len(names) > 0 {
			return names[0], true
		}
	}
	return "", false
}

// writeStepFn runs one write-capable step (delegate or refine): it returns the finding to
// record and done=true when the write actually landed. Both runners share this signature so
// executeSteps dispatches them through one path (the record-finding / OR-delegated glue).
// brief is the delegate context brief (see delegateBrief); refine ignores it (it clones the
// parent context instead), so the caller passes "" for refine steps.
// rs threads the refine shared-session state across a plan's phases; delegate ignores it.
type writeStepFn func(ctx context.Context, s session.Session, st planStep, brief string, i, depth int, budget *int, rs *refineShare) (finding string, done bool)

// writeStepRunner maps a write-capable strategy to its runner, or nil for a strategy this
// method does not own (parallel/scout/solo fall through to explorer/degrade handling).
func (a *App) writeStepRunner(strategy string) writeStepFn {
	switch strategy {
	case "delegate":
		return a.runDelegateStep
	case "refine":
		return a.runRefineStep
	}
	return nil
}

// runDelegateStep dispatches one delegate step: hand its self-contained sub-task to a
// write-capable executor that re-plans at depth+1. It charges the write budget per dispatch
// and returns the finding to record plus done=true when the write actually
// landed — the caller ORs that into its delegated flag. An empty finding means the step
// degraded to solo (no valid executor); the caller records nothing and the main agent
// handles that work. Sequential by construction (never fanned out), so the writes can't
// race the council's change capture — see allParallelSafe.
func (a *App) runDelegateStep(ctx context.Context, s session.Session, st planStep, brief string, i, depth int, budget *int, _ *refineShare) (finding string, done bool) {
	agentName, ok := a.resolveWriteExecutor(st.Agent, false) // delegate requires a named executor
	if !ok {
		return "", false // no valid executor → degrade to solo (the main agent does it)
	}
	*budget-- // count against the per-turn WRITE dispatch budget (maxPlanWriteSteps)
	a.advanceTo(ctx, s.ID, plannerActor, i)
	// Context curator (MAGI_CURATE): distill a focused, literal-preserving brief and a task-scoped
	// tool allowlist for this worker. Best-effort — an empty brief leaves the mechanical brief and
	// the worker's default toolset (curTools nil), so curation never blocks the delegate.
	var curTools []string
	if curateEnabled() {
		if cb, ct := a.curateDelegate(ctx, a.agentFor(s), s, st, brief); cb != "" {
			brief = cb
			curTools = ct
		}
	}
	// The worker's acceptance checklist (the plan-audit's deliverable checks for this step) goes to
	// delegatePrompt as its own argument, NOT into the brief: the brief is rendered under a header
	// that calls itself reference-only and NOT a to-do list, which is the wrong thing to say about
	// the block that defines when this part is done. It rides with YOUR PART instead.
	checklist := workerChecklist(a.cachedChecks(s.ID), i)
	// The parent's context blocks — the shared artifact ledger, the mined contract, the council's
	// unresolved concern — VERBATIM and AFTER curation, so the curator's paraphrase can't drop a
	// file location or an identifier the next step needs. One list, shared with refine
	// (workerContextBlocks), because assembling it per hand-off is how the two drifted apart.
	brief = a.withWorkerContext(s.ID, brief)
	req := port.SpawnRequest{Agent: agentName, Prompt: delegatePrompt(st, brief, checklist), Tools: curTools, PlanStepIndex: &i}
	// A whole retry ladder already spent on THIS step is the answer, not a reason to start another
	// one. The ladder exists to give a failing attempt a different route — the failure reason and
	// the previous attempt's tool trail — so a second ladder is that same experiment repeated, and
	// the run pays another full attempt budget to learn what it already knows. Observed: one plan
	// step handed to six workers over twenty-eight minutes, every one of them ending before it
	// could report, and the step no closer to done than after the first.
	//
	// So say so instead of spawning. The finding lands as a FAILED step exactly as a spent ladder
	// would have, and carries what the planner needs to do something DIFFERENT: this part is too
	// big for one attempt, split it or run it here. A re-planned step whose task text changes keys
	// to a fresh ladder and is dispatched normally — the cap only bites on re-emitting the same
	// part verbatim.
	if n, spent := a.stepLadderSpent(s.ID, agentName, req); spent {
		a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending")
		a.emitToolProgress(s.ID, plannerActor, "", agentName, fmt.Sprintf(
			"not re-dispatching this step: %d attempt(s) have already been spent on it, none reported", n))
		return stepFinding(st.Title, "delegate NOT re-dispatched — the spent attempts ARE the finding",
			fmt.Sprintf("(this sub-task has already cost %d worker attempt(s), none of which reported a "+
				"result; handing the same part to another worker repeats that experiment. Split it into "+
				"smaller parts that each finish on their own, or do this part here yourself.)", n)), false
	}
	r := a.spawn(ctx, s, depth, req)
	text := strings.TrimSpace(r.Text)
	// ADaPT failure branch (reactive, as-needed decomposition): a hard failure (spawn error
	// or empty result), while we're still below the plan-depth cap and have budget, gets ONE
	// retry that tells the SAME executor to DECOMPOSE the sub-task into smaller independent
	// steps. The child re-plans at depth+1 (this is the natural decomposition point — it plans
	// from the Task), so a monolithic attempt that failed can succeed piece by piece. Single
	// attempt — bounded by the depth gate and the shared budget. Gated by MAGI_ADAPT: with it
	// off, a failed delegate backtracks after one shot (planned decomposition only).
	if !adaptDisabled() && delegateNotDone(r, text) && depth+1 < a.cfg.MaxPlanDepth && *budget > 0 {
		*budget--
		// Don't re-hand the identical brief — that restarts YOUR PART from scratch. Append what the
		// first attempt already satisfied (skip) vs what still fails (continue), so the retry picks up
		// mid-task rather than re-deriving. Falls back to a generic pivot digest when no checks exist.
		retryBrief := brief
		if cont := a.retryContinuation(ctx, s, a.effectiveSpec(agentName, curTools), i, r); cont != "" {
			retryBrief = strings.TrimSpace(brief + "\n\n" + cont)
		}
		r = a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agentName, Prompt: redecomposePrompt(st, retryBrief, checklist), Tools: curTools, PlanStepIndex: &i})
		text = strings.TrimSpace(r.Text)
	}
	if delegateNotDone(r, text) {
		// Still not done — spawn error, empty result, OR the worker reported it BLOCKED/FAILED (an
		// acceptance-checklist item it could not meet). The sub-task is NOT done: leave its todo
		// pending and record it as FAILED (never "(delegated to …)") WITH the worker's reason, so the
		// unmet requirement surfaces to the finish gate and drives re-planning rather than a silent
		// "done".
		note := "the delegated worker returned no result"
		if r.Err != "" {
			note = "the delegated worker errored: " + r.Err
		} else if text != "" {
			note = "the delegated worker could not complete it: " + clipLine(stripReportStatus(text), 300)
		}
		a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending")
		return stepFinding(st.Title, "delegate FAILED — re-plan or do it yourself", "("+note+"; this sub-task is unfinished)"), false
	}
	// Persist any review-approved check substitutions the worker filed: rewrite the stored checks to
	// the working commands BEFORE the step gate runs, so the gate (and every later gate) verifies the
	// command that actually works here instead of skipping the broken original. The worker's review
	// council already vetted these; applyCheckSubs records them ✓ (trusted, not re-run).
	if len(r.CheckSubs) > 0 {
		a.applyCheckSubs(ctx, s.ID, r.CheckSubs)
	}
	// Step gate: the worker CLAIMED done, but the step only completes when its OWN deliverable checks
	// actually PASS. A failing check means the claim is unverified — route the step to re-planning
	// (carrying the failing-check output as the reason, so the re-plan ADAPTS instead of re-emitting the
	// identical step) rather than advance a false "done" the council would then have to catch. There is
	// no restart loop: re-planning is already bounded by depth/budget and can change the approach, and a
	// worker that genuinely cannot pass a check reports it blocked/failed above (불가 + reason). This is
	// what lets the council assume every step it sees was actually verified.
	if pass, fails := a.verifyStepChecks(ctx, s, i); !pass {
		a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending")
		return stepFinding(st.Title, "delegate FAILED — deliverable check unmet, re-plan",
			"(the worker reported done but its deliverable checks FAILED:\n"+fails+"\nthis sub-task is NOT done)"), false
	}
	a.completeThrough(ctx, s.ID, plannerActor, i)
	return stepFinding(st.Title, "delegated to "+agentName, text), true
}

// verifyStepChecks runs the plan-audit deliverable checks that belong to step stepIdx and reports
// whether they ALL pass, plus a ledger of the failing ones (deliverable — command → actual output).
// It is the deterministic half of the step gate: a delegate worker's "done" is accepted only when its
// step's checks actually pass. Returns (true, "") when verification is inactive — flag off, no
// platform, or no checks for this step — so it never blocks a step it cannot judge.
func (a *App) verifyStepChecks(ctx context.Context, s session.Session, stepIdx int) (bool, string) {
	if !stepVerifyEnabled() || a.plat == nil {
		return true, ""
	}
	// STRICT match, not stepChecks' lenient "all checks" fallback: gate a step ONLY on checks labelled
	// for it. An unmatched check (e.g. a later step's "server on port 5328") cannot pass yet and would
	// falsely re-plan a step that is fine. Unlabelled/other-step checks are caught at the terminal gate.
	mine := matchStepChecks(a.cachedChecks(s.ID), stepIdx)
	if len(mine) == 0 {
		return true, ""
	}
	var fails []string
	for _, c := range mine {
		out, code := a.runCheck(ctx, s.ID, s.Workdir, c)
		if code == -1 { // platform vanished mid-run: can't verify → don't block the step
			return true, ""
		}
		// The CHECK could not be evaluated — its reader was not found (127), or the runner has no
		// assertion to apply (126) — which is not the deliverable failing. Do NOT churn the work on it:
		// skip it here (the worker files a substitution and the termination council judges the goal on
		// that evidence). Only a check that actually RAN and failed gates the step.
		if checkUnrunnable(code) {
			continue
		}
		pass := c.Passes(out, code)
		a.emitStepCheck(ctx, s.ID, c, code, pass, out)
		if !pass {
			d := strings.TrimSpace(c.Deliverable)
			if d == "" {
				d = checkWhat(c)
			}
			fails = append(fails, fmt.Sprintf("- %s — `%s` → %s", d, checkWhat(c), clipLine(strings.TrimSpace(out), 200)))
		}
	}
	if len(fails) == 0 {
		return true, ""
	}
	return false, strings.Join(fails, "\n")
}

// partitionStepChecks RUNS step stepIdx's deliverable checks and splits them by result: passed holds
// each passing check's Deliverable description, fails a "deliverable — command → actual output" ledger
// line per failing check. active is false when there is nothing to run — verify disabled, no platform,
// or no checks tagged for this step — so the caller can fall back to a non-check strategy. It is the
// detailed sibling of verifyStepChecks (which needs only the all-pass verdict + fail ledger); the
// checks are idempotent/no-mutation, so running them here to compute a retry split is side-effect free.
func (a *App) partitionStepChecks(ctx context.Context, s session.Session, stepIdx int) (passed, fails []string, active bool) {
	if !stepVerifyEnabled() || a.plat == nil {
		return nil, nil, false
	}
	mine := matchStepChecks(a.cachedChecks(s.ID), stepIdx)
	if len(mine) == 0 {
		return nil, nil, false
	}
	for _, c := range mine {
		out, code := a.runCheck(ctx, s.ID, s.Workdir, c)
		if code == -1 { // platform vanished mid-run: cannot judge → let the caller fall back
			return nil, nil, false
		}
		// The CHECK could not be evaluated (127 reader not found, or 126 nothing to assert), which is
		// NOT the deliverable failing — mirror verifyStepChecks and SKIP it, rather than counting it as
		// "still unmet" (which would steer the retry at a non-problem) and emitting a false ✗.
		if checkUnrunnable(code) {
			continue
		}
		pass := c.Passes(out, code)
		a.emitStepCheck(ctx, s.ID, c, code, pass, out)
		d := strings.TrimSpace(c.Deliverable)
		if d == "" {
			d = checkWhat(c)
		}
		if pass {
			passed = append(passed, d)
		} else {
			fails = append(fails, fmt.Sprintf("- %s — `%s` → %s", d, checkWhat(c), clipLine(strings.TrimSpace(out), 200)))
		}
	}
	// Every matched check was unrunnable (127/126) → nothing was actually verified, so there is no split to
	// give; signal inactive so the caller falls back to the generic pivot instead of an empty "re-checked" block.
	if len(passed) == 0 && len(fails) == 0 {
		return nil, nil, false
	}
	return passed, fails, true
}

// retryContinuation builds the addendum that turns a delegate FAIL retry from a restart into a
// continuation. When the step has executable deliverable checks (retrySplitEnabled), it re-runs them
// via partitionStepChecks and hands the second attempt a split by REAL disk state: passing checks
// become an advisory "already satisfied — do not redo" block, failing checks a "still unmet — focus
// here" block. Checks are a SUBSET of the contract, so the passing set is a LOWER BOUND on real
// progress — the block says "don't redo what's proven", never "only the failing checks remain", so
// work not covered by a check is still the worker's own. With no checks to run (or the split flag
// off), it falls back to retryPivotNote's generic tool-trail digest + pivot directive, which is still
// better than re-handing the identical brief. Returns "" only if even the fallback yields nothing.
// spec is the retrying agent's own, so the fallback pivot is phrased for what it can reach.
func (a *App) retryContinuation(ctx context.Context, s session.Session, spec AgentSpec, stepIdx int, last port.SpawnResult) string {
	if retrySplitEnabled() {
		if passed, fails, active := a.partitionStepChecks(ctx, s, stepIdx); active {
			var b strings.Builder
			b.WriteString("# Continue from the previous attempt — do NOT restart YOUR PART from scratch\n")
			b.WriteString("A previous attempt left partial work on disk; its acceptance criteria were just re-checked:\n")
			if len(passed) > 0 {
				b.WriteString("\nALREADY SATISFIED (proven done — do NOT redo these; the rest of YOUR PART is still yours):\n")
				for _, d := range passed {
					b.WriteString("- " + d + "\n")
				}
			}
			if len(fails) > 0 {
				b.WriteString("\nSTILL UNMET (focus here — deliverable — command → actual output):\n" + strings.Join(fails, "\n") + "\n")
			}
			return strings.TrimSpace(b.String())
		}
	}
	return strings.TrimSpace(retryPivotNote(ctx, a, spec, last, 1))
}

// stepChecks selects the deliverable checks that belong to plan step stepIdx (0-based),
// matching the council's 1-based Step label ("3", "3.", "3) …"). The lenient "show ALL when
// none match" fallback fires ONLY when the WHOLE set is unlabeled (no check carries a numeric
// step): then step attribution is impossible and over-informing the worker beats dropping a
// requirement. But once ANY check IS step-labeled, a step whose label matches none of them gets
// an EMPTY list, never the union — because dumping every step's checks onto one worker flattens
// temporally-separate steps into a jointly-unsatisfiable checklist (a "produce a.tgz" existence
// check beside a later "cleanup: a.tgz absent" check can never both pass at once; observed on
// plexus #224 when title-labeled checks matched no step and fell back to the union). Shared by
// the worker brief (workerChecklist) and the TUI's per-subagent checklist view.
func stepChecks(checks []council.DeliverableCheck, stepIdx int) []council.DeliverableCheck {
	if len(checks) == 0 {
		return nil
	}
	if mine := matchStepChecks(checks, stepIdx); len(mine) > 0 {
		return mine
	}
	if anyStepLabeled(checks) {
		return nil // labeled set, but none for THIS step → show its own (none), never the contradictory union
	}
	return checks // wholly unlabeled: step attribution impossible → over-inform rather than drop
}

// anyStepLabeled reports whether at least one check carries a numeric step label (its Step,
// trimmed, begins with a digit) — i.e. the council attributed checks to steps, so stepChecks
// must honor those labels strictly instead of flattening the whole set onto one worker.
func anyStepLabeled(checks []council.DeliverableCheck) bool {
	for _, c := range checks {
		if s := strings.TrimSpace(c.Step); s != "" && s[0] >= '0' && s[0] <= '9' {
			return true
		}
	}
	return false
}

// matchStepChecks returns ONLY the checks whose council Step label matches plan step stepIdx
// (0-based → 1-based label "3", "3.", "3) …"), with NO lenient fallback. The gate (verifyStepChecks)
// uses this strict form: running an UNMATCHED check against a step is a false failure — e.g. gating
// step 1 "install deps" on a step-4 "server listening on port 5328" check that cannot pass until the
// server exists — which would re-plan a step that is actually fine. stepChecks keeps the lenient
// fallback because over-informing the WORKER (its acceptance checklist) is safe; over-GATING is not.
func matchStepChecks(checks []council.DeliverableCheck, stepIdx int) []council.DeliverableCheck {
	want := strconv.Itoa(stepIdx + 1)
	var mine []council.DeliverableCheck
	for _, c := range checks {
		s := strings.TrimSpace(c.Step)
		if s == want || strings.HasPrefix(s, want+".") || strings.HasPrefix(s, want+" ") || strings.HasPrefix(s, want+")") {
			mine = append(mine, c)
		}
	}
	return mine
}

// refineLocalRetries bounds how many INFORMED local attempts a refine node gets before it
// is declared exhausted and backtracks to the parent. Small on purpose: each attempt is a
// full child run, and a weak model must not thrash one node indefinitely. Also bounded by
// the shared per-turn dispatch budget and the depth cap.
const refineLocalRetries = 2

// refineContext appends the VERBATIM reference blocks a refine child cannot inherit, in the same
// order and position a delegate worker gets them (after the hand-off, post-curation). Being CLONED
// is not the same as being told: the clone carries the parent's goal and the siblings' actual work,
// and neither of these rides in it, yet this step is judged against both.
//   - ledger: the exact paths survive a clone only until the shared refine session stops
//     re-cloning (ReuseSession, the default) — a delegate step landing between phase 1 and
//     phase 2 writes a ledger entry the shared child will never see.
//   - concern: injected as an ActorSystem message, and cloneConversation drops ActorSystem
//     prompts outright, so it does not inherit even on the phase that DOES clone.
//
// The acceptance checklist is NOT one of these: it is the definition of done for this step, not
// reference, so it is stated inside the sub-goal itself (refinePrompt/assignmentChecklist) rather
// than appended here below the prompt's closing "before reporting done" clause.
//
// Each block already carries its own header and is empty when it has nothing to say.
func refineContext(blocks ...string) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk = strings.TrimSpace(blk); blk != "" {
			b.WriteString("\n\n" + blk)
		}
	}
	return b.String()
}

// runRefineStep executes one hierarchical refine step: a large, NON-independent sub-goal
// worked out IN-CONTEXT. Unlike delegate's context-free hand-off, the sub-goal is re-planned at
// depth+1 with the full context carried forward. By default (sharedRefineEnabled) a plan's
// refine phases share ONE child session: the first phase seeds it by CLONING the parent, and
// later phases REUSE it (ReuseSession) so each sees its predecessors' actual work; with
// MAGI_REFINE_SHARED=0 every phase instead gets its own spawn-time clone. It drives the local
// re-plan / escalate loop the hierarchical model needs:
//   - success   → the child's writes are already in the shared tree; complete the todo and
//     return done=true (the caller ORs it into `delegated`, so the depth-0 review gate
//     verifies the merged result).
//   - failure   → record the failure back into the PARENT context and retry the node
//     locally. The failure reason is prefixed onto the retry prompt so the attempt is informed
//     ("a previous attempt failed because X"); under the shared session the retry also runs on
//     top of the failed attempt's actual conversation.
//   - exhausted → leave the todo pending, return a FAILED finding and done=false. The
//     failures now stand in the parent context, so the parent (itself possibly a refine
//     node) re-approaches with them in view — the "no more to try → backtrack up" step.
//
// An explicit STATUS: FAILED report from the child backtracks EARLY (its own accumulated
// failures told it the node is hopeless), without spending the remaining local retries.
// The executor is the step's own agent if it named one, else any delegatable agent; refine
// degrades to solo (the main agent works it out in-context) only when NONE is available.
func (a *App) runRefineStep(ctx context.Context, s session.Session, st planStep, _ string, i, depth int, budget *int, rs *refineShare) (finding string, done bool) {
	// The mechanical brief arg is ignored: refine spawns a CLONED-context child (CloneContext
	// below), so the parent goal, prior refine seeds (recordRefineSuccess), and sibling notes
	// already ride in the cloned conversation. What does NOT ride in it is appended explicitly
	// below (ctxBlocks) — being cloned is not the same as being told everything.
	// A refine step usually names NO executor (its "agent" is optional — see resolveWriteExecutor),
	// so fall back to any delegatable agent; the CLONED context, not the executor identity, carries
	// the sub-goal. Degrade to solo only when no delegatable agent exists at all.
	agentName, ok := a.resolveWriteExecutor(st.Agent, true)
	if !ok {
		return "", false
	}
	// Shared-session (default): once the first phase has created the shared child, later phases
	// reuse it AND its executor, so the run stays in ONE session with a stable agent.
	shared := sharedRefineEnabled()
	if shared && rs.sid != "" && rs.agent != "" {
		agentName = rs.agent
	}
	a.advanceTo(ctx, s.ID, plannerActor, i)
	// Reactive (as-needed) informed retries are the ADaPT mechanism; MAGI_ADAPT=0 cuts them to a
	// single shot, so a failed refine node backtracks to the parent instead of re-attempting.
	retries := refineLocalRetries
	if adaptDisabled() {
		retries = 1
	}
	// The checklist is stated INSIDE the sub-goal (refinePrompt), not trailed after the prompt's
	// closing "before reporting done" clause where it used to land — what the step must satisfy
	// belongs with the statement of the step. The ledger and the concern really are reference and
	// stay where they were, appended verbatim after it.
	checklist := workerChecklist(a.cachedChecks(s.ID), i)
	ctxBlocks := refineContext(a.workerContext(s.ID))
	fail := ""
	for attempt := 0; attempt < retries && *budget > 0; attempt++ {
		*budget-- // count each attempt against the per-turn WRITE dispatch budget (maxPlanWriteSteps)
		req := port.SpawnRequest{Agent: agentName, Prompt: refinePrompt(st, fail, checklist) + ctxBlocks, CloneContext: true, PlanStepIndex: &i}
		if shared && rs.sid != "" {
			// Reuse the shared child instead of re-cloning the parent: this phase (or retry)
			// runs on top of its predecessors' ACTUAL conversation, not a spawn-time snapshot.
			req.ReuseSession = rs.sid
			req.CloneContext = false
		}
		r := a.spawn(ctx, s, depth, req)
		// r.Err is checked because an EXHAUSTED spawn now carries its last attempt's session id
		// (for the salvage callers), and a spawn that failed outright is not the session later
		// phases should be built on — that id is a forensic record, not a working thread.
		if shared && r.Err == "" && r.SessionID != "" {
			// Pin the shared session + its executor for later phases. Assigned every attempt so a
			// reuse miss (fresh id returned) self-heals onto the new session; on a normal reuse
			// r.SessionID is the same id, so this is a no-op.
			rs.sid, rs.agent = r.SessionID, agentName
		}
		text := strings.TrimSpace(r.Text)
		if r.Err == "" && text != "" && !refineReportsFailure(text) {
			a.completeThrough(ctx, s.ID, plannerActor, i)
			// Seed the parent (main-agent) context with this phase's output. Under the shared
			// session siblings already see each other's ACTUAL work; this note is the summary the
			// PARENT reads (and the sibling-visibility fallback when MAGI_REFINE_SHARED=0).
			a.recordRefineSuccess(ctx, s.ID, st, text)
			return stepFinding(st.Title, "refined", text), true
		}
		// Record the failure into the PARENT context: the next clone carries it (informed
		// retry), and on exhaustion the parent backtracks with it in view.
		fail = refineFailReason(r, text)
		a.recordRefineFailure(ctx, s.ID, st, fail)
		if r.Err == "" && refineReportsFailure(text) {
			break // the child judged the node hopeless → backtrack now, don't burn retries
		}
	}
	a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending")
	return stepFinding(st.Title, "refine FAILED after local retries — reconsider the approach yourself", "("+fail+"; this sub-goal is unfinished)"), false
}

// refineReportsFailure reports whether the child explicitly declared the sub-goal failed
// (a "STATUS: FAILED" report frame — see subReport.result in app.go). This is the child's
// own "no viable approach" verdict, used to backtrack early.
func refineReportsFailure(text string) bool {
	line, _, _ := strings.Cut(strings.TrimLeft(text, "\n"), "\n")
	return reportStatusClaim(line) == "FAILED"
}

// delegateNotDone reports whether a delegate attempt did NOT finish its sub-task: a spawn error, an
// empty result, or a worker report whose leading STATUS is BLOCKED or FAILED (an acceptance-checklist
// item it could not meet). Unlike refineReportsFailure (FAILED only), a BLOCKED delegate also counts
// as not-done, so an unmet requirement surfaces for re-planning instead of being marked complete.
func delegateNotDone(r port.SpawnResult, text string) bool {
	if r.Err != "" || strings.TrimSpace(text) == "" {
		return true
	}
	line, _, _ := strings.Cut(strings.TrimLeft(text, "\n"), "\n")
	switch reportStatusClaim(line) {
	case "BLOCKED", "FAILED":
		return true
	}
	return false
}

// refineFailReason summarizes why a refine attempt failed, for the parent-context failure
// note and the next retry's prompt.
func refineFailReason(r port.SpawnResult, text string) string {
	switch {
	case r.Err != "":
		return "the attempt errored: " + r.Err
	case text == "":
		return "the attempt produced no result"
	case refineReportsFailure(text):
		if reason := strings.TrimSpace(stripReportStatus(text)); reason != "" {
			return "the attempt reported failure: " + clipLine(reason, 500)
		}
		return "the attempt reported failure"
	default:
		return "the attempt did not complete the sub-goal"
	}
}

// recordRefineFailure appends a refine node's failure into the PARENT session as an
// agent-authored note, so it enters the parent's context: the next local retry re-clones
// the parent and therefore sees it, and on escalation the parent re-approaches with it in
// view. This accumulating failure record is what the hierarchical backtracking relies on.
func (a *App) recordRefineFailure(ctx context.Context, sid session.SessionID, st planStep, reason string) {
	note := "Sub-goal not yet achieved — \"" + strings.TrimSpace(st.Title) + "\": " + reason
	_ = a.appendPromptText(context.WithoutCancel(ctx), sid,
		event.Actor{Kind: event.ActorAgent, ID: plannerAgent}, note)
}

// recordRefineSuccess is the SUCCESS mirror of recordRefineFailure: it appends a completed
// refine node's result into the PARENT session as an agent-authored note. This is what makes
// sequentially-dependent refine phases cohere. Each refine child's conversation is CLONED
// from the parent AT SPAWN TIME (CloneContext), and executeSteps only injects the batched
// findings once the whole loop is done — so without this, phase N spawns with a clone that
// predates phase N-1's output and can't build on it (mismatched packages/signatures, import
// cycles). Writing a compact completion note here seeds the next clone with what the prior
// phase produced. The result is clipped: the real code is already on disk, so the note only
// needs to carry the narrative (what was built, key names/signatures), not the transcript.
func (a *App) recordRefineSuccess(ctx context.Context, sid session.SessionID, st planStep, result string) {
	note := "Sub-goal completed — \"" + strings.TrimSpace(st.Title) + "\": " + clipLine(strings.TrimSpace(result), 800)
	_ = a.appendPromptText(context.WithoutCancel(ctx), sid,
		event.Actor{Kind: event.ActorAgent, ID: plannerAgent}, note)
}

// redecomposeStuck is the ADaPT failure-branch for a SOLO agent that got stuck — the same
// "BREAK IT DOWN and re-plan" recovery as runDelegateStep, but triggered mid-run when a solo
// attempt thrashed (stall guard exhausted) or deadlocked (council never approved) instead of
// on a delegated child's failure. It hands the ORIGINAL task, plus the concrete reason the
// last attempt was stuck, to a fresh write-capable executor that re-plans at depth+1 and
// continues from the partial work already on disk. The caller gates it to fire at most once
// per run and only for a plan-eligible (write-capable, below the depth cap) agent, so it can
// never recurse unboundedly or fire for a read-only leaf. Returns true when the child produced
// a result to integrate (injected as findings, so the parent verifies the merged output rather
// than blindly re-running); false when no executor is available or the child also failed, so
// the caller falls through to its existing force-stop/finish.
func (a *App) redecomposeStuck(ctx context.Context, s session.Session, agent AgentSpec, task, blockReason string, depth int) bool {
	// Recovery re-runs the stuck agent's OWN spec on a fresh re-plan of the task — the main
	// orchestrator doing the work itself, not a handoff to a separate coder subagent. Every
	// call site gates on planEligible → producesFiles(agent), so the stuck agent is guaranteed
	// write-capable; the guard below is a defensive backstop for that invariant. This is an
	// emergency lifeline (NOT normal delegation), so it stays available with no delegatable executor
	// where the stall/deadlock/idle-resubmit recovery would otherwise be dead. spawnResolved
	// (not spawn) is used because the main agent's spec is built on the fly and absent from
	// cfg.Agents, so a name lookup would fail.
	if !producesFiles(agent) {
		return false // read-only stuck agent → cannot author a recovery, let the caller stop
	}
	// Preferred recovery (default): decompose the stuck task into an explicit TODO list and drive
	// the units one at a time, each in a full-context child scoped to just that unit. This forces
	// incremental forward progress instead of re-handing the monolith. Falls through to the
	// single whole-task re-spawn below ONLY when the flag is off or decomposition yielded <2
	// units. When the decomposition actually ran and EVERY scoped unit failed, re-spawning the
	// whole task has even less chance than the units did — that would just burn one more child's
	// budget after N failures — so recovery reports failure and the caller force-stops honestly.
	if stuckDecomposeEnabled() {
		landed, attempted := a.driveStuckTodos(ctx, s, agent, task, blockReason, depth)
		if landed || attempted {
			return landed
		}
	}
	// Recovery is honored only under the run-tree cap (recoveryRunCapEnabled, default off): the
	// child then starts already-recovered and cannot cascade its own redecomposeStuck. Off =
	// baseline, multiple recovery executors per run tree (child re-arms recovery per depth level).
	// CloneContext seeds the child with the parent's conversation: recovery is the main orchestrator
	// CONTINUING its own work (not a clean-room hand-off), so the accumulated context — files already
	// read, partial work on disk — must carry forward, or the fresh child re-derives it and re-fixates.
	r := a.spawnResolved(ctx, s, depth, agent, port.SpawnRequest{
		Agent:        agent.Name,
		Prompt:       stuckRedecomposePrompt(task, blockReason),
		CloneContext: true,
		Recovery:     recoveryRunCapEnabled(),
	})
	if r.Err != "" || strings.TrimSpace(r.Text) == "" {
		return false
	}
	a.injectSubagentResult(ctx, s.ID, agent.Name, r)
	return true
}

// driveStuckTodos is the decomposing recovery: it re-plans the stuck task into an ordered TODO
// list and drives the units ONE AT A TIME. The first unit's child is seeded with the FULL parent
// context (CloneContext) — so it inherits everything already read/changed and does not re-fixate
// rebuilding context — and each later unit CONTINUES the previous landed unit's session
// (ReuseSession, the refine shared-session pattern), so it sees its predecessors' actual tool
// work rather than a summary, and the parent conversation is not re-cloned per unit. A unit that
// fails poisons its session with the failed attempt, so the chain resets and the next unit starts
// from a fresh parent clone. A unit that lands is integrated and its todo checked off before the
// next starts; a failed unit is left pending and the driver moves on, so a single stuck unit
// never sinks the whole recovery.
//
// landed is true when at least one unit produced integrated work. attempted is true when the
// decomposition actually ran (>=2 units were driven): the caller uses attempted && !landed to
// skip the whole-task fallback re-spawn — after every scoped unit failed, the monolith has even
// less chance. attempted==false means decomposition wasn't possible (no planner / <2 units) and
// the fallback is still worth one child.
func (a *App) driveStuckTodos(ctx context.Context, s session.Session, agent AgentSpec, task, blockReason string, depth int) (landed, attempted bool) {
	spec, ok := a.cfg.Agents[plannerAgent]
	if !ok {
		return false, false // no planner configured → cannot decompose
	}
	plan := a.runPlanner(ctx, spec, s, task, "", replanContext{}, depth, a.cfg.MaxSteps, task)
	steps := guardExpansion(sanitizeSteps(plan), depth, a.cfg.MaxPlanDepth)
	if len(steps) < 2 {
		return false, false // nothing gained from decomposing into a single unit
	}
	// Where the recovery units go depends on whether an OUTER delegated plan is in progress. If any
	// existing step has spawned a child session (a real delegate/refine sub-plan whose progress
	// renders in the tree), the stuck task is one step of that plan and clobbering the list would
	// erase its progress — so append the units BELOW. But on the SOLO path (no step has a child; the
	// existing todos are just this same whole task's own, now-superseded plan the main agent ran
	// inline) appending would stack a duplicate decomposition of the same task under the original —
	// so REPLACE wholesale, exactly like a fresh plan, and the panel shows one plan not two. Todos()
	// hands out the live slice, so copy before reusing.
	existing := a.Todos(s.ID)
	outerPlan := false
	for i := range existing {
		if a.childLanded(s.ID, i) {
			outerPlan = true
			break
		}
	}
	var combined []session.Todo
	base := 0
	if outerPlan {
		combined = append([]session.Todo(nil), existing...)
		base = len(existing)
	}
	for _, st := range steps {
		combined = append(combined, session.Todo{Content: st.Title, Status: "pending"})
	}
	a.putTodos(ctx, s.ID, plannerActor, combined)

	// Per-step contract for the RECOVERY re-plan (D-contract Stage 2): the normal plan path authors
	// per-step deliverable checks and gates each step on them, but this recovery re-plan spawned each
	// unit and marked it done on any non-empty result — no checks. Author them now so recovery units
	// are contracted and verified like every other step. Only on the solo-REPLACE path: these checks
	// wholly own the ledger there (step numbers 1..N align with the recovery units), whereas the
	// outerPlan APPEND path must keep the outer plan's own checks intact rather than clobber them.
	// Only when the step gate can actually RUN the checks (a platform is present, the same guard
	// verifyStepChecks uses): authoring checks that could never execute is a wasted side-call.
	gateRecovery := stepContractEnabled() && !outerPlan && a.plat != nil
	if gateRecovery {
		a.storeCoveredChecks(ctx, s, task, steps, nil)
	}
	var chain session.SessionID // last landed unit's session; empty → fresh clone from the parent
	for i, st := range steps {
		if ctx.Err() != nil {
			break
		}
		// Per-unit status, NOT advanceTo/completeThrough: those back-fill every earlier step to
		// completed (they assume strict in-order completion), which would silently mark a skipped
		// failed unit "done". Here each unit owns its status independently so a stalled one stays
		// visibly not-done while the rest advance.
		a.markTodoActive(ctx, s.ID, plannerActor, base+i) // this unit running ◐
		// Hand the recovery unit its own acceptance checklist, stated with the unit itself rather than
		// appended below the prompt's closing clauses — the same discipline, and the same placement, a
		// normal delegated step gets (workerChecklist / assignmentChecklist).
		unitChecks := ""
		if gateRecovery {
			unitChecks = workerChecklist(a.cachedChecks(s.ID), i)
		}
		prompt := stuckUnitPrompt(st, blockReason, unitChecks)
		req := port.SpawnRequest{
			Agent:    agent.Name,
			Prompt:   prompt,
			MaxSteps: stuckUnitBudget(a.cfg.MaxSteps),
			Recovery: recoveryRunCapEnabled(),
		}
		if gateRecovery {
			// Name the step this unit serves, so its pane shows the acceptance checklist it was
			// actually handed (SubagentChecklist keys on ParentStep). Only under gateRecovery:
			// there the recovery checks wholly own the ledger and unit i IS step i, whereas on the
			// outerPlan APPEND path the stored checks belong to the OUTER plan, and claiming step i
			// would show this unit a checklist written for somebody else's step.
			step := i
			req.PlanStepIndex = &step
		}
		if chain != "" {
			req.ReuseSession = chain
		} else {
			req.CloneContext = true
		}
		r := a.spawnResolved(ctx, s, depth, agent, req)
		if r.Err != "" || strings.TrimSpace(r.Text) == "" {
			a.setTodoStatusIf(ctx, s.ID, plannerActor, base+i, "in_progress", "pending") // stalled → revert, keep going
			chain = ""                                                                   // failed attempt poisons the shared session → next unit re-clones the parent
			continue
		}
		// Step gate for recovery: the unit CLAIMED done, but it only completes when its own deliverable
		// checks pass. A failing check leaves the unit pending (the next unit re-clones the parent), so a
		// recovery re-plan cannot false-complete a unit that returned text but did not meet its contract.
		if gateRecovery {
			if pass, _ := a.verifyStepChecks(ctx, s, i); !pass {
				a.setTodoStatusIf(ctx, s.ID, plannerActor, base+i, "in_progress", "pending")
				chain = ""
				continue
			}
		}
		a.injectSubagentResult(ctx, s.ID, agent.Name, r)
		a.setTodoStatusIf(ctx, s.ID, plannerActor, base+i, "in_progress", "completed") // this unit done
		chain = r.SessionID
		landed = true
	}
	return landed, true
}

// stuckUnitBudget caps a recovery unit child's loop steps. A unit is deliberately a small slice
// of the task, so it gets a quarter of the whole-task budget: enough for a read→edit→verify
// cycle, small enough that a unit which re-fixates fails fast and yields to the next unit
// instead of burning the full budget times the restart count, times N units — which could eat
// the run's remaining wall clock inside a single recovery. Floor of 8 keeps tiny configured
// budgets from degenerating to a child that can't finish one honest cycle.
func stuckUnitBudget(maxSteps int) int {
	b := maxSteps / 4
	if b < 8 {
		b = 8
	}
	return b
}
