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

// contractDraftSystem drafts an initial acceptance contract for the council to review. Same
// necessity/sufficiency bounds the contract members apply — kept short here since the council
// refines it. Reply is a single JSON object.
const contractDraftSystem = "You draft a coding task's ACCEPTANCE CONTRACT — the completion " +
	"conditions a council will then review and refine. Produce two lists: `criteria` (prose " +
	"done-conditions — what must be TRUE for the task to count as finished) and `checks` " +
	"(executable verifications — a shell `command` from the task's working directory, optional " +
	"`expect` regexp its output must match). BOUND IT: assert ONLY what the task itself states " +
	"(never a version/path/value the task did not require); and where the deliverable must DO " +
	"something, a check must EXERCISE that behavior through the same interface its consumer uses and " +
	"assert the outcome (a stub that merely exists must FAIL), replaying the task's own example " +
	"verbatim when it gives one. GOAL, NOT METHOD: a criterion states WHAT must be true, never HOW to " +
	"achieve it — leave the implementation method to the worker; a check verifies the OUTCOME and must " +
	"NOT prescribe a specific tool or build step (it may not exist at run time). Portable commands only " +
	"(coreutils, grep/test, python3, the task's toolchain; never ss/netstat/lsof/pgrep). Keep the " +
	"contract SMALL — a few essential conditions, not an exhaustive list; prefer fewer, higher-value " +
	"items. Reply with ONLY a JSON object, no prose:\n" +
	`{"criteria":["..."],"checks":[{"deliverable":"...","command":"...","expect":"..."}]}`

