package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// noCriteria is the cached sentinel meaning "elicitation ran this turn and
// produced nothing" — distinct from "" (not yet elicited).
const noCriteria = "\x00"

// storePlanCriteria records the completion criteria the plan-audit council derived
// as this turn's contract, so the termination gate reads them (without re-eliciting)
// and judges "done" against them. It NEVER writes the noCriteria sentinel — an
// empty set leaves the opt-in elicitation path intact — and emits the same
// reviewable artifact as elicitation (D15 parity). Called only for the plan that
// is actually proceeding (approved or force-approved), so a re-plan overwrites.
func (a *App) storePlanCriteria(ctx context.Context, s session.Session, crit []string) {
	if len(crit) == 0 {
		return
	}
	// A contract-first gate (D-contract) already authored+reviewed this turn's criteria before the
	// plan existed and FROZE them; the later plan-audit must not overwrite that reviewed contract
	// with criteria it derived as a byproduct of the plan. The contract gate itself stores criteria
	// BEFORE setting the freeze, so this guard never blocks the contract's own write.
	a.mu.Lock()
	frozen := a.stateLocked(s.ID).contractFrozen
	a.mu.Unlock()
	if frozen {
		return
	}
	text := "- " + strings.Join(crit, "\n- ")
	a.mu.Lock()
	a.stateLocked(s.ID).criteria = text
	a.mu.Unlock()
	content, _ := json.Marshal(text)
	a.emitArtifact(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "council"}, artifact.Artifact{
		ID: "art_" + newID(), Kind: "acceptance-criteria", Title: "Acceptance criteria (plan audit)",
		Content: content, SourceAgent: "council", Status: "proposed", Created: time.Now(),
	})
}

// renderCriteriaChecklist turns the stored criteria (a "- item" bullet block) into an ENUMERATED
// per-item checklist with an explicit instruction: the termination council must judge EACH item
// SATISFIED/UNSATISFIED on evidence and may land "done" only when EVERY item is satisfied, naming
// any unsatisfied one in feedback (D-contract Stage 3). This is the holistic-judgment fix — a block
// of criteria invites a weak model to wave a partly-met contract to done; a numbered list forces a
// per-item verdict. Falls back to the raw block if nothing parses.
func renderCriteriaChecklist(crit string) string {
	var items []string
	for _, ln := range strings.Split(crit, "\n") {
		ln = strings.TrimSpace(ln)
		ln = strings.TrimSpace(strings.TrimPrefix(ln, "-"))
		if ln != "" {
			items = append(items, ln)
		}
	}
	if len(items) == 0 {
		return "Acceptance criteria:\n" + crit
	}
	var b strings.Builder
	b.WriteString("Acceptance criteria — judge EACH item individually against the evidence; vote \"done\" ONLY if EVERY " +
		"item is SATISFIED, and if ANY item is UNSATISFIED vote continue and name which one(s) in feedback:\n")
	for i, it := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, it)
	}
	return strings.TrimRight(b.String(), "\n")
}

// storeStepEstimate records the planner's advisory step estimate for the turn
// (clamped to sane bounds); 0/garbage stores nothing. Never a limit — see the
// budget line in volatileContext for how it is worded.
func (a *App) storeStepEstimate(sid session.SessionID, est int) {
	if est <= 0 || est > 10000 {
		return
	}
	a.mu.Lock()
	a.stateLocked(sid).estSteps = est
	a.mu.Unlock()
}

// stepEstimate returns the turn's advisory estimate, or 0 when none was made.
func (a *App) stepEstimate(sid session.SessionID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.estSteps
	}
	return 0
}

// cachedCriteria returns this turn's already-known acceptance criteria (e.g. set by
// the plan-audit council) WITHOUT eliciting — the noCriteria sentinel reads empty.
func (a *App) cachedCriteria(sid session.SessionID) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	st, ok := a.stateIf(sid)
	if !ok {
		return ""
	}
	if c := st.criteria; c != noCriteria {
		return c
	}
	return ""
}

// acceptanceCriteria returns the turn's acceptance criteria (D15), eliciting them
// once (cached per session, cleared on a new turn) and emitting them as a
// reviewable artifact so the contract the council judges against is observable.
func (a *App) acceptanceCriteria(ctx context.Context, agent AgentSpec, s session.Session, task string) string {
	a.mu.Lock()
	c := a.stateLocked(s.ID).criteria
	a.mu.Unlock()
	if c == noCriteria { // elicitation already ran this turn and produced nothing
		return ""
	}
	if c != "" {
		return c
	}
	if strings.TrimSpace(task) == "" {
		return ""
	}
	c = a.elicitCriteria(ctx, agent, s, task)
	if c == "" {
		// Cache the miss so a persistently failing elicitation isn't retried every
		// round (strictly once per turn).
		a.mu.Lock()
		a.stateLocked(s.ID).criteria = noCriteria
		a.mu.Unlock()
		return ""
	}
	a.mu.Lock()
	a.stateLocked(s.ID).criteria = c
	a.mu.Unlock()
	content, _ := json.Marshal(c)
	a.emitArtifact(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "council"}, artifact.Artifact{
		ID: "art_" + newID(), Kind: "acceptance-criteria", Title: "Acceptance criteria",
		Content: content, SourceAgent: "council", Status: "proposed", Created: time.Now(),
	})
	return c
}

// elicitCriteriaSystem instructs the criteria elicitation. Beyond listing prose
// done-conditions, it asks that any execution-confirmable condition also carry HOW to
// confirm it (the command/call and expected output), reusing any verification procedure
// the task itself states — so the contract is checkpoint-friendly for both the executor
// (checkpoint-first) and the termination gate.
const elicitCriteriaSystem = "You define acceptance criteria for a coding task. List the concrete, checkable " +
	"conditions that must ALL hold for it to be DONE — correctness, tests/build passing, edge cases, and staying " +
	"in scope. For any condition that can be confirmed by execution, also state HOW to confirm it (the exact " +
	"command or function call to run and the expected output), reusing any verification procedure the task itself " +
	"specifies. Output a short bullet checklist only, no preamble."

// elicitCriteria asks the model (tool-free) for the concrete done-conditions of a
// task. Uses the agent's provider so it follows per-agent backend routing.
func (a *App) elicitCriteria(ctx context.Context, agent AgentSpec, s session.Session, task string) string {
	req := port.ChatRequest{
		Model:    s.Model.Model,
		System:   elicitCriteriaSystem,
		Messages: []session.Message{{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: task}}}},
	}
	stream, err := a.providerFor(agent).StreamChat(ctx, req)
	if err != nil {
		return ""
	}
	text, cut := drainStream(stream)
	if cut != nil {
		a.emitToolProgress(s.ID, plannerActor, "", "criteria",
			fmt.Sprintf("criteria: the reply was CUT OFF after %d chars — %v (the contract may be incomplete)", len(text), cut))
	}
	return strings.TrimSpace(text)
}
