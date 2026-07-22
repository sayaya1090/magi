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
// A per-turn explorer budget caps total dispatch; a step that can't dispatch
// (solo, or a scout/parallel that yields nothing) degrades to "the main agent
// handles it" without aborting the procedure. Solo→delegate routing is already
// applied up front (forceDelegateSteps), so a "solo" step here means force-delegate
// is off or no worker exists — the main agent handles it inline.
func (a *App) executeSteps(ctx context.Context, s session.Session, goal string, steps []planStep, depth int) (findings string, delegated bool) {
	budget := maxPlanExplorers
	stepCtx := !stepContextDisabled() // A/B: off → delegate/fan-out run context-free (pre-brief baseline)
	var rshare refineShare            // shared-session state carried across this plan's refine phases
	var out []string
	for i, st := range steps {
		if budget <= 0 || ctx.Err() != nil {
			break
		}
		// Write-capable steps (delegate, refine) are dispatched by the same caller glue — both
		// run inline in this sequential loop (never fanned out) so their writes can't race the
		// council's change capture (see allParallelSafe), and both re-plan at depth+1. They
		// differ only in the child's context and retry model: delegate hands off a self-contained
		// sub-task context-free; refine works an in-context sub-goal with the parent CLONED in.
		// The strategy selects the runner; the record-finding/OR-delegated glue is shared.
		if run := a.writeStepRunner(st.Strategy); run != nil {
			brief := ""
			if st.Strategy == "delegate" && stepCtx {
				brief = delegateBrief(goal, steps, i, out) // refine ignores this (it clones context)
			}
			if f, done := run(ctx, s, st, brief, i, depth, &budget, &rshare); f != "" {
				out = append(out, f)
				delegated = delegated || done
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
		var groups []planGroup
		switch st.Strategy {
		case "parallel":
			groups = capGroups(st.Groups, &budget)
		case "scout":
			groups = a.scoutGroups(ctx, s, st, &budget, depth)
		default: // solo → main agent does it; nothing to dispatch
			continue
		}
		if len(groups) == 0 {
			continue // per-step degrade
		}
		a.advanceTo(ctx, s.ID, plannerActor, i) // moved on to step i: earlier steps ✓, step i running ◐
		fanGoal := ""
		if stepCtx {
			fanGoal = goal // orient read-only explorers with the overall goal (no sibling outputs — they produce none)
		}
		if f := strings.TrimSpace(a.runExplorers(ctx, s, groups, fanGoal, depth)); f != "" {
			out = append(out, stepFinding(st.Title, "", f))
			a.completeThrough(ctx, s.ID, plannerActor, i) // step i done
		} else {
			a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending") // degraded → don't leave a stuck ◐
		}
	}
	return strings.Join(out, "\n\n"), delegated
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
// write-capable executor that re-plans at depth+1. It charges *budget per dispatch (like
// an explorer) and returns the finding to record plus done=true when the write actually
// landed — the caller ORs that into its delegated flag. An empty finding means the step
// degraded to solo (no valid executor); the caller records nothing and the main agent
// handles that work. Sequential by construction (never fanned out), so the writes can't
// race the council's change capture — see allParallelSafe.
func (a *App) runDelegateStep(ctx context.Context, s session.Session, st planStep, brief string, i, depth int, budget *int, _ *refineShare) (finding string, done bool) {
	agentName, ok := a.resolveWriteExecutor(st.Agent, false) // delegate requires a named executor
	if !ok {
		return "", false // no valid executor → degrade to solo (the main agent does it)
	}
	*budget-- // count against the per-turn dispatch budget like an explorer
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
	// Hand the worker its acceptance checklist (the plan-audit's executable deliverable checks for
	// this step): it must RUN each and confirm it passes before reporting done, so a delegated part
	// isn't left with a requirement silently skipped (caught only later at the orchestrator gate).
	if cl := workerChecklist(a.cachedChecks(s.ID), i); cl != "" {
		brief = strings.TrimSpace(brief + "\n\n" + cl)
	}
	r := a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agentName, Prompt: delegatePrompt(st, brief), Tools: curTools, PlanStepIndex: &i})
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
		r = a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agentName, Prompt: redecomposePrompt(st, brief), Tools: curTools, PlanStepIndex: &i})
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
	a.completeThrough(ctx, s.ID, plannerActor, i)
	return stepFinding(st.Title, "delegated to "+agentName, text), true
}

