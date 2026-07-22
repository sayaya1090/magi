package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// injectStuckNudge gives a thrashing agent ONE corrective nudge before the force-stop:
// re-read the task and change approach. Fires only when the run guard flags a nudge-worthy
// pattern (repeat / no-op spin / stall), with a message tailored to which. A stuck weak
// model often just needs redirecting, and this is far cheaper than burning the budget.
func (a *App) injectStuckNudge(ctx context.Context, tc turnCtx, turnTask string, evs []event.Event) {
	kind := tc.guard.shouldNudge()
	if kind == "" {
		return
	}
	sid := tc.s.ID
	// turnTask is empty for a subagent run (its seed is authored by ActorAgent, not
	// ActorUser), so fall back to the latest user-role message — the subagent's task —
	// mirroring the council gate's defensive fallback. Otherwise the re-grounding
	// would no-op exactly where weak models thrash most (narrow tool-driven subtasks).
	task := a.turnTaskOr(turnTask, sid, evs)
	msg := "You've repeated the same no-progress action several times and are getting blocked. " +
		"Stop and change approach: try a different tool or a smaller step, or inspect WHY the last " +
		"attempts failed (read the error, check paths/state) before retrying. Re-read the original task:\n" +
		clipSpec(task, 1500)
	if kind == "spin" {
		msg = "You've run a no-op command (echo/printf/true) several times in a row — a \"done\" banner is " +
			"not a step and does not finish the task. If the work is genuinely COMPLETE: end your turn now by " +
			"replying with NO tool call at all — that is what triggers verification and completion; do NOT run " +
			"another command. If it is NOT complete: stop announcing success and take a real action — run the " +
			"actual program/test against the deliverable, or fix what's failing. Re-read the original task:\n" +
			clipSpec(task, 1500)
	}
	if kind == "stalled" {
		msg = "You've run many steps without changing anything or making concrete progress — you may be " +
			"re-running checks or restating the same conclusion instead of advancing the task. If the work is " +
			"genuinely COMPLETE: end your turn now by replying with NO tool call at all — that is what triggers " +
			"verification and completion; another confirmation command does not, and never delete or rebuild " +
			"finished work just to produce visible activity. If it is NOT complete: stop and take a DIFFERENT " +
			"concrete action toward the deliverable; if something is blocking you, state exactly what it is and " +
			"why before continuing. If you are WAITING on a long-running install or build: do NOT restart it or " +
			"launch another copy — start it once (bash with background=true), then poll bash_output or block on " +
			"wait_for until it actually finishes. Guess only when necessary: if a value is unknown but " +
			"discoverable, run the tool or command that determines it (compute, parse, crack, query, or read " +
			"the real state) rather than trying values blindly. Re-read the original task:\n" +
			clipSpec(task, 1500)
	}
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
	})
	a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
}

// handleStuckGuard is the loop/stall/spin force-stop: it ends the run rather than burning
// the full step budget on hard repeats or a stall the agent kept ignoring after every nudge
// (varied-but-unproductive calls never trip the repeat count). It returns (stop, clean):
// stop=false means keep looping (not stuck, or recovery re-armed the run); stop=true ends the
// run now, with clean telling the caller whether to mark the turn finished (finalizeTodos
// completes vs cancels). A run that already wrote real output (mutationEpoch>0) and is only
// spinning on confirmation is effectively DONE — finish it cleanly (exit 0) rather than
// flagging an agent-level error that misreports a completed task as failure. A run that
// produced NOTHING is genuine thrash — abort with a visible error.
// turnTaskOr returns the tracked turnTask, falling back to re-deriving the task from the
// event log when it is empty — the shared fallback of the re-ground, stuck-recovery, and
// idle-resubmit paths, so they can't drift apart (this was copy-pasted four times).
func (a *App) turnTaskOr(turnTask string, sid session.SessionID, evs []event.Event) string {
	if task := strings.TrimSpace(turnTask); task != "" {
		return task
	}
	return strings.TrimSpace(lastUserText(reconstruct(a.taskEvents(sid, evs))))
}

