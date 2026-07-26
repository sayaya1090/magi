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
// checkCommandUnrunnable reports whether an exit code / output shows the CHECK COMMAND ITSELF could not
// run in this environment (a missing tool, a non-executable file, a bad interpreter path) — as opposed
// to the command running and the deliverable failing. Beyond 127 (command not found) it covers 126
// (found but not executable / permission denied) and the shell not-found signatures some wrappers
// surface under a different code. Deliberately narrow — a command that RAN and returned a normal failure
// (exit 1, an assertion, an expect mismatch) is NOT unrunnable; that is a real deliverable failure.
func checkCommandUnrunnable(out string, code int) bool {
	if code == 127 || code == 126 {
		return true
	}
	if code == 0 {
		return false // it RAN and succeeded — definitely runnable, whatever the output text says
	}
	low := strings.ToLower(out)
	// A non-zero exit MAY be a wrapper surfacing a not-found under a different code: only the
	// command-level shell signatures ("<name>: not found", "command not found", "executable file not
	// found") — not "no such file"/"permission denied" alone, which a running command can emit about
	// its ARGUMENTS (a deliverable failure, not an unrunnable check). Guarded by code!=0 above so a
	// command that ran fine but happened to print "…: not found" in its output is never misread.
	return strings.Contains(low, "command not found") ||
		strings.Contains(low, ": not found") ||
		strings.Contains(low, "executable file not found")
}

// filterSubsByNecessity runs each substitution's ORIGINAL check command and classifies whether the
// substitution is warranted: justified = the original genuinely cannot run here (a real substitution
// case → the council reviews it); ranFailed = the original RAN and FAILED (a real deliverable failure,
// NOT a broken check — must not be substituted away). A substitution whose original RAN and PASSED is
// dropped (unneeded — the check works, keep it). Platform-gone (-1) or a missing original is treated as
// justified so the council still judges rather than the guard silently dropping it.
func (a *App) filterSubsByNecessity(ctx context.Context, s session.Session, subs []port.CheckSub) (justified, ranFailed []port.CheckSub) {
	if a.plat == nil {
		return subs, nil
	}
	checks := a.cachedChecks(s.ID)
	for _, sub := range subs {
		orig := strings.TrimSpace(sub.Original)
		if orig == "" {
			justified = append(justified, sub)
			continue
		}
		out, code := a.runVerifyCmd(ctx, s.Workdir, orig)
		if code == -1 || checkCommandUnrunnable(out, code) {
			justified = append(justified, sub)
			continue
		}
		// The original RAN. Judge pass by the matching stored check's own test (its Expect matters) when
		// we can find it, else exit 0. A pass means the substitution is unneeded (dropped); a fail means a
		// real deliverable failure the agent must fix, not substitute.
		pass := code == 0
		for _, c := range checks {
			if strings.TrimSpace(c.Step) == strings.TrimSpace(sub.Step) && strings.TrimSpace(c.Command) == orig {
				pass = c.Passes(out, code)
				break
			}
		}
		if !pass {
			ranFailed = append(ranFailed, sub)
		}
	}
	return justified, ranFailed
}

func (a *App) reviewSubstitutions(ctx context.Context, tc turnCtx, rounds *int, critique *string) (loopAction, bool) {
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

	// Deterministic necessity guard (before the council): a substitution is only warranted when the
	// ORIGINAL check command genuinely cannot run here. Run each original and split three ways — an
	// original that RAN and PASSED means the substitution is unneeded (dropped, keep the working check);
	// one that RAN and FAILED is a real deliverable failure the agent must FIX, not substitute away.
	justified, ranFailed := a.filterSubsByNecessity(ctx, s, pending)
	if len(ranFailed) > 0 {
		// Refuse the failure-masking subs (their failing original stays and drives the gate/re-plan), but
		// KEEP any genuinely-justified subs pending so a mixed report doesn't lose them — they are reviewed
		// on the next finish attempt after the agent addresses the real failure.
		a.setPendingSubs(sid, justified)
		*rounds = *rounds + 1
		a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{Round: *rounds, Phase: "substitution",
			Decision: string(council.Continue), Note: "substitution refused — the original check RAN and FAILED (a real deliverable failure, not a broken check)"})
		msg := "Do NOT substitute these acceptance checks — their ORIGINAL command RAN here and FAILED, which is a " +
			"real failure of the DELIVERABLE, not a check that could not run:\n" + renderSubs(ranFailed) +
			"\nFix the deliverable so the original check passes, or report status blocked/failed with why. " +
			"substitute_check is ONLY for a check whose command cannot RUN at all (not found / exit 127)."
		pd, _ := json.Marshal(event.PromptSubmittedData{MessageID: "m_" + newID(), Parts: []session.Part{{Kind: session.PartText, Text: msg}}})
		a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "council"}, pd)
		return loopContinue, true
	}
	if len(justified) == 0 { // every original ran and passed → substitutions unneeded, originals kept
		a.clearPendingSubs(sid)
		return 0, false
	}
	// Only genuinely-unrunnable substitutions reach the council, which judges whether each equivalent
	// actually verifies the goal (the adequacy lens) before it is applied.
	pending = justified

	task := substReviewTask(pending)
	subsText := renderSubs(pending)
	round := *rounds + 1

	a.emitCouncilConvened(ctx, sid, actor, round, "substitution", members, rule, task, subsText)
	a.emitToolProgress(sid, actor, "", "council",
		fmt.Sprintf("substitution review round %d/%d: %d member(s) deliberating…", round, maxRounds, len(members)))

	delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
		Round: round, Phase: "substitution", Task: task, Plan: subsText, Report: subsText,
		Revision: substRevisionContext(*critique),
		Members:  members, Rule: rule, Debate: councilDebateEnabled(), DefaultModel: s.Model.Model,
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
	*critique = fb // the next round must judge whether THIS objection was met
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

// substRevisionContext hands the next substitution round the objection the PREVIOUS round raised,
// so a member judges whether its own concern was met instead of reviewing a re-declared
// substitution cold. Empty on the first round, which has nothing to remember.
func substRevisionContext(prior string) string {
	prior = strings.TrimSpace(prior)
	if prior == "" {
		return ""
	}
	return "The previous round REJECTED the substitution with this objection:\n" + clipSpec(prior, 1200)
}