// workerChecklist renders the plan-audit's executable deliverable checks as an explicit acceptance
// checklist the delegated worker must satisfy before reporting done — each with the command to run
// and the expected result. Checks tagged for THIS step (by leading step number) are preferred; when
// none are tagged, all of the turn's checks are shown, since over-informing beats letting the
// worker silently skip a requirement. Empty when no checks were derived.
// stepChecks selects the deliverable checks that belong to plan step stepIdx (0-based),
// matching the council's 1-based Step label ("3", "3.", "3) …"); when none match it returns
// ALL checks — the same lenient fallback workerChecklist relies on so a mislabeled check is
// still surfaced rather than silently dropped. Shared by the worker brief and the TUI's
// per-subagent checklist view.
func stepChecks(checks []council.DeliverableCheck, stepIdx int) []council.DeliverableCheck {
	if len(checks) == 0 {
		return nil
	}
	want := strconv.Itoa(stepIdx + 1)
	var mine []council.DeliverableCheck
	for _, c := range checks {
		s := strings.TrimSpace(c.Step)
		if s == want || strings.HasPrefix(s, want+".") || strings.HasPrefix(s, want+" ") || strings.HasPrefix(s, want+")") {
			mine = append(mine, c)
		}
	}
	if len(mine) == 0 {
		return checks
	}
	return mine
}

func workerChecklist(checks []council.DeliverableCheck, stepIdx int) string {
	mine := stepChecks(checks, stepIdx)
	if len(mine) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Acceptance checklist — before you report done, RUN each of these and confirm it passes; " +
		"do NOT report done while any of them is failing. If an item genuinely CANNOT be satisfied — a real " +
		"blocker, not a bug you can fix — stop retrying it and report (status blocked/failed) WHICH item is " +
		"unmet and WHY, so it can be re-planned rather than silently dropped:")
	for i, c := range mine {
		fmt.Fprintf(&b, "\n%d. ", i+1)
		if d := strings.TrimSpace(c.Deliverable); d != "" {
			b.WriteString(d + " — ")
		}
		b.WriteString("run: " + strings.TrimSpace(c.Command))
		if e := strings.TrimSpace(c.Expect); e != "" {
			b.WriteString("  (expect: " + e + ")")
		}
	}
	return b.String()
}

// refineLocalRetries bounds how many INFORMED local attempts a refine node gets before it
// is declared exhausted and backtracks to the parent. Small on purpose: each attempt is a
// full child run, and a weak model must not thrash one node indefinitely. Also bounded by
// the shared per-turn dispatch budget and the depth cap.
const refineLocalRetries = 2

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
	// The brief arg is ignored: refine spawns a CLONED-context child (CloneContext below), so
	// the parent goal, prior refine seeds (recordRefineSuccess), and sibling notes already ride
	// in the cloned conversation — no separate brief is needed or wanted.
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
	fail := ""
	for attempt := 0; attempt < retries && *budget > 0; attempt++ {
		*budget-- // count each attempt against the per-turn dispatch budget, like an explorer
		req := port.SpawnRequest{Agent: agentName, Prompt: refinePrompt(st, fail), CloneContext: true, PlanStepIndex: &i}
		if shared && rs.sid != "" {
			// Reuse the shared child instead of re-cloning the parent: this phase (or retry)
			// runs on top of its predecessors' ACTUAL conversation, not a spawn-time snapshot.
			req.ReuseSession = rs.sid
			req.CloneContext = false
		}
		r := a.spawn(ctx, s, depth, req)
		if shared && r.SessionID != "" {
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
	return reportStatusWord(line) == "FAILED"
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
	switch reportStatusWord(line) {
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
	plan := a.runPlanner(ctx, spec, s, task, "", depth, a.cfg.MaxSteps, task)
	steps := guardExpansion(sanitizeSteps(plan), depth, a.cfg.MaxPlanDepth)
	if len(steps) < 2 {
		return false, false // nothing gained from decomposing into a single unit
	}
	// Append the recovery units BELOW any existing plan todos rather than replacing the list
	// (registerPlanTodos replaces wholesale): the stuck task is often one step of an outer plan,
	// and clobbering that list would erase the outer plan's progress from the panel. Todos()
	// hands out the live slice, so copy before appending.
	existing := a.Todos(s.ID)
	base := len(existing)
	combined := append([]session.Todo(nil), existing...)
	for _, st := range steps {
		combined = append(combined, session.Todo{Content: st.Title, Status: "pending"})
	}
	a.putTodos(ctx, s.ID, plannerActor, combined)
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
		req := port.SpawnRequest{
			Agent:    agent.Name,
			Prompt:   stuckUnitPrompt(st, blockReason),
			MaxSteps: stuckUnitBudget(a.cfg.MaxSteps),
			Recovery: recoveryRunCapEnabled(),
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
