package app

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// injectStuckNudge tells a circling agent what magi's counters say, once per pattern: an exact
// repeat ("blocked") or many varied calls with nothing changing ("stalled"). There is no
// force-stop behind it any more — it says the thing and the turn continues.
//
// The stalled message names BOTH forms of the council tool. It used to name only
// `complete: true`, so an agent told "take a different action" was never told that asking is
// one: measured across three tasks in one session, `council` was called to declare completion
// and never once to ask a question, while the only task that passed did so because the council
// spoke and was disagreed with.
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
	msg := "You've made the same call several times with nothing changing in between — magi is not " +
		"refusing it, it is telling you it is a repeat. Change approach: a different tool or a smaller " +
		"step, or inspect WHY the last attempts failed (read the error, check paths/state) before " +
		"retrying. Re-read the original task:\n" +
		clipSpec(task, 1500)
	if kind == "stalled" {
		msg = "You've run many steps without changing anything or making concrete progress — you may be " +
			"re-running checks or restating the same conclusion instead of advancing the task. If the work is " +
			"genuinely COMPLETE: say so — call the `council` tool with `complete: true`, and the members read " +
			"the record and either accept or tell you what is undone. Going quiet does not finish anything, " +
			"another confirmation command is not progress, and never delete or rebuild finished work just to " +
			"produce visible activity. If it is NOT complete: stop and take a DIFFERENT " +
			"concrete action toward the deliverable; if something is blocking you, state exactly what it is and " +
			"why before continuing. The same tool also answers a QUESTION without ending the turn: " +
			"`council` with a `question` and no `complete` puts the record you are working from in " +
			"front of the same three readers and hands back what each of them sees. " +
			backgroundWaitAdvice(tc.agent) +
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

// handleStuckGuard no longer guards anything: it returns (false, false) always. The doc that
// stood here described a force-stop — and the exercise-churn landing beside it — that both came
// out; the reasoning for their removal is inside the function, where the code is.
//
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
	if act, done := a.requireFinishDeclaration(ctx, tc, usedTools, ts); done {
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

// requireFinishDeclaration keeps a working turn open until the agent SAYS it is finished and the
// council accepts. Ending was previously the absence of an action — the model stopped calling tools
// and the turn stopped with it, so a turn that trailed off mid-thought and a turn that was actually
// done ended identically, and neither was ever asked.
//
// Only for a turn that did work: a conversational reply has nothing to declare, and demanding a
// declaration for "hello" would be a loop with no exit. Only when a council exists to answer, since
// the declaration is made to it. And it does not fire once and give up — going quiet is not a way to
// finish, or the requirement would be a formality any silence could step around.
func (a *App) requireFinishDeclaration(ctx context.Context, tc turnCtx, usedTools bool, ts *turnState) (loopAction, bool) {
	if ts.declared {
		return 0, false // it declared, and the council accepted — that IS the finish
	}
	if !declareFinishEnabled() || !usedTools || a.cfg.Council == nil || a.cfg.Workflow {
		return 0, false
	}
	if _, ok := a.tools.Get("council"); !ok || !tc.agent.allows("council") {
		return 0, false
	}
	// Bounded, because the alternative is a turn that never ends. Asking is worth doing when the
	// agent simply forgot the form; it is worth nothing against an agent that cannot produce it, and
	// that one would hold the session open until the wall clock while looking busy — each reminder
	// answered with a tool call, so "is it still working" says yes forever. After declareAskCap the
	// work lands as it stands, with the reason recorded: the turn ends undeclared, which is a
	// different thing from ending declared and is written down as such.
	if ts.declareAsks >= declareAskCap {
		ts.unverifiedReason = "the agent never declared the task finished, so no council read it — " +
			"the work stands as it was left"
		return 0, false
	}
	ts.declareAsks++
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts: []session.Part{{Kind: session.PartText, Text: "You stopped without saying you are finished. " +
			"A turn ends by declaring it: call the `council` tool with `complete: true`, and the council reads " +
			"the record — what actually ran, how it ended, what is on disk now — and either accepts (the turn " +
			"is over) or tells you what is still undone. If the work is finished, declare it now. If it is not, " +
			"keep working." + notesTail(a.turnNotesBlock(tc.s.ID))}},
	})
	a.appendFact(ctx, tc.s.ID, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "orchestrator"}, pd)
	return loopContinue, true
}

// notesTail appends the agent's own turn notes to a finish-seam message, or nothing when it left
// none. Separated so both seams render them identically and neither can quietly drop them.
func notesTail(block string) string {
	if block == "" {
		return ""
	}
	return "\n\n" + block
}

// finishDeclared reports (and consumes) the signal the council tool leaves when the agent declared
// the task finished and the members accepted. It is read where the loop decides whether the step
// produced work, so a declared finish takes the same path a silent one does — the difference is
// that this one was decided.
func (a *App) finishDeclared(sid session.SessionID) bool {
	declared := false
	a.signalTurnControl(sid, func(tc *turnControl) {
		declared = tc.finish
		tc.finish = false
	})
	return declared
}
