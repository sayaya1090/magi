package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// When magi's counters decide to speak mid-turn, ask the council what to say.
//
// The stall and repeat nudges are fixed paragraphs. They are the same words on every run, and the
// repeat one HEDGES — "if those calls were FAILING … if they were SUCCEEDING …" — because magi
// cannot tell which it is looking at. The hedge is not a style choice: the unhedged version
// asserted a failure a run never had, and naming both shapes was the fix. A reader of the actual
// record does not have to hedge.
//
// So the trigger is left exactly as it was — no new condition, no new counter — and only the
// SPEECH changes. This cannot block, stop, or end anything: it produces the text of a nudge that
// was going to be emitted anyway, and when the members say the run is on track it produces
// nothing and the turn goes on in silence.
//
// Cost is bounded by the trigger it borrows: across 121 recorded sessions the two nudges fired 87
// times, 36% of sessions, which is 2.2 council rounds per session at the very most — not a poll.
//
// MAGI_INTERVENTION_COUNCIL=1 turns it on; default off until an A/B says otherwise.
func interventionCouncilEnabled() bool {
	v := strings.TrimSpace(os.Getenv("MAGI_INTERVENTION_COUNCIL"))
	return v == "1" || strings.EqualFold(v, "on") || strings.EqualFold(v, "true")
}

// interventionAdvice convenes the council on the CURRENT state of a turn and returns what to tell
// the agent, or "" when there is nothing to say — the members judged it on track, the council is
// unavailable, or it could not be reached. Every "" falls back to the fixed paragraph, so a
// backend that is down costs the run nothing but the wait.
func (a *App) interventionAdvice(ctx context.Context, s session.Session, task string, evs []event.Event, guard *runGuard) string {
	if !interventionCouncilEnabled() || a.cfg.Council == nil {
		return ""
	}
	sid := s.ID
	members, rule, _ := a.councilParams()
	actor := event.Actor{Kind: event.ActorSystem, ID: "council"}

	// The same record the finish gate is shown, assembled the same way: magi's own log of what
	// ran first, then a fresh look at the world, then what this council already objected to. The
	// difference is the question, not the evidence.
	actions := turnToolEvidence(evs, councilActionsCap)
	if rec := a.stopRecord(ctx, sid); rec != "" {
		actions = joinBlocks(rec, actions)
	}
	if snap := a.worldDiffFor(sid, s.Workdir, lastUserPromptTS(evs)); snap != "" {
		actions = joinBlocks(snap, actions)
	}
	if prior := priorCouncilObjections(evs, priorObjectionsCap, councilActionCap); prior != "" {
		actions = joinBlocks(prior, actions)
	}
	changes := truncateForCouncil(buildCouncilChanges(guardChanges(guard)), councilDiffCap)

	labels := make([]string, len(members))
	for i, m := range members {
		labels[i] = m.Name
	}
	cd, _ := json.Marshal(event.CouncilConvenedData{
		Round: 1, Members: labels, Rule: string(rule), Task: task,
		Report: lastTurnAssistantText(evs), Actions: clipEvidenceForRecord(actions, councilDiffCap),
		Changes: changes, NoChanges: strings.TrimSpace(changes) == "",
	})
	a.appendFact(ctx, sid, event.TypeCouncilConvened, actor, cd)

	delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
		Round: 1, Phase: port.PhaseIntervention, Task: task,
		Report: lastTurnAssistantText(evs), Actions: actions, Changes: changes,
		NoChanges: strings.TrimSpace(changes) == "",
		Members:   members, Rule: rule, DefaultModel: s.Model.Model,
	})
	if err != nil {
		return "" // unreachable backend → the fixed paragraph, same as before this existed
	}
	a.emitCouncilVerdicts(ctx, sid, actor, 1, "", delib.Verdicts)
	dd, _ := json.Marshal(event.CouncilDecidedData{
		Round: 1, Decision: string(delib.Decision), Tally: delib.Breakdown, Feedback: delib.Feedback,
		Note: "asked mid-turn whether this needs redirecting — it does not end or block anything",
	})
	a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)

	if delib.Decision == council.Done {
		return "" // on track through every lens: say nothing rather than interrupt working code
	}
	return strings.TrimSpace(renderCouncilAdvice(delib,
		"magi's counters flagged this turn as circling, so the council read the record. This is their "+
			"reading, not a decision — the turn is not being stopped and nothing here is a refusal."))
}

// joinBlocks puts b under a with a blank line between, skipping either when empty. The evidence
// assembly did this inline in three places and each copy had its own empty-string handling.
func joinBlocks(a, b string) string {
	switch {
	case strings.TrimSpace(a) == "":
		return b
	case strings.TrimSpace(b) == "":
		return a
	}
	return a + "\n\n" + b
}
