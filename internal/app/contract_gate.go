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

// runContractGate is the contract-first gate (D-contract): BEFORE the planner decomposes the
// request, the council authors and reviews the turn's acceptance CONTRACT — completion criteria
// (prose done-conditions) and executable deliverable checks — for the TASK itself. The contract
// is bounded above by necessity (assert only what the task states) and below by sufficiency
// (exercise the behavior, not its mere existence); the contract member prompt carries both bounds.
//
// It reuses the plan-audit machinery (Phase="contract"): each member proposes+critiques the
// contract, MergeCriteria/MergeChecks synthesize the round's draft, and a CRITICAL revision drives
// one more refining round (bounded by CouncilMaxRounds). On approval — or at the cap — the approved
// criteria are stored and FROZEN so the later plan-audit does not overwrite them, and the checks are
// stashed for the planner to target. The plan is then built to satisfy a reviewed contract rather
// than the contract being a byproduct of whatever plan the planner emitted.
func (a *App) runContractGate(ctx context.Context, s session.Session, prompt string) {
	if !contractFirstEnabled() || a.cfg.Council == nil {
		return
	}
	sid := s.ID
	// Already established this turn: a same-turn re-plan (honorReplan without a new top-level)
	// keeps the reviewed contract; only a genuinely new request (resetForNewTopLevel clears the
	// freeze) re-derives it. Avoids paying an extra council pass on every re-plan.
	a.mu.Lock()
	already := a.stateLocked(sid).contractFrozen
	a.mu.Unlock()
	if already {
		return
	}
	actor := event.Actor{Kind: event.ActorSystem, ID: "council"}
	a.setStage(sid, stageCouncil)

	members := a.cfg.CouncilMembers
	if len(members) == 0 {
		members = council.DefaultMembers()
	}
	labels := make([]string, len(members))
	for i, m := range members {
		labels[i] = m.Name
	}
	rule := a.cfg.CouncilRule
	if rule == "" {
		rule = council.DefaultRule
	}
	maxRounds := a.cfg.CouncilMaxRounds
	if maxRounds <= 0 {
		maxRounds = 3
	}

	// Ground the contract in the conversation, mirroring the plan audit: a follow-up request
	// ("change it to two newlines") is uncontractable against the bare instruction alone.
	task := prompt
	if evs, err := a.store.Read(ctx, sid, 0); err == nil {
		if cx := recentTranscript(reconstruct(evs), 1500); cx != "" {
			task = "# Recent conversation (for context)\n" + cx + "\n\n# Current request to contract for\n" + prompt
		}
	}

	var criteria []string
	var checks []council.DeliverableCheck
	draft := "" // rendered draft carried into the next round for refinement

	for round := 1; round <= maxRounds; round++ {
		if ctx.Err() != nil {
			break
		}
		cd, _ := json.Marshal(event.CouncilConvenedData{
			Round: round, Phase: "contract", Members: labels, Rule: string(rule), Task: task, Plan: draft,
		})
		a.appendFact(ctx, sid, event.TypeCouncilConvened, actor, cd)
		for _, m := range members {
			ld, _ := json.Marshal(event.CouncilDeliberatingData{Round: round, Member: m.Name, State: "asking"})
			a.publishTransient(sid, event.TypeCouncilDeliberating, actor, ld)
		}
		a.emitToolProgress(sid, actor, "", "council",
			fmt.Sprintf("contract gate round %d/%d: %d member(s) deliberating…", round, maxRounds, len(members)))

		delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
			Round: round, Phase: "contract", Task: task, Plan: draft,
			Members: members, Rule: rule, Debate: councilDebateEnabled(), DefaultModel: s.Model.Model,
		})
		if err != nil { // a gate failure must never block the turn → proceed with whatever we have
			dd, _ := json.Marshal(event.CouncilDecidedData{Round: round, Phase: "contract", Decision: string(council.Done), Note: "contract council unavailable: " + err.Error(), Forced: true})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
			break
		}
		a.emitDebate(sid, actor, "contract", round, delib.Debate)
		for _, v := range delib.Verdicts {
			vd, _ := json.Marshal(event.CouncilVerdictData{
				Round: round, Phase: "contract", Member: v.Member, Lens: v.Lens, Decision: string(v.Decision),
				Confidence: v.Confidence, Rationale: v.Rationale, Feedback: v.Feedback, Severity: v.Severity,
			})
			a.appendFact(ctx, sid, event.TypeCouncilVerdict, actor, vd)
		}
		// Keep the round's synthesized contract (the members' merged proposals).
		if len(delib.Criteria) > 0 {
			criteria = delib.Criteria
		}
		if len(delib.Checks) > 0 {
			checks = delib.Checks
		}

		critical := council.HasCriticalRevision(delib.Verdicts)
		if !critical {
			dd, _ := json.Marshal(event.CouncilDecidedData{
				Round: round, Phase: "contract", Decision: string(council.Done), Tally: delib.Breakdown, Criteria: criteria,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
			break
		}
		fb := strings.TrimSpace(council.CriticalFeedback(delib.Verdicts))
		if round >= maxRounds || fb == "" {
			dd, _ := json.Marshal(event.CouncilDecidedData{
				Round: round, Phase: "contract", Decision: string(council.Done), Tally: delib.Breakdown,
				Note: fmt.Sprintf("contract concern unresolved after %d round(s) — proceeding", round), Criteria: criteria, Forced: true,
			})
			a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
			break
		}
		dd, _ := json.Marshal(event.CouncilDecidedData{Round: round, Phase: "contract", Decision: string(council.Continue), Tally: delib.Breakdown, Feedback: fb})
		a.appendFact(ctx, sid, event.TypeCouncilDecided, actor, dd)
		// Refine: carry the current draft plus the blocking concern into the next round.
		draft = renderContract(criteria, checks)
		if draft != "" {
			draft += "\n\n# Council concern to fix:\n" + fb
		} else {
			draft = "# Council concern to address:\n" + fb
		}
	}

	if len(criteria) == 0 && len(checks) == 0 {
		return // nothing usable → leave the opt-in criteria path intact, no freeze
	}
	// Store criteria FIRST (before the freeze so this write is not blocked), then freeze so the
	// plan-audit cannot overwrite the reviewed contract, and stash the checks for the planner.
	a.storePlanCriteria(ctx, s, criteria)
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.contractFrozen = true
	st.contractChecks = checks
	a.mu.Unlock()
	a.setStage(sid, stagePlan)
}