func (a *App) handleStuckGuard(ctx context.Context, tc turnCtx, turnTask string, evs []event.Event, u event.Usage, ts *turnState) (bool, bool) {
	kind := tc.guard.stuck()
	if kind == "" {
		return false, false
	}
	s, agent, guard, depth := tc.s, tc.agent, tc.guard, tc.depth
	sid := s.ID
	// Last-resort structural recovery: a stall the agent kept ignoring after every nudge means
	// it is genuinely stuck, not just slow. Before force-stopping, hand the remaining work to a
	// fresh child that re-plans from scratch (redecomposeStuck), ONCE, for a plan-eligible
	// (write-capable, below the depth cap) agent. planEligible (not a depth==0 guard) is
	// intentional: a stuck solo SUBAGENT below the plan-depth cap can recover the same way; the
	// child is depth+1 so its own planEligible bounds further recursion. On success, reset the
	// stall window and keep looping so the parent integrates and verifies the child's result; on
	// failure fall through to the stop. recovered is set only on a successful spawn, so a
	// transient child failure does not permanently disable the (still fire-at-most-once) hook.
	// …UNLESS the stall is really an environment wait (a window dominated by sleep/ping/poll
	// commands): a fresh coder cannot speed an external wait, and with no delegatable executor the recovery
	// cascades coder→coder whose timeout is misreported as the run's own context-deadline.
	// waitGuardEnabled suppresses only the spawn; the honest stall stop below still lands.
	// The decomposing recovery (stuckDecomposeEnabled) also rescues a "repeat" stall — a
	// loop-guard block spiral (the read-loop fixation) that the old whole-task re-hand-off
	// could never help, since handing the monolith back just re-fixates. It is gated on the
	// flag precisely because only the decomposing recovery (small scoped units) breaks that
	// spiral; with the flag off, "repeat" keeps its baseline behavior (force-stop below).
	// The step-based "idle" kind (turnProgressCheckEnabled) is a reasoning rabbit hole — hours of
	// thinking with no deliverable — and routes to the SAME recovery. It carries an EXTRA wait
	// guard: besides the tool-call wait ratio (stallIsWait), childWaitMajority on the recent calls
	// suppresses it when the agent is polling a background job (a booting VM, a running build), so
	// a legitimate wait is never mistaken for a rabbit hole and cut.
	recoverable := kind == "stall" || (kind == "repeat" && stuckDecomposeEnabled()) ||
		(kind == "idle" && turnProgressCheckEnabled())
	waitBlocked := waitGuardEnabled() && (guard.stallIsWait() ||
		(kind == "idle" && childWaitMajority(evs, judgeDigestCalls)))
	// recoveryOutcome names why the run is stopping WITHOUT a successful recovery, and is
	// carried into the stop message: a force-stopped run whose recovery silently declined
	// (spawn cap, planner failure, wait-stall, already used) was otherwise undiagnosable
	// from the transcript — the forensics had to guess. One word each, greppable.
	recoveryOutcome := "not-attempted"
	switch {
	case !recoverable:
		recoveryOutcome = "repeat-not-recoverable (MAGI_STUCK_DECOMPOSE off)"
	case ts.recovered:
		recoveryOutcome = "already-used-this-run"
	case !a.planEligible(agent, depth):
		recoveryOutcome = "plan-ineligible"
	case waitBlocked:
		recoveryOutcome = "wait-stall (a fresh child cannot speed an external wait)"
	}
	if recoverable && !ts.recovered && a.planEligible(agent, depth) && !waitBlocked {
		task := a.turnTaskOr(turnTask, sid, evs)
		blockReason := "repeated no-progress: the previous attempt could not advance the task"
		if kind == "repeat" {
			blockReason = "loop guard blocked repeated identical actions: the previous attempt kept " +
				"retrying the same call instead of taking the next real step"
		} else if kind == "idle" {
			blockReason = "many steps of analysis produced NO deliverable (no file written/changed): the " +
				"previous attempt reasoned in circles instead of writing, running, and verifying the artifact"
		}
		// Name the CONCRETE walls (failed commands, timeouts, missing files) rather than only the generic
		// label, so recovery attacks the real obstacle instead of re-emitting the same plan — the
		// leadership move: specific reality + what to do differently beats "you went in circles" (which
		// produced the same plan 16x on fix-ocaml-gc).
		blockReason += stuckEvidence(evs, 4)
		if a.redecomposeStuck(ctx, s, agent, task, blockReason, depth) {
			ts.recovered = true
			if kind == "repeat" {
				guard.resetRepeat() // clear the blocked counter too, else stuck() re-halts immediately
			} else {
				guard.resetStall()
			}
			return false, false // recovered → keep looping
		}
		recoveryOutcome = "attempted-but-failed (planner or child spawn declined)"
	}
	if guard.mutationEpoch() > 0 {
		// Delivered-but-spinning: finish cleanly (TurnFinished → exit 0) with the work as-is.
		// The repeated no-progress steps already stand in the transcript, so no error event is
		// needed to explain the stop.
		a.setStage(sid, stageFinalize)
		fd, _ := json.Marshal(event.TurnFinishedData{Usage: u})
		a.appendFact(ctx, sid, event.TypeTurnFinished, tc.actor, fd)
		return true, true
	}
	msg, code := "stopped: the agent repeated the same action without progress (loop guard)", "loop_guard"
	if kind == "stall" {
		msg, code = "stopped: no real progress after repeated redirection (stall guard)", "stall_guard"
	}
	if kind == "spin" {
		msg, code = "stopped: repeated completion-banner spin without ending the turn (spin guard)", "spin_guard"
	}
	msg += "; recovery: " + recoveryOutcome
	d, _ := json.Marshal(event.ErrorData{Message: msg, Code: code})
	a.appendFact(ctx, sid, event.TypeError, event.Actor{Kind: event.ActorSystem, ID: "loop"}, d)
	return true, false
}

