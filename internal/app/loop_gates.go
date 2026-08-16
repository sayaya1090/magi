package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
		"step. If those calls were FAILING, read the error and check paths/state before retrying. If " +
		"they were SUCCEEDING and handing back the same answer each time, that answer is not going to " +
		"change by asking again — act on what it already told you, or find the fact you actually need " +
		"somewhere else. Re-read the original task:\n" +
		clipSpec(task, 1500)
	if kind == "stalled" && tc.guard.stallNudges > 1 {
		// The repeats shrink. The full nudge below is ~1.3KB plus the task re-pasted at 1500 bytes,
		// persisted as a user-role message — and it re-arms every no-progress window with no cap, so
		// a truly stuck run stacked byte-identical copies (observed: one was ignored 57 times).
		// The Nth identical copy adds nothing and dilutes the attention the tool results need; after
		// the first, a one-liner keeps the record honest without repeating the sermon.
		msg = "Still no concrete progress since the last note — same advice stands: finish via the " +
			"`council` tool if the work is complete, otherwise take a DIFFERENT concrete action or say " +
			"exactly what is blocking you."
	} else if kind == "stalled" {
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

// turnTaskOr returns the tracked turnTask, falling back to re-deriving the task from the
// event log when it is empty — the shared fallback of the re-ground, stuck-recovery, and
// idle-resubmit paths, so they can't drift apart (this was copy-pasted four times).
func (a *App) turnTaskOr(turnTask string, sid session.SessionID, evs []event.Event) string {
	if task := strings.TrimSpace(turnTask); task != "" {
		return task
	}
	return taskSeedText(a.taskEvents(sid, evs))
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
	if act, done := a.sayWhatWasNotRun(ctx, tc, ts); done {
		return act
	}
	if act, done := a.noteOutstandingHandoffs(ctx, tc, ts); done {
		return act
	}
	if act, done := a.askWhatTheAnswersWereWorth(ctx, tc, evs, ts); done {
		return act
	}
	// Accepted. Before the turn closes, ask what was worth keeping — the one moment where the
	// answer is both knowable and cheap, because the whole turn is still in context. Off by
	// default; see distil.go.
	if ts.declared && usedTools && a.askToDistil(ctx, tc, ts) {
		return loopContinue
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
		// A message folded into this turn at its start was answered BY this turn. Say so, or its
		// bubble keeps the waiting glyph and sits where it was typed, looking like a request no
		// one ever got to — the display layer has no other way to learn it was handled.
		for _, id := range a.takeFolded(tc.s.ID) {
			ad, _ := json.Marshal(event.InterjectionAnsweredData{MessageID: id})
			a.appendFact(ctx, tc.s.ID, event.TypeInterjectionAnswered, event.Actor{Kind: event.ActorSystem, ID: "interject"}, ad)
		}
	}
	// A finish no council ever read (the agent never declared, past declareAskCap) lands as
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
	// A CHILD never convenes one. The council is the gate on ending the USER's turn — it reads the
	// user's task, the plan, and a fresh look at the workspace, and judges whether the work is
	// done. A child answers to the tool that spawned it and to the parent turn that call belongs
	// to, and that parent has its own declaration to make. The machinery that came out of this
	// tree carried the same guard ("a leaf subagent runs at depth>0 and never convenes a council");
	// it went with the last thing that ran at depth>0, and nothing has since.
	if tc.depth != 0 {
		return 0, false
	}
	if _, ok := a.tools.Get("council"); !ok || !tc.agent.allows("council") {
		return 0, false
	}
	// The budget is per STRETCH of no progress, not per turn. It counted for the whole turn and
	// never reset, so three quiet moments an hour apart — with real work between them — spent the
	// same budget as an agent stuck in place, and the turn ended on the third as though nothing had
	// happened since the first. A mutation since the last ask is the evidence that the reminder was
	// answered by working, so the count starts over.
	//
	// The epoch is the right signal and a tool call is not: an agent that cannot produce the
	// declaration answers every reminder WITH a tool call, so counting calls would make the budget
	// infinite for exactly the case it exists to bound. The epoch rises only on a real file
	// mutation, which that agent never manages.
	if epoch := tc.guard.mutationEpoch(); ts.declareAsks > 0 && epoch != ts.declareAskEpoch {
		ts.declareAsks = 0
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
	ts.declareAskEpoch = tc.guard.mutationEpoch()
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
// the task finished and the members accepted — or the rejection cap landed the turn. It adopts the
// signal into ts, carrying the cap's UNVERIFIED reason with it: consuming the flag here and leaving
// the reason to a later drain lost it, because the drain empties fields this already took. It is
// read where the loop decides whether the step produced work, so a declared finish takes the same
// path a silent one does — the difference is that this one was decided.
func (a *App) finishDeclared(ts *turnState, sid session.SessionID) bool {
	if !ts.declared {
		a.signalTurnControl(sid, func(tc *turnControl) {
			if tc.finish {
				ts.declared = true
				if tc.unverifiedReason != "" {
					ts.unverifiedReason = tc.unverifiedReason
				}
			}
			tc.finish, tc.unverifiedReason = false, ""
		})
	}
	return ts.declared
}

// askWhatTheAnswersWereWorth asks a finishing turn to judge the answers other companions gave it.
//
// # Here, because here is the only place it can be answered
//
// Whether an answer was the one needed is a judgement about content, and it needs the question,
// the answer, and the work they were both for. That is exactly what a turn has and nothing else
// does — not the wait that delivered it, not a later turn reading the log, not a person. The same
// argument the distil gate makes about what was worth keeping, at the same moment and for the same
// reason: the whole turn is still in context, so the answer is both knowable and cheap.
//
// # A note, not a gate
//
// Asked once and never again. A turn that ignores it finishes; no verdict is not a bad verdict,
// and a nag that repeats until it gets one would collect ratings written to make it stop, which is
// worse than no record at all.
//
// # Already judged is not asked about
//
// The turn's own tool calls are the evidence. A companion the agent has already rated in this turn
// is dropped, so an agent that does this unprompted is never told to do what it just did — the
// shape of a system that asks for something it cannot see it already has.
//
// Top level only, like the note above it: a child has no Expect, so nothing can have come back.
func (a *App) askWhatTheAnswersWereWorth(ctx context.Context, tc turnCtx, evs []event.Event,
	ts *turnState) (loopAction, bool) {

	if ts.ratingAsked || tc.depth != 0 {
		return 0, false
	}
	got := a.takeAnsweredHandoffs(tc.s.ID)
	if len(got) == 0 {
		return 0, false
	}
	ts.ratingAsked = true
	// The finish path is about to ask for a tool, so the finish path must let that tool run. Its
	// calls are dropped once a turn declares itself done, and without this the agent did exactly
	// what it was told and nothing happened.
	ts.allowAtFinish("rate_handoff")
	rated := ratedThisTurn(evs)
	var ask []answeredHandoff
	for _, h := range got {
		if !rated[strings.ToLower(strings.TrimSpace(h.Who))] {
			ask = append(ask, h)
		}
	}
	if len(ask) == 0 {
		return 0, false
	}
	var b strings.Builder
	b.WriteString("Before you finish: you got an answer back from somebody, and nothing but this " +
		"turn knows whether it was the answer you needed.\n")
	for _, h := range ask {
		fmt.Fprintf(&b, "\n  %s (%s) — you asked: %s",
			h.Who, h.Receipt, clipLine(oneLine(h.Request), 160))
	}
	b.WriteString("\n\nCall `rate_handoff` once for each, naming it by the receipt in brackets so " +
		"two answers from one companion are told apart, and judging the ANSWER and not whether it " +
		"arrived. It is a record for whoever chooses next, changes nothing about who work goes " +
		"to, and ranks nobody. If you would rather not judge it, say so and finish — an unrated " +
		"hand-off is not a bad one, and you will not be asked again.")
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: b.String()}},
	})
	a.appendFact(ctx, tc.s.ID, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "handoff"}, pd)
	return loopContinue, true
}