// renderContract renders the criteria + checks as a compact, human/model-readable block used both
// to carry a draft between contract rounds and to inject the approved contract into the planner.
func renderContract(criteria []string, checks []council.DeliverableCheck) string {
	var b strings.Builder
	if len(criteria) > 0 {
		b.WriteString("Acceptance criteria (each must hold):\n")
		for _, c := range criteria {
			if c = strings.TrimSpace(c); c != "" {
				b.WriteString("- " + c + "\n")
			}
		}
	}
	if len(checks) > 0 {
		b.WriteString("Executable checks (each must pass):\n")
		for _, c := range checks {
			cmd := strings.TrimSpace(c.Command)
			if cmd == "" {
				continue
			}
			line := "- "
			if d := strings.TrimSpace(c.Deliverable); d != "" {
				line += d + " — "
			}
			line += "`" + cmd + "`"
			if e := strings.TrimSpace(c.Expect); e != "" {
				line += " (expect: " + e + ")"
			}
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// contractForPlanner returns the frozen contract rendered for injection into the planner, or ""
// when no contract-first gate ran this turn. The planner appends it so the plan targets a reviewed
// contract instead of an unbounded interpretation of the request.
func (a *App) contractForPlanner(sid session.SessionID) string {
	a.mu.Lock()
	st := a.stateLocked(sid)
	frozen, crit, checks := st.contractFrozen, st.criteria, append([]council.DeliverableCheck(nil), st.contractChecks...)
	a.mu.Unlock()
	if !frozen {
		return ""
	}
	var criteria []string
	if strings.TrimSpace(crit) != "" {
		for _, ln := range strings.Split(crit, "\n") {
			criteria = append(criteria, strings.TrimPrefix(strings.TrimSpace(ln), "- "))
		}
	}
	return renderContract(criteria, checks)
}
