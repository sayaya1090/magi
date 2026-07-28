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
			"why before continuing. " + backgroundWaitAdvice(tc.agent) +
			"Guess only when necessary: if a value is unknown but " +
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
	// Observed check-churn (finish-independent, solo-path counterpart to the delegate path's
	// verifyStepChecks gate): the agent's OWN build/test command has now FAILED across
	// exerciseChurnCap distinct edits without ever passing (noteExerciseResult, wired from the
	// bash exec path in execute.go). That is a non-convergence signal even when NO stall/idle/repeat
	// kind trips — a solo agent that keeps rewriting the deliverable with never-seen-before content
	// (monotonic-novel churn, not oscillation, so retractProgress never arms) and re-running the same
	// failing build would otherwise burn to the external wall clock with reward 0. Land gracefully
	// UNVERIFIED with the work standing so the external verifier judges the live deliverable — using
	// ONLY the agent's own executed results, no external clock. Checked before stuck() so it fires on
	// a run that never trips a stall/idle window (the solo gap this closes).
	if exerciseChurnLandEnabled() && tc.guard.exerciseChurnMax() >= exerciseChurnCap() {
		sid := tc.s.ID
		ts.unverifiedReason = "the agent's own build/test kept failing across repeated edits without " +
			"converging — landing with work standing so the external verifier judges the live deliverable"
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ts.council.rounds + 1, Decision: string(council.Done),
			Note: "the agent's own build/test kept failing across repeated edits without converging — " +
				"landing with work standing so the external verifier judges the live deliverable; treat as UNVERIFIED",
			Forced: true,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
		a.setStage(sid, stageFinalize)
		fd, _ := json.Marshal(event.TurnFinishedData{Usage: u, Unverified: true, Reason: ts.unverifiedReason})
		a.appendFact(ctx, sid, event.TypeTurnFinished, tc.actor, fd)
		return true, true
	}
	// Sterile re-plan loop (plan-structural counterpart of the exercise-churn land above): the agent
	// has replanned sterileReplanCap times without the completed-step high-water ever advancing —
	// re-decomposition that finishes nothing. Each replan mutates novel content, so the stall/idle
	// windows keep resetting and no build/test is the recurring failure, so neither the stall path nor
	// exercise-churn fires; this closes that window. Land UNVERIFIED with work standing (external
	// verifier judges the live deliverable) using ONLY magi's own replan/step signals — no external clock.
	if sterileReplanLandEnabled() && tc.guard.sterileReplanMax() >= sterileReplanCap() {
		sid := tc.s.ID
		ts.unverifiedReason = "repeated re-planning finished no new step (the completed-step high-water " +
			"never advanced) — landing with work standing so the external verifier judges the live deliverable"
		dd, _ := json.Marshal(event.CouncilDecidedData{
			Round: ts.council.rounds + 1, Decision: string(council.Done),
			Note: "repeated re-planning finished no new step (the completed-step high-water never advanced) — " +
				"landing with work standing so the external verifier judges the live deliverable; treat as UNVERIFIED",
			Forced: true,
		})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
		a.setStage(sid, stageFinalize)
		fd, _ := json.Marshal(event.TurnFinishedData{Usage: u, Unverified: true, Reason: ts.unverifiedReason})
		a.appendFact(ctx, sid, event.TypeTurnFinished, tc.actor, fd)
		return true, true
	}
	// What used to stand here was the force-stop: a repeat/stall/idle/spin kind from the guard
	// ended the run, with a visible error when nothing had been produced. Measured across every
	// recorded trial, that stop bought nothing — 396 runs that instead reached the external
	// deadline were still verified and 76 of them PASSED, while 28 runs magi stopped itself
	// produced no pass at all and 8 were never scored, because a nonzero exit reads to the caller
	// as "the agent failed to run" rather than "the agent decided to stop".
	//
	// The signals it read are still collected and still SAID — the stuck nudge above fires from the
	// same counters. What is gone is magi ending the run on its own reading of them.
	return false, false
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

