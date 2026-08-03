package app

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// councilParams resolves the council roster and tally rule from config, applying the shared
// defaults (DefaultMembers / DefaultRule).
func (a *App) councilParams() ([]council.Member, council.Rule) {
	members := a.cfg.CouncilMembers
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	rule := a.cfg.CouncilRule
	if rule == "" {
		rule = council.DefaultRule
	}
	return members, rule
}

// emitCouncilVerdicts publishes one CouncilVerdict fact per member verdict — the record of what
// each member decided in a round.
func (a *App) emitCouncilVerdicts(ctx context.Context, sid session.SessionID, actor event.Actor, round int, verdicts []council.Verdict) {
	for _, v := range verdicts {
		vd, _ := json.Marshal(event.CouncilVerdictData{
			Round: round, Member: v.Member, Lens: v.Lens, Decision: string(v.Decision),
			Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback,
			Keep: v.Keep, Cite: v.Cite,
		})
		a.appendFact(ctx, sid, event.TypeCouncilVerdict, actor, vd)
	}
}