// ratedThisTurn is who the agent has already judged, read off its own tool calls.
func ratedThisTurn(evs []event.Event) map[string]bool {
	out := map[string]bool{}
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			continue
		}
		tc := d.Part.ToolCall
		if d.Part.Kind != session.PartToolCall || tc == nil || tc.Name != "rate_handoff" {
			continue
		}
		var args struct {
			Who string `json:"who"`
		}
		if json.Unmarshal(tc.Args, &args) != nil {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(args.Who))] = true
	}
	return out
}

// maxReasks is how many times a turn may ask somebody again after declaring itself finished.
//
// Not zero, because the case is real and was watched: shown an answer that came back empty, the
// agent immediately re-delegated with a sharper request. That is the right move and it was being
// thrown away. Not unbounded, because the reason calls are dropped after a declaration is that a
// turn has to end — two is enough for "that was not it, here is what I actually need" and few
// enough that it cannot become a loop.
const maxReasks = 2

// callsAfterDeclaring decides what a turn that has declared itself finished may still do.
//
// Three answers, and each is a different fact about the call:
//
//   - A tool the FINISH PATH asked for runs. Rating a hand-off, saving a lesson: magi asked for it
//     one step earlier, and asking for something and then discarding it is the defect this whole
//     seam exists to stop.
//   - Asking somebody AGAIN runs, up to maxReasks, and REOPENS the turn. A re-ask is an assertion
//     that the work is not done; allowed while the turn stays closed it would be pointless, since
//     the answer would arrive after the turn ended and land where nothing reads it.
//   - Anything else is dropped, and said so by sayWhatWasNotRun.
func (a *App) callsAfterDeclaring(ctx context.Context, sid session.SessionID,
	calls []*session.ToolCall, ts *turnState) []*session.ToolCall {

	kept := make([]*session.ToolCall, 0, len(calls))
	reopened := 0
	for _, c := range calls {
		switch {
		case c == nil:
		case ts.finishTools[c.Name]:
			kept = append(kept, c)
		case c.Name == "hand_off" && ts.reasks < maxReasks:
			ts.reasks++
			ts.declared = false
			reopened = ts.reasks
			kept = append(kept, c)
		default:
			ts.dropped = append(ts.dropped, c.Name)
		}
	}
	if reopened > 0 {
		pd, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: "m_" + newID(),
			Parts: []session.Part{{Kind: session.PartText, Text: fmt.Sprintf(
				"You asked somebody again after declaring this turn finished, so the turn is NOT "+
					"finished any more — it is open, which is the only way their answer can land "+
					"in it. That is %d of %d times you may do this. Carry on until it comes back, "+
					"then declare again: the council will read the record afresh.",
				reopened, maxReasks)}},
		})
		a.appendFact(ctx, sid, event.TypePromptSubmitted,
			event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
	}
	return kept
}