// finishTurn runs the no-tool-call finish path for a single step: stop-hook enforcement,
// the empty-subagent nudge, the top-level background-subagent wait, the consensus council
// termination gate (with its idle-resubmission short-circuit and deadlock redecompose), and
// — when the turn truly ends — the late-steer sweep plus the turn.finished record. It mutates
// ts (the once-per-turn guards, council accounting, stuck-recovery flag, and UNVERIFIED reason)
// and returns a loopAction telling the step loop whether to keep looping, re-enter without
// spending a step, finish the turn, or unwind a cancellation. Extracted verbatim from runLoop's
// step loop — behavior is unchanged; the caller owns step/lastText and acts on the returned action.
func (a *App) finishTurn(ctx context.Context, tc turnCtx, step int, turnTask, lastText string, evs []event.Event, usedTools bool, handledUserPrompts int, u event.Usage, ts *turnState) loopAction {
	if act, done := a.enforceStopHooks(ctx, tc, ts); done {
		return act
	}
	if act, done := a.nudgeEmptyResult(ctx, tc, lastText, ts); done {
		return act
	}
	if act, done := a.parkForBackground(ctx, tc); done {
		return act
	}
	if act, done := a.runTerminationGate(ctx, tc, step, turnTask, lastText, evs, usedTools, ts); done {
		return act
	}
	// A user steer can land AFTER this step's top-of-loop interjection scan but
	// before the turn commits here: during the final (no-tool) step's model stream,
	// or during a council deliberation that then voted done. It was never enqueued
	// (only the top-of-loop scan enqueues), and the finish path persists the
	// assistant's text after it, so the run goroutine's last-message-role safety net
	// (hasUnansweredUserPrompt) is fooled by the trailing assistant message too — the
	// steer would be silently lost, not even queued. Re-read the store one last time
	// and enqueue any prompt that appeared past the baseline so it drains as its own
	// turn. Top level only — subagents are not user-steerable.
	if tc.depth == 0 && !a.cfg.Workflow {
		a.enqueueLateInterjections(ctx, tc.s.ID, handledUserPrompts, turnTask)
	}
	a.setStage(tc.s.ID, stageFinalize) // turn is ending (D15)
	// A finish the council never approved (deadlock/cost-cap/resubmit) lands as
	// UNVERIFIED so the UI stops painting an abandoned task as a confident done.
	d, _ := json.Marshal(event.TurnFinishedData{Usage: u, Unverified: ts.unverifiedReason != "", Reason: ts.unverifiedReason})
	a.appendFact(ctx, tc.s.ID, event.TypeTurnFinished, tc.actor, d)
	return loopFinish
}

// enforceStopHooks runs the workspace's stop hooks once per turn. A failing hook is
// injected as a follow-up prompt and the turn keeps looping to fix it (returns true so
// the caller stops here); a pass leaves ts.stopChecked unset so a later finish attempt
// re-runs them, and the finish proceeds (false).
func (a *App) enforceStopHooks(ctx context.Context, tc turnCtx, ts *turnState) (loopAction, bool) {
	if ts.stopChecked {
		return 0, false
	}
	fail := a.runStopHooks(ctx, tc.s.Workdir)
	if fail == "" {
		return 0, false
	}
	ts.stopChecked = true // enforce once per turn to avoid an infinite loop
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: "A required check failed before finishing — fix it, then continue:\n" + fail}},
	})
	a.appendFact(ctx, tc.s.ID, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "hook"}, pd)
	return loopContinue, true
}

