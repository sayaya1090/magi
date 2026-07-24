package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// substReviewEnabled gates the substitution review (MAGI_SUBST_REVIEW). On (default), a check
// substitution declared via the substitute_check tool (or a report) must be vetted by a strict review
// council — in the declaring agent's OWN session, shown in its detail view — before the turn finishes,
// and the agent corrects and re-declares until the council agrees (bounded). Off restores pass-through
// where a substitution is only prose evidence the terminal council later weighs. Applies identically to
// a solo agent and a delegated worker (same finish-boundary review).
func substReviewEnabled() bool { return !envOff("MAGI_SUBST_REVIEW") }

// reviewSubstitutions runs the review council over the substitutions declared this turn (pendingSubs).
// It returns (action, looped): looped=true means the council asked for a correction, so the agent is
// looped (loopContinue) with the concern injected to re-declare a better equivalent; looped=false means
// approved or inapplicable — proceed to finish. On approval the substitutions are applied: a solo agent
// (depth 0) rewrites its OWN session's stored checks immediately; a worker (depth>0) stashes them for
// the parent to apply (its checks live in the parent). The CORRECTION LOOP is the agent re-declaring
// (this runs again on each finish attempt), bounded by *rounds against CouncilMaxRounds; on exceeding
// the budget it drops the unapproved substitutions and proceeds (terminal gate + external verifier are
// the backstop). Unlike the plan/contract phase this review is STRICT — the deliverable is built, so an
// inadequate equivalent is rejected rather than leniently accepted.
func (a *App) reviewSubstitutions(ctx context.Context, tc turnCtx, rounds *int) (loopAction, bool) {
	if !substReviewEnabled() || a.cfg.Council == nil {
		return 0, false
	}
	s := tc.s
	sid := s.ID
	pending := a.pendingSubsOf(sid)
	if len(pending) == 0 {
		return 0, false
	}
	actor := event.Actor{Kind: event.ActorAgent, ID: orDefault(s.Agent, "default")}

	members, rule, maxRounds := a.councilParams()
	if *rounds >= maxRounds { // correction budget spent → drop the unapproved subs and proceed
		a.clearPendingSubs(sid)
		return 0, false
	}

	task := substReviewTask(pending)
	subsText := renderSubs(pending)
	round := *rounds + 1

	a.emitCouncilConvened(ctx, sid, actor, round, "substitution", members, rule, task, subsText)
	a.emitToolProgress(sid, actor, "", "council",
		fmt.Sprintf("substitution review round %d/%d: %d member(s) deliberating…", round, maxRounds, len(members)))

	delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
		Round: round, Phase: "substitution", Task: task, Plan: subsText, Report: subsText,
		Members: members, Rule: rule, Debate: councilDebateEnabled(), DefaultModel: s.Model.Model,
	})
	if err != nil { // a gate failure must never block the agent → approve and proceed
		a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{Round: round, Phase: "substitution", Decision: string(council.Done), Note: "substitution council unavailable: " + err.Error(), Forced: true})
		a.approveSubs(ctx, tc, pending)
		return 0, false
	}
	a.emitDebate(sid, actor, "substitution", round, delib.Debate)
	a.emitCouncilVerdicts(ctx, sid, actor, round, "substitution", delib.Verdicts)

	// Only a CRITICAL concern (an inadequate equivalent) forces a correction; an advisory note does not
	// loop the agent. Approved → apply the substitutions and proceed to finish.
	if !council.HasCriticalRevision(delib.Verdicts) {
		a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{Round: round, Phase: "substitution", Decision: string(council.Done), Tally: delib.Breakdown})
		a.approveSubs(ctx, tc, pending)
		return 0, false
	}
	fb := strings.TrimSpace(council.CriticalFeedback(delib.Verdicts))
	*rounds = round
	a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{Round: round, Phase: "substitution", Decision: string(council.Continue), Tally: delib.Breakdown, Feedback: fb})
	msg := "The council reviewed your acceptance-check substitution(s) and asks you to correct them before finishing:\n" + fb +
		"\n\nRun a better equivalent that verifies the SAME goal as the original check, then declare it again with " +
		"substitute_check (same step/original). If no valid equivalent exists, report status \"blocked\" and say which " +
		"check and why — do not pass off a weak proxy as done."
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + newID(),
		Parts:     []session.Part{{Kind: session.PartText, Text: msg}},
	})
	a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "council"}, pd)
	return loopContinue, true
}

// approveSubs applies review-approved substitutions: a solo/top-level agent (depth 0) rewrites its OWN
// session's stored checks immediately (its session IS the one the terminal gate checks); a delegated
// worker (depth>0) stashes them for the parent's spawn attempt to apply (the checks live in the parent).
// Either way the pending queue is cleared so an already-approved substitution is not re-reviewed.
func (a *App) approveSubs(ctx context.Context, tc turnCtx, subs []port.CheckSub) {
	a.clearPendingSubs(tc.s.ID)
	if tc.depth == 0 {
		a.applyCheckSubs(ctx, tc.s.ID, subs)
		return
	}
	a.stashApprovedSubs(tc.s.ID, subs)
}

// substReviewTask renders what the review council judges: each substitution's step and the original
// command it replaces, plus the strict-equivalence instruction.
func substReviewTask(subs []port.CheckSub) string {
	var b strings.Builder
	b.WriteString("Acceptance-check substitution review. For each step below an agent's given check command could not run " +
		"in this environment and it substituted an equivalent that passed. Judge STRICTLY whether each equivalent verifies " +
		"the SAME goal as the original check — reject a weaker proxy (mere existence/reachability when the original exercised " +
		"behavior). The deliverable is already built, so do NOT be lenient as at plan time; but do not demand more than the " +
		"original check itself required.")
	for _, cs := range subs {
		fmt.Fprintf(&b, "\n- step %s: original check `%s`", strings.TrimSpace(cs.Step), strings.TrimSpace(cs.Original))
	}
	return b.String()
}

// renderSubs renders the declared substitutions for the council to review.
func renderSubs(subs []port.CheckSub) string {
	var b strings.Builder
	for _, cs := range subs {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "- step %s: ran `%s`", strings.TrimSpace(cs.Step), strings.TrimSpace(cs.Command))
		if e := strings.TrimSpace(cs.Expect); e != "" {
			fmt.Fprintf(&b, " (expect %s)", e)
		}
		if why := strings.TrimSpace(cs.Reason); why != "" {
			fmt.Fprintf(&b, " — original `%s` could not run: %s", strings.TrimSpace(cs.Original), why)
		}
	}
	return b.String()
}