// elicitContractDraft asks a single model for an initial contract (criteria + checks) so the first
// council round reviews a concrete draft. Best-effort: an unparseable/empty reply yields nil, and the
// council then authors from scratch.
func (a *App) elicitContractDraft(ctx context.Context, spec AgentSpec, sid session.SessionID, model, task string) ([]string, []council.DeliverableCheck) {
	if a.providerFor(spec) == nil { // no model wired (e.g. council-only tests) → skip, council authors
		return nil, nil
	}
	raw := a.specMineCall(ctx, spec, sid, "contract-draft", model, contractDraftSystem, task)
	for _, js := range balancedObjects(raw) {
		var d struct {
			Criteria []string                   `json:"criteria"`
			Checks   []council.DeliverableCheck `json:"checks"`
		}
		if json.Unmarshal([]byte(js), &d) == nil && (len(d.Criteria) > 0 || len(d.Checks) > 0) {
			return d.Criteria, d.Checks
		}
	}
	return nil, nil
}

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
	actor := councilSystemActor()
	a.setStage(sid, stageCouncil)

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

	// Ground the contract in the conversation, mirroring the plan audit: a follow-up request
	// ("change it to two newlines") is uncontractable against the bare instruction alone.
	task := prompt
	if evs, err := a.store.Read(ctx, sid, 0); err == nil {
		if cx := recentTranscript(reconstruct(evs), 1500); cx != "" {
			task = "# Recent conversation (for context)\n" + cx + "\n\n# Current request to contract for\n" + prompt
		}
	}

	agent := a.agentFor(s)
	model := s.Model.Model
	if agent.Model != (session.ModelRef{}) {
		model = agent.Model.Model
	}
	// Draft the contract FIRST (single-model), so the very first council round REVIEWS a concrete
	// draft — shown at convened, before the deliberation — instead of authoring it silently and only
	// revealing it at the decision. Reads like the plan audit (the plan is shown before it is judged).
	// Best-effort: on failure the draft is empty and the council authors from scratch as before.
	criteria, checks := a.elicitContractDraft(ctx, agent, sid, model, task)
	draft := renderContract(criteria, checks)

	for round := 1; round <= maxRounds; round++ {
		if ctx.Err() != nil {
			break
		}
		a.emitCouncilConvened(ctx, sid, actor, round, "contract", members, rule, task, draft)
		a.emitToolProgress(sid, actor, "", "council",
			fmt.Sprintf("contract gate round %d/%d: %d member(s) deliberating…", round, maxRounds, len(members)))

		delib, err := a.cfg.Council.Deliberate(ctx, port.DeliberationRequest{
			Round: round, Phase: "contract", Task: task, Plan: draft,
			Members: members, Rule: rule, Debate: councilDebateEnabled(), DefaultModel: s.Model.Model,
		})
		if err != nil { // a gate failure must never block the turn → proceed with whatever we have
			a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{Round: round, Phase: "contract", Decision: string(council.Done), Note: "contract council unavailable: " + err.Error(), Forced: true})
			break
		}
		a.emitDebate(sid, actor, "contract", round, delib.Debate)
		a.emitCouncilVerdicts(ctx, sid, actor, round, "contract", delib.Verdicts)
		// Seed from the members' proposals ONLY when elicit produced nothing (no draft to review yet);
		// once there is a draft, refinement is by consolidation below, not by re-merging.
		if len(criteria) == 0 && len(checks) == 0 {
			criteria, checks = delib.Criteria, delib.Checks
			draft = renderContract(criteria, checks)
		}
		// NOTE: the contract is the current `criteria`/`checks` (draft), REVISED by consolidation when
		// there is feedback — NOT the union of the members' fresh proposals. Merging the members'
		// proposals every round UNIONS them and only GROWS the contract, so a "reduce to 4" produced
		// the opposite; the members here VOTE and give feedback, and one consolidation applies it.

		// Incorporate ALL actionable feedback — critical AND advisory. The contract is the artifact
		// being DEFINED here, so an advisory suggestion must REFINE it (there is no executor to hand
		// a non-blocking note to, as the plan audit has). Finalize only on a clean round or the cap.
		critical := council.HasCriticalRevision(delib.Verdicts)
		fb := strings.TrimSpace(council.CriticalFeedback(delib.Verdicts))
		if fb == "" {
			fb = strings.TrimSpace(council.AdvisoryFeedback(delib.Verdicts))
		}
		if fb == "" || round >= maxRounds {
			note := ""
			forced := critical && round >= maxRounds // proceeding past an unresolved BLOCKING concern
			if forced {
				note = fmt.Sprintf("contract concern unresolved after %d round(s) — proceeding", round)
			}
			a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{
				Round: round, Phase: "contract", Decision: string(council.Done), Tally: delib.Breakdown, Criteria: criteria, Note: note, Forced: forced,
			})
			break
		}
		a.emitCouncilDecided(ctx, sid, actor, event.CouncilDecidedData{Round: round, Phase: "contract", Decision: string(council.Continue), Tally: delib.Breakdown, Feedback: fb})
		// APPLY the feedback to the current contract by CONSOLIDATION (a REPLACE, not a union): the
		// revised contract is a RESULT of the feedback, so "reduce to 4" reduces. Best-effort — a failed
		// consolidation keeps the current contract and carries the feedback in the draft as a fallback.
		if nc, nk, ok := a.consolidateContract(ctx, agent, sid, model, task, criteria, checks, fb); ok {
			criteria, checks = nc, nk
			draft = renderContract(criteria, checks)
		} else {
			draft = strings.TrimSpace(renderContract(criteria, checks) + "\n\n# Council feedback to incorporate:\n" + fb)
		}
	}

	if len(criteria) == 0 && len(checks) == 0 {
		return // nothing usable → leave the opt-in criteria path intact, no freeze
	}
	// Store the reviewed contract as this turn's actual verification set BEFORE the freeze (so these
	// writes are not blocked): the criteria for the termination gate, and the CHECKS as the
	// deliverable checks the step/finish gate executes and the panel displays — so the completion
	// conditions the user sees and the run is verified against ARE the contract's own (facts +
	// commands), not a separate set the plan-audit re-authored. Then freeze so the plan-audit does
	// not overwrite the reviewed contract.
	a.storePlanCriteria(ctx, s, criteria)
	a.storePlanChecks(ctx, s, checks)
	a.mu.Lock()
	st := a.stateLocked(sid)
	st.contractFrozen = true
	st.contractChecks = checks
	st.contractText = renderContract(criteria, checks)
	a.mu.Unlock()
	a.setStage(sid, stagePlan)
}

// consolidateContractSystem revises a contract by APPLYING the council's feedback and nothing more —
// a REPLACE, so a "reduce to N" reduces (the old flow re-merged the members' proposals and only grew).
const consolidateContractSystem = "You revise a task's acceptance contract by APPLYING the council's " +
	"feedback — nothing more. Given the TASK, the CURRENT contract (criteria + checks), and the " +
	"FEEDBACK, return the REVISED contract. Apply the feedback EXACTLY: if it says reduce/consolidate " +
	"to N, return that many or fewer; if it says drop/merge/rename, do so; if it says fix, fix that. " +
	"Do NOT re-expand, re-add dropped items, or introduce anything the feedback did not ask for — the " +
	"result must be a CONSEQUENCE of the feedback, never a fresh superset. Keep criteria GOAL-focused " +
	"(what must be TRUE, not how to do it) and checks portable, outcome-verifying (never prescribe an " +
	"implementation method or a tool that may be absent). Reply with ONLY a JSON object, no prose:\n" +
	`{"criteria":["..."],"checks":[{"deliverable":"...","command":"...","expect":"..."}]}`