// nudgeEmptyResult fires the once-per-turn "you ended without a result" nudge and loops.
// A turn that produced no tool call this step must still deliver something:
//   - Subagent: if report is available it has NOT filed one (report terminates the run
//     earlier), so nudge it to call report; when report is unavailable, only an EMPTY
//     result warrants the nudge.
//   - Top level: empty answer text delivered nothing the user can read — a reasoning-only
//     stop, common with harmony-format weak models. This holds whether the turn ran no
//     tool at all or ran tools then went silent, so the user never gets a silent finish.
//
// Fires once (ts.nudgedEmpty), so a still-empty retry then finishes normally.
func (a *App) nudgeEmptyResult(ctx context.Context, tc turnCtx, lastText string, ts *turnState) (loopAction, bool) {
	if ts.nudgedEmpty {
		return 0, false
	}
	isSub := tc.s.Parent != ""
	_, hasReport := a.tools.Get("report")
	reportAvail := isSub && hasReport && tc.agent.allows("report")
	emptyResult := strings.TrimSpace(lastText) == ""
	if !((isSub && (reportAvail || emptyResult)) || (!isSub && emptyResult)) {
		return 0, false
	}
	ts.nudgedEmpty = true
	msg := "You are ending your turn without delivering a result. Call the 'report' tool NOW with " +
		"your actual findings/answer and a status (done/blocked/failed). Do not stop with a partial " +
		"thought; if the task isn't finished, continue it first."
	if !reportAvail {
		msg = "You ended without giving a result. Write your findings/answer for the task now as your message."
	}
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
	})
	a.appendFact(ctx, tc.s.ID, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "orchestrator"}, pd)
	return loopContinue, true
}

// parkForBackground is the sidecar wait (top level only): the orchestrator stays alive
// while background subagents run but is re-woken ONLY when there is something to act on —
// all subagents done (synthesize), a real user steer, or a subagent asking (escalation),
// NOT per individual result (those accumulate silently). Waiting does not consume the step
// budget. Returns loopRetryStep when there is work to act on, loopAbort on cancellation
// (so a cancelled park does not fall through and emit a second, "successful" turn.finished).
func (a *App) parkForBackground(ctx context.Context, tc turnCtx) (loopAction, bool) {
	if tc.depth != 0 {
		return 0, false
	}
	sid := tc.s.ID
	for a.bgOutstanding(sid) > 0 && ctx.Err() == nil && !a.needsOrchestratorTurn(ctx, sid) {
		select {
		case <-a.bgWaitChan(sid):
		case <-ctx.Done():
		}
	}
	if ctx.Err() == nil && a.needsOrchestratorTurn(ctx, sid) {
		// Mark current results consumed so we don't re-wake for them again
		// (multi-wave delegation re-arms this when new results are injected).
		a.bgConsume(sid)
		return loopRetryStep, true
	}
	if ctx.Err() != nil {
		return loopAbort, true
	}
	return 0, false
}

