package app

import (
	"context"
	"strings"

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

// executeSteps runs each step by its strategy, accumulating explorer findings.
// A per-turn explorer budget caps total dispatch; a step that can't dispatch
// (solo, or a scout/parallel that yields nothing) degrades to "the main agent
// handles it" without aborting the procedure.
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
	r := a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agentName, Prompt: delegatePrompt(st, brief), PlanStepIndex: &i})
	text := strings.TrimSpace(r.Text)
	// ADaPT failure branch (reactive, as-needed decomposition): a hard failure (spawn error
	// or empty result), while we're still below the plan-depth cap and have budget, gets ONE
	// retry that tells the SAME executor to DECOMPOSE the sub-task into smaller independent
	// steps. The child re-plans at depth+1 (this is the natural decomposition point — it plans
	// from the Task), so a monolithic attempt that failed can succeed piece by piece. Single
	// attempt — bounded by the depth gate and the shared budget. Gated by MAGI_ADAPT: with it
	// off, a failed delegate backtracks after one shot (planned decomposition only).
	if !adaptDisabled() && (r.Err != "" || text == "") && depth+1 < a.cfg.MaxPlanDepth && *budget > 0 {
		*budget--
		r = a.spawn(ctx, s, depth, port.SpawnRequest{Agent: agentName, Prompt: redecomposePrompt(st, brief), PlanStepIndex: &i})
		text = strings.TrimSpace(r.Text)
	}
	if r.Err != "" || text == "" {
		// Still failed → the sub-task is NOT done. Leave its todo pending so the main
		// agent picks it up, and record it as FAILED (never "(delegated to …)", so the
		// redo-prevention directive can't tell the agent to skip re-doing it).
		note := "the delegated sub-agent returned no result"
		if r.Err != "" {
			note = "the delegated sub-agent errored: " + r.Err
		}
		a.setTodoStatusIf(ctx, s.ID, plannerActor, i, "in_progress", "pending")
		return stepFinding(st.Title, "delegate FAILED — do this yourself", "("+note+"; this sub-task is unfinished)"), false
	}
	a.completeThrough(ctx, s.ID, plannerActor, i)
	return stepFinding(st.Title, "delegated to "+agentName, text), true
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
	// Pick a write-capable executor for the re-plan, preferring the stuck agent itself
	// (same-executor retry, mirroring runDelegateStep). recoveryExecutor is intentionally NOT
	// gated by DisableDelegate: this is an emergency lifeline, not normal delegation, so it
	// stays available under the delegate=off default where the stall/deadlock/idle-resubmit
	// recovery would otherwise be dead.
	name, ok := a.recoveryExecutor(agent.Name)
	if !ok {
		return false // no write-capable executor → cannot recover, let the caller stop
	}
	r := a.spawn(ctx, s, depth, port.SpawnRequest{Agent: name, Prompt: stuckRedecomposePrompt(task, blockReason)})
	if r.Err != "" || strings.TrimSpace(r.Text) == "" {
		return false
	}
	a.injectSubagentResult(ctx, s.ID, name, r)
	return true
}
