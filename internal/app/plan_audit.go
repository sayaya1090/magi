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

// runPlanAuditGate has the council review the PROCEDURE before it runs (D17). It
// returns the procedure to execute — the original (approved or force-approved) or
// a revised one. The pure tally is reused via the council adapter with Phase=plan;
// there is no diff/report/signals, and revise feedback drives a re-plan (it is NOT
// injected into the main session). It has its own bounded rounds.
func (a *App) runPlanAuditGate(ctx context.Context, s session.Session, spec AgentSpec, prompt string, steps []planStep, depth, maxSteps int) []planStep {
	sid := s.ID
	actor := councilSystemActor()
	a.setStage(sid, stageCouncil)
	members := a.cfg.CouncilMembers
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	// Consensus rule — the same one the termination gate uses (no special quorum:1
	// relaxation): the plan audit is a real consensus. planMemberSystem already
	// revises only for a concrete flaw, so majority converges.
	rule := a.cfg.CouncilRule
	if rule == "" {
		rule = council.DefaultRule
	}
	// The plan audit shares the termination gate's round cap (CouncilMaxRounds,
	// default 3) rather than a shorter hardcoded limit: round 1 often surfaces a
	// concrete fix that round 2 still hasn't fully absorbed, so a too-small cap
	// force-proceeds on a plan that one more round would have converged.
	maxRounds := a.cfg.CouncilMaxRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}

	// Ground the audit in the conversation: a follow-up plan ("change it to two
	// newlines") is unjudgeable against the bare instruction alone, so the members
	// thrash (revise → no consensus). Prepend a compact recent transcript.
	auditTask := prompt
	if evs, err := a.store.Read(ctx, sid, 0); err == nil {
		if cx := recentTranscript(reconstruct(evs), 1500); cx != "" {
			auditTask = "# Recent conversation (for context)\n" + cx + "\n\n# Current request to plan for\n" + prompt
		}
	}

	for round := 1; round <= maxRounds; round++ {
		if ctx.Err() != nil {
			return steps
		}
		a.emitCouncilConvened(ctx, sid, actor, round, "plan", members, rule, auditTask, renderSteps(steps))

		// Council deliberation is len(members) sequential side LLM calls with no
		// stream events; on a slow model a round is minutes of silence. Same
		// rationale as runPlanner's progress emit.
		a.emitToolProgress(sid, actor, "", "council",
			fmt.Sprintf("plan audit round %d/%d: %d member(s) deliberating…", round, maxRounds, len(members)))
		delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
			Round: round, Phase: "plan", Task: auditTask, Plan: renderSteps(steps),
			Members: members, Rule: rule, Debate: councilDebateEnabled(), DefaultModel: s.Model.Model,
		})
		if err != nil { // a gate failure must not block the turn → proceed with the plan
			a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{Round: round, Phase: "plan", Decision: string(council.Done), Note: "plan council unavailable: " + err.Error(), Forced: true})
			return steps
		}
		a.emitDebate(sid, actor, "plan", round, delib.Debate)
		a.emitCouncilVerdicts(ctx, sid, actor, round, "plan", delib.Verdicts)

		// Severity-gated decision (D17): only a CRITICAL revision blocks the agent from
		// starting work. warn/info concerns are ACCEPTED as advice — injected so the
		// executor heeds them during the turn, and the council's completion criteria (which
		// the termination gate verifies) still apply — instead of looping the plan, which on
		// a slow model burned the whole budget before any work. critical vetoes (one member
		// suffices) so a genuine plan flaw still stops the agent.
		// The abstract-vs-absurd distinction for refine plans is made by the member LENS,
		// not a blanket code rule: the lens approves an intentionally high-level refine step
		// (abstractness is not a flaw — it is worked out at execution time) but STILL fires
		// critical for a genuinely unsound plan (wrong approach, a required part missing, a
		// plan that would not achieve the task), even when abstract. A deterministic
		// "refine ⇒ never critical" override was rejected: it would also wave through an
		// absurd plan, which must still be rejected.
		critical := council.HasCriticalRevision(delib.Verdicts)
		advice := strings.TrimSpace(council.AdvisoryFeedback(delib.Verdicts))

		if !critical { // approve, possibly carrying non-blocking advice
			a.storePlanCriteria(ctx, s, delib.Criteria)               // the contract for the termination gate
			a.storeCoveredChecks(ctx, s, prompt, steps, delib.Checks) // …plus per-step deliverable checks, coverage-filled
			note := ""
			if advice != "" {
				a.injectCouncilAdvice(ctx, s.ID, advice, true) // accepted: the executor heeds it
				note = "plan approved with advisory notes (non-blocking)"
				if a.cfg.CouncilPlanAbsorb { // option B: fold the advice into the plan now
					if next := sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, advice, depth, maxSteps, "")); len(next) > 0 {
						steps = next
					}
				}
			}
			a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done),
				Tally: delib.Breakdown, Note: note, Criteria: delib.Criteria,
			})
			return steps
		}

		// critical → block. Stop if the cap is hit or there is no actionable feedback.
		fb := strings.TrimSpace(council.CriticalFeedback(delib.Verdicts))
		if round >= maxRounds || fb == "" {
			a.storePlanCriteria(ctx, s, delib.Criteria) // proceeding with this plan → keep its criteria
			a.storeCoveredChecks(ctx, s, prompt, steps, delib.Checks)
			// Proceeding PAST an unresolved critical: hand the executor that critical
			// concern (plus any advice) so it can still try to address it — don't bury it
			// in a note only.
			if carry := strings.TrimSpace(fb + "\n\n" + advice); carry != "" {
				a.injectCouncilAdvice(ctx, s.ID, carry, false)
			}
			a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done), Tally: delib.Breakdown,
				Note: fmt.Sprintf("critical plan concern unresolved after %d round(s) — proceeding", round), Criteria: delib.Criteria, Forced: true,
			})
			return steps
		}
		a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{Round: round, Phase: "plan", Decision: string(council.Continue), Tally: delib.Breakdown, Feedback: fb})

		// Re-plan with the blocking feedback folded in (one retry — local models are flaky
		// and an empty/unparseable reply shouldn't silently drop the revision). Carry the
		// members' `keep` — the steps some lens already blessed — as ADVICE, so a revision
		// triggered by one member's critical flaw doesn't discard the parts the others approved.
		// Advisory, never a constraint: if fixing the flaw genuinely requires changing a kept
		// step, the re-planner is free to.
		revise := fb
		if councilKeepEnabled() {
			if keep := strings.TrimSpace(council.AggregateKeep(delib.Verdicts)); keep != "" {
				revise = fb + "\n\nAlready sound through some lens — PREFER to preserve these, but this is " +
					"advice, not a rule: change them if the fix truly requires it.\n" + keep
			}
		}
		a.setStage(sid, stagePlan)
		next := sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, revise, depth, maxSteps, ""))
		if len(next) == 0 {
			next = sanitizeSteps(a.runPlanner(ctx, spec, s, prompt, revise, depth, maxSteps, ""))
		}
		a.setStage(sid, stageCouncil)
		if len(next) == 0 {
			// Re-plan failed → proceed with the prior plan, but say so (don't silently
			// run a plan the council just rejected). Keep this round's criteria.
			a.storePlanCriteria(ctx, s, delib.Criteria)
			a.storeCoveredChecks(ctx, s, prompt, steps, delib.Checks)
			a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done), Tally: delib.Breakdown,
				Note: "re-plan failed — proceeding with the prior plan", Criteria: delib.Criteria, Forced: true,
			})
			return steps
		}

		// Plan-audit convergence (D17): judge whether the revision actually engaged the
		// council's critical concern. A productive revision (addressed) keeps looping to the
		// cap as before; an unproductive one (ignored the concern) stops early — re-planning
		// again tends to repeat the same conclusion, so hand the concern to the executor and
		// let the execution + landing gates arbitrate ("run it to know", the plan-side symmetry
		// of the evidence gate) instead of burning rounds. The before/after diff + critique +
		// verdict are always emitted as a PlanRevised fact, so the revision is observable even
		// when gating is off (Addressed nil then).
		var addressed *bool
		reason := ""
		if planConvergeEnabled() {
			a.emitToolProgress(sid, actor, "", "council", "judging whether the revision addressed the concern…")
			v, jerr := a.cfg.Council.JudgeRevision(ctx, port.RevisionJudgeRequest{
				Critique: fb, PriorPlan: renderSteps(steps), RevisedPlan: renderSteps(next), DefaultModel: s.Model.Model,
			})
			if jerr != nil { // fail open — a flaky judge must never cut a productive loop
				v = port.RevisionVerdict{Addressed: true, Reason: "revision judge error: " + jerr.Error()}
			}
			ok := v.Addressed
			addressed = &ok
			reason = v.Reason
		}
		pr, _ := json.Marshal(event.PlanRevisedData{
			Round: round, Critique: fb, Before: stepSummaries(steps), After: stepSummaries(next),
			Addressed: addressed, Reason: reason,
		})
		a.appendFact(ctx, sid, event.TypePlanRevised, actor, pr)

		if addressed != nil && !*addressed {
			// Unproductive re-plan → stop early. Proceed with the revised plan but hand the
			// executor the unaddressed concern (execution + landing gates arbitrate).
			a.storePlanCriteria(ctx, s, delib.Criteria)
			a.storeCoveredChecks(ctx, s, prompt, next, delib.Checks)
			a.injectCouncilAdvice(ctx, s.ID, fb, false)
			a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{
				Round: round, Phase: "plan", Decision: string(council.Done), Tally: delib.Breakdown,
				Note:     fmt.Sprintf("plan revision did not address the concern after %d round(s) — proceeding (execution + landing gates arbitrate)", round),
				Criteria: delib.Criteria, Forced: true,
			})
			return next
		}
		steps = next
	}
	return steps
}