// nudgeEmptyResult fires the once-per-turn "you ended without a result" nudge and loops. An empty
// answer delivered nothing the user can read — a reasoning-only stop, common with harmony-format
// weak models — and it holds whether the turn ran no tool at all or ran tools and then went silent,
// so a turn never finishes in silence.
//
// Fires once (ts.nudgedEmpty), so a still-empty retry then finishes normally.
func (a *App) nudgeEmptyResult(ctx context.Context, tc turnCtx, lastText string, ts *turnState) (loopAction, bool) {
	if ts.nudgedEmpty {
		return 0, false
	}
	if strings.TrimSpace(lastText) != "" {
		return 0, false
	}
	ts.nudgedEmpty = true
	msg := "You ended without giving a result. Write your findings/answer for the task now as your message."
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
	})
	a.appendFact(ctx, tc.s.ID, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "orchestrator"}, pd)
	return loopContinue, true
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
	s, agent, guard, maxSteps := tc.s, tc.agent, tc.guard, tc.maxSteps
	sid := s.ID
	// Non-converging self-check landing (internal signal only — no external wall clock): if the
	// agent's OWN build/test keeps failing across repeated edit cycles (observedCheckChurn, counted
	// where the calls actually run), verification is not converging. Churning edits to an external
	// hard-kill tears the live deliverable down; the council branch below lands UNVERIFIED with work
	// standing instead, so the external verifier judges the running result.
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
				"magi's record of this turn has no executed command naming what you wrote: "+
					strings.Join(un, ", ")+". That is what it can see, not a verdict on your work — a "+
					"compile or a syntax check is not an invocation either. Run the smallest REAL "+
					"invocation of each (its primary scenario) and check the output before finishing; "+
					"if one already ran under a name this record cannot match, or is not meant to be "+
					"executed directly, say so and finish.")
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
		stepsLeft:   maxSteps - step,
		turnElapsed: time.Since(tc.runStart),
	}, &ts.council)
	if keepWorking {
		// Council-path non-convergence landing (internal signal only — no external wall clock, the
		// same honest signal the step-gate branch uses). The council rejected this finish. If the agent
		// has EDITED the deliverable (mutation epoch advancing — noteCheckFail's epoch guard) across
		// checkChurnCap such rejected finishes without the council ever approving, verification is not
		// converging: a contradictory/impossible deliverable check the agent can't satisfy (kv-store-grpc
		// demanded stub files named with a hyphen that grpc_tools MUST spell with an underscore, so
		// satisfying the grep broke the import and vice-versa — an unwinnable edit loop). Churning edits
		// to the external hard-kill tears the live deliverable down (reward 0); land UNVERIFIED with work
		// standing so the external verifier judges the running result. Counted only on an epoch-advancing
		// edit (an idle re-finish is the idle-resubmit path above); a converging task resets on approval.
		if checkChurnLandEnabled() && guard.noteCheckFail() >= checkChurnCap() {
			dd, _ := json.Marshal(event.CouncilDecidedData{
				Round: ts.council.rounds + 1, Decision: string(council.Done),
				Note: "council kept rejecting across repeated edit cycles without converging — landing with " +
					"work standing so the external verifier judges the live deliverable; treat as UNVERIFIED",
				Forced: true,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, event.Actor{Kind: event.ActorSystem, ID: "council"}, dd)
			ts.unverifiedReason = "the council kept rejecting across repeated edit cycles without converging — " +
				"landing with work standing so the external verifier judges the live deliverable"
			return 0, false
		}
		ts.prevFinishText, ts.prevFinishCalls = lastText, guard.callCount()
		return loopContinue, true
	}
	// The gate is letting the turn finish. A non-empty reason means it did so WITHOUT approving
	// (deadlock/cost-cap/no-feedback/unavailable) — carry that into turn.finished so the UI shows
	// UNVERIFIED, not a confident done.
	ts.unverifiedReason = unv
	if unv == "" {
		guard.resetCheckChurn() // genuine council approval → verification converged; clear the churn count
	}
	return 0, false
}