// sayWhatWasNotRun tells a declared turn that the tools it just called were not run.
//
// A turn that has declared itself finished does no more work on the task, so its calls are dropped.
// That is the rule and it stays. What was wrong was doing it in silence: the agent asked for
// something, nothing happened, and the transcript kept the call with no result — which is exactly
// what a call that DID happen looks like to whoever reads it next, including this agent on a
// resumed session.
//
// Observed live and unprompted: shown an answer that came back empty, the agent immediately
// re-delegated with a sharper request. It was the right move. It never left the process, and
// nothing said so.
//
// Once, and it keeps the turn open so the answer can be acted on: "not finished after all" is a
// thing an agent can still say here, and it is the only way back.
func (a *App) sayWhatWasNotRun(ctx context.Context, tc turnCtx, ts *turnState) (loopAction, bool) {
	if ts.dropTold || len(ts.dropped) == 0 {
		return 0, false
	}
	ts.dropTold = true
	names := ts.dropped
	ts.dropped = nil
	var b strings.Builder
	fmt.Fprintf(&b, "You called %s, and this turn had already declared itself finished — so it "+
		"was not run. Once the council has accepted, a turn does no more work on the task. The "+
		"call is in the transcript with no result, which is what a call that never happened looks "+
		"like; nothing crossed and nobody was asked.\n\n", strings.Join(quoted(names), ", "))
	b.WriteString("If the work is NOT finished after all, say that plainly now — it is the only " +
		"way back, and it is a better answer than a call that goes nowhere. If it IS finished, " +
		"ignore this and write your final answer.")
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: b.String()}},
	})
	a.appendFact(ctx, tc.s.ID, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "loop"}, pd)
	return loopContinue, true
}

// quoted renders tool names so a list of one does not read as prose.
func quoted(names []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, "`"+n+"`")
	}
	return out
}

// noteOutstandingHandoffs tells a finishing turn that a companion it asked has not answered.
//
// Once, and it is a note rather than a gate: the agent decides. Handing work over is asynchronous
// precisely so the asker can carry on, and forcing it to sit here until every peer answers would
// turn it back into the blocking call it exists to avoid. But finishing with a piece still out
// throws that piece away — the watch delivers into a turn that has ended, where nothing reads it —
// and the agent is the only thing that can weigh whether the answer was load-bearing.
//
// Top level only. A child has no Expect (see execute.go), so it can have nothing outstanding, and
// asking would be a question with one possible answer.
func (a *App) noteOutstandingHandoffs(ctx context.Context, tc turnCtx, ts *turnState) (loopAction, bool) {
	if ts.handoffTold || tc.depth != 0 {
		return 0, false
	}
	out := a.PendingHandoffs(tc.s.ID)
	if len(out) == 0 {
		return 0, false
	}
	ts.handoffTold = true
	var b strings.Builder
	b.WriteString("You handed work to another companion and it has not come back yet:\n")
	for _, h := range out {
		fmt.Fprintf(&b, "\n  %s — %s (asked %s ago)",
			h.Who, clipLine(oneLine(h.Request), 160), time.Since(h.Since).Round(time.Second))
	}
	b.WriteString("\n\nIf that answer is part of what you were asked for, keep working until it " +
		"arrives — it lands in this conversation on its own and you do not need to ask again, or " +
		"to poll anything. If it is not, finish and say plainly in your answer what is still out " +
		"with whom, so whoever reads this knows a piece is missing.")
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: b.String()}},
	})
	a.appendFact(ctx, tc.s.ID, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "handoff"}, pd)
	return loopContinue, true
}
