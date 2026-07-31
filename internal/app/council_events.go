package app

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// councilParams resolves the council roster, tally rule, and per-turn round cap from config, applying
// the shared defaults (DefaultMembers / DefaultRule / 3). Every council gate — plan audit, contract
// gate, termination gate, substitution review — uses these same fallbacks, so they live here once.
func (a *App) councilParams() ([]council.Member, council.Rule, int) {
	members := a.cfg.CouncilMembers
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	rule := a.cfg.CouncilRule
	if rule == "" {
		rule = council.DefaultRule
	}
	maxRounds := a.cfg.CouncilMaxRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}
	return members, rule, maxRounds
}

// emitCouncilVerdicts publishes one CouncilVerdict fact per member verdict — the standard record
// of what each member decided in a round, shared by the plan-audit and contract gates.
func (a *App) emitCouncilVerdicts(ctx context.Context, sid session.SessionID, actor event.Actor, round int, phase string, verdicts []council.Verdict) {
	for _, v := range verdicts {
		vd, _ := json.Marshal(event.CouncilVerdictData{
			Round: round, Phase: phase, Member: v.Member, Lens: v.Lens, Decision: string(v.Decision),
			Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback, Severity: v.Severity,
			Keep: v.Keep, Cite: v.Cite,
		})
		a.appendFact(ctx, sid, event.TypeCouncilVerdict, actor, vd)
	}
}