// runTerminationGate is the consensus council's finish decision (D14): top level only, not
// in workflow mode, and only for turns that did real work (a purely conversational reply has
// nothing to verify). The deterministic per-step deliverable gate (MAGI_STEP_VERIFY) runs
// first — all-pass finishes VERIFIED and SKIPS the open-ended council; a real failure injects
// the failing output and loops. Otherwise the termination council votes, with the idle-resubmit
// and deadlock stuck-recovery lifelines attached. Returns (action, true) to keep looping;
// (0, false) lets the finish proceed (carrying any UNVERIFIED reason the gate set on ts).
func (a *App) runTerminationGate(ctx context.Context, tc turnCtx, step int, turnTask, lastText string, evs []event.Event, usedTools bool, ts *turnState) (loopAction, bool) {
	if !(tc.depth == 0 && a.cfg.Council != nil && !a.cfg.Workflow && usedTools) {
		return 0, false
	}
	s, agent, guard, depth, maxSteps := tc.s, tc.agent, tc.guard, tc.depth, tc.maxSteps
	sid := s.ID
	stepGate, checkLedger := a.runStepGate(ctx, s, ts)
	if stepGate == gateFailRetry {
		return loopContinue, true
	}
	// No all-pass council skip: the ledger (checkLedger) is EVIDENCE fed to the council below, not
	// a fast-path to done — the council always judges, so trivial passing checks can't false-done.
	// Structural fabrication evidence for the council: if the agent changed a deliverable this
	// turn but ran no command exercising the current version, that is a hard, language-agnostic
	// fact a text-only vote can't wave through.
	fab := ""
	if guard.unverifiedDeliverable() {
		fab = "the agent changed a deliverable this turn but ran no command exercising the current version — it is unverified by execution"
	}
	// Idle resubmission short-circuit: the council rejected this answer, and the agent came back
	// having run NO tool and changed (almost) nothing — re-deliberating burns a round and prints
	// the same answer twice. Finish instead, marked UNVERIFIED.
	if ts.prevFinishCalls >= 0 && guard.callCount() == ts.prevFinishCalls && normEq(lastText, ts.prevFinishText) {
		// Before finishing UNVERIFIED, hand the task plus the concern it failed to act on to a
		// fresh write-capable child that re-plans and breaks it down (ADaPT solo-stuck branch) —
		// the same lifeline the stall/deadlock branches use, so the dominant idle-resubmit failure
		// gets one real recovery attempt. Fires once (ts.recovered); on success reset the stall
		// window AND the finish latch so the next round re-deliberates the child's integrated work.
		if !ts.recovered && a.planEligible(agent, depth) {
			task := a.turnTaskOr(turnTask, sid, evs)
			reason := strings.TrimSpace(ts.council.feedback)
			if reason == "" {
				reason = "the same answer was resubmitted unchanged after council feedback"
			}
			if a.redecomposeStuck(ctx, s, agent, task, reason, depth) {
				ts.recovered = true
				guard.resetStall()
				ts.prevFinishText, ts.prevFinishCalls = "", -1
				return loopContinue, true
			}
		}
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ts.council.rounds + 1, Decision: string(council.Done),
			Note:   "answer resubmitted unchanged after council feedback — finishing without re-deliberation; treat as UNVERIFIED",
			Forced: true,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
		ts.unverifiedReason = "the same answer was resubmitted unchanged after council feedback, without re-deliberation"
		return 0, false
	}
	// Exec-evidence layer 1 (deterministic, pre-council): the turn authored runnable
	// files that NO exercising command ever named — the exact signature of the
	// "written, never run, council approved anyway" regressions (headless-terminal
	// 2/7, large-scale 3/5; field-confirmed cross-model). One nudge before spending
	// a council round is cheaper than a rejection round, and non-blocking: a second
	// finish proceeds to the gate with the fact in the council's evidence instead.
	if execEvidenceEnabled() && !ts.execNudged {
		if un := guard.unexercisedArtifacts(); len(un) > 0 {
			ts.execNudged = true
			_ = a.appendPromptText(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "guard"},
				"You are finishing without ever RUNNING what you wrote: "+strings.Join(un, ", ")+
					" — no executed command has invoked these. Importing or compiling is not running. "+
					"Execute the smallest REAL invocation of each (the program's primary scenario) and "+
					"check its output before finishing; if one is genuinely not meant to be executed "+
					"directly, say so in your report.")
			return loopContinue, true
		}
	}
	changes := buildCouncilChanges(guard.changeSet())
	if execEvidenceEnabled() {
		if un := guard.unexercisedArtifacts(); len(un) > 0 {
			changes += "\n\n### authored but never executed this turn (no command ever invoked them)\n- " +
				strings.Join(un, "\n- ")
		}
	}
	keepWorking, unv := a.runCouncilGate(ctx, s, agent, councilInput{
		turnTask:    turnTask,
		lastText:    lastText,
		changes:     changes,
		fabrication: fab,
		checkLedger: checkLedger,
		stepsLeft:   maxSteps - step,
		turnElapsed: time.Since(tc.runStart),
	}, &ts.council)
	if keepWorking {
		ts.prevFinishText, ts.prevFinishCalls = lastText, guard.callCount()
		return loopContinue, true
	}
	// The gate is letting the turn finish. A non-empty reason means it did so WITHOUT approving
	// (deadlock/cost-cap/no-feedback/unavailable) — carry that into turn.finished so the UI shows
	// UNVERIFIED, not a confident done.
	ts.unverifiedReason = unv
	if !ts.recovered && ts.council.deadlocked &&
		strings.TrimSpace(ts.council.feedback) != "" && a.planEligible(agent, depth) {
		// Council deadlock: the members used every round and never approved, still holding an unmet
		// concern (ts.council.deadlocked — a DONE vote on the last allowed round does NOT reach here).
		// Before finishing UNVERIFIED, hand the task plus that exact concern to a fresh child that
		// re-plans and breaks it down (ADaPT failure-recursion). Fires once; on success reset the
		// stall window and loop so the parent integrates and verifies the child's work.
		task := a.turnTaskOr(turnTask, sid, evs)
		if a.redecomposeStuck(ctx, s, agent, task, ts.council.feedback, depth) {
			ts.recovered = true
			guard.resetStall()
			return loopContinue, true
		}
	}
	return 0, false
}
