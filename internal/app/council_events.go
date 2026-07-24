package app

import (
	"context"
	"encoding/json"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// councilSystemActor is the actor every council-gate event is attributed to.
func councilSystemActor() event.Actor { return event.Actor{Kind: event.ActorSystem, ID: "council"} }

// emitCouncilConvened publishes the standard "a council round is starting" events: the convened
// fact (round/phase/members/rule/task/plan) and a transient "deliberating" ping per member. It is
// the shared preamble of every council gate round (plan audit, contract gate), extracted so the
// gate bodies carry only their own decision logic. Member labels are derived here, so callers no
// longer maintain a parallel labels slice.
func (a *App) emitCouncilConvened(ctx context.Context, sid session.SessionID, actor event.Actor, round int, phase string, members []council.Member, rule council.Rule, task, plan string) {
	labels := make([]string, len(members))
	for i, m := range members {
		labels[i] = m.Name
	}
	cd, _ := json.Marshal(event.CouncilConvenedData{
		Round: round, Phase: phase, Members: labels, Rule: string(rule), Task: task, Plan: plan,
	})
	a.appendFact(ctx, sid, event.TypeCouncilConvened, actor, cd)
	for _, m := range members {
		ld, _ := json.Marshal(event.CouncilDeliberatingData{Round: round, Member: m.Name, State: "asking"})
		a.publishTransient(sid, event.TypeCouncilDeliberating, actor, ld)
	}
}

// emitCouncilVerdicts publishes one CouncilVerdict fact per member verdict — the standard record
// of what each member decided in a round, shared by the plan-audit and contract gates.
func (a *App) emitCouncilVerdicts(ctx context.Context, sid session.SessionID, actor event.Actor, round int, phase string, verdicts []council.Verdict) {
	for _, v := range verdicts {
		vd, _ := json.Marshal(event.CouncilVerdictData{
			Round: round, Phase: phase, Member: v.Member, Lens: v.Lens, Decision: string(v.Decision),
			Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback, Severity: v.Severity,
		})
		a.appendFact(ctx, sid, event.TypeCouncilVerdict, actor, vd)
	}
}

// emitCouncilDecided publishes a round's decision fact (done/continue, tally, note, criteria …).
// Every council gate — plan audit, contract, and the termination gate — closes a round by
// marshalling a CouncilDecidedData and appending it; this folds that two-line boilerplate into one
// call so the gate bodies read as decisions, not serialization.
func (a *App) emitCouncilDecided(ctx context.Context, sid session.SessionID, actor event.Actor, data event.CouncilDecidedData) {
	dd, _ := json.Marshal(data)
	a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
}