// consolidateContract applies the council's feedback to the current contract and returns the revised
// criteria + checks. Best-effort: nil provider / unparseable / empty reply → (_, _, false), and the
// caller keeps the current contract.
func (a *App) consolidateContract(ctx context.Context, spec AgentSpec, sid session.SessionID, model, task string, criteria []string, checks []council.DeliverableCheck, fb string) ([]string, []council.DeliverableCheck, bool) {
	if a.providerFor(spec) == nil {
		return nil, nil, false
	}
	cur, err := json.Marshal(struct {
		Criteria []string                   `json:"criteria"`
		Checks   []council.DeliverableCheck `json:"checks"`
	}{criteria, checks})
	if err != nil {
		return nil, nil, false
	}
	input := "# Task\n" + task + "\n\n# Current contract\n" + string(cur) + "\n\n# Council feedback to APPLY (and nothing more)\n" + fb
	raw := a.specMineCall(ctx, spec, sid, "contract-consolidate", model, consolidateContractSystem, input)
	for _, js := range balancedObjects(raw) {
		var d struct {
			Criteria []string                   `json:"criteria"`
			Checks   []council.DeliverableCheck `json:"checks"`
		}
		if json.Unmarshal([]byte(js), &d) == nil && (len(d.Criteria) > 0 || len(d.Checks) > 0) {
			return d.Criteria, d.Checks, true
		}
	}
	return nil, nil, false
}

// assignChecksSystem maps each contract check to the plan step that produces it.
const assignChecksSystem = "You assign each executable deliverable CHECK to the plan STEP that " +
	"produces it. You are given the numbered plan STEPS and a JSON array of CHECKS " +
	"({step, deliverable, command, expect}). Return the SAME checks, unchanged EXCEPT set each " +
	"check's `step` to the 1-based number of the step whose work produces that deliverable — the " +
	"step after which the check can first pass. Do NOT modify command/expect/deliverable, and do " +
	"NOT add or drop any check (return exactly as many as you were given). If a check verifies the " +
	"whole task or you cannot tell which step, assign it the LAST step's number. JSON array only, " +
	"no prose, no code fence."

// assignContractChecksToSteps labels the frozen contract's checks with the plan step that produces
// each, now that the plan exists. Without step labels the per-step delegate gate (verifyStepChecks,
// matched by step number) cannot gate on them — only the solo finish gate, which runs every check,
// does. Best-effort and idempotent: it no-ops when there is no frozen contract, no checks, no plan,
// no model, the checks are already all labelled, or the reply drops/adds checks. On success it
// re-stores the labelled set as the turn's deliverable checks.
func (a *App) assignContractChecksToSteps(ctx context.Context, s session.Session, steps []planStep) {
	sid := s.ID
	a.mu.Lock()
	frozen := a.stateLocked(sid).contractFrozen
	checks := append([]council.DeliverableCheck(nil), a.stateLocked(sid).deliverableChecks...)
	a.mu.Unlock()
	if !frozen || len(checks) == 0 || len(steps) == 0 {
		return
	}
	allLabelled := true
	for _, c := range checks {
		if strings.TrimSpace(c.Step) == "" {
			allLabelled = false
			break
		}
	}
	if allLabelled {
		return
	}
	agent := a.agentFor(s)
	if a.providerFor(agent) == nil {
		return
	}
	model := s.Model.Model
	if agent.Model != (session.ModelRef{}) {
		model = agent.Model.Model
	}
	in, err := json.Marshal(checks)
	if err != nil {
		return
	}
	input := "# Plan steps\n" + renderSteps(steps) + "\n\n# Checks to assign a step to\n" + string(in)
	raw := a.specMineCall(ctx, agent, sid, "check-assign", model, assignChecksSystem, input)
	out, ok := parseChecksArray(raw)
	if !ok || len(out) != len(checks) { // must preserve every check → otherwise keep the unlabelled set
		return
	}
	a.storePlanChecks(ctx, s, out)
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
	defer a.mu.Unlock()
	st := a.stateLocked(sid)
	if !st.contractFrozen {
		return ""
	}
	return st.contractText
}
