package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/artifact"
	"github.com/sayaya1090/magi/internal/core/council"
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

// storePlanChecks records the per-step executable deliverable checks the plan-audit
// council derived, so the solo loop's deterministic step gate can settle the contract
// by execution (see stepVerifyEnabled). Mirrors storePlanCriteria: called only for
// the plan that actually proceeds, so a re-plan overwrites, and it emits a reviewable
// artifact so the executable contract is observable. Empty input stores nothing.
func (a *App) storePlanChecks(ctx context.Context, s session.Session, checks []council.DeliverableCheck) {
	if len(checks) == 0 || !stepVerifyEnabled() { // OFF → fully inert (no state, no artifact, no todo change)
		return
	}
	// Validate the checks BEFORE they become the gate: the authoring members can write a check whose
	// own command cannot satisfy its own expect (a `sort -u` that dedups two identical versions while
	// the expect wants both), which then FALSE-FAILS a correct step forever. A tool-free review pass
	// repairs/drops such checks — the same "review beats self-check" principle the council rests on.
	checks = a.validateChecks(ctx, a.agentFor(s), s, checks)
	if len(checks) == 0 {
		return
	}
	a.mu.Lock()
	a.stateLocked(s.ID).deliverableChecks = checks
	a.mu.Unlock()
	content, _ := json.Marshal(checks)
	a.emitArtifact(ctx, s.ID, event.Actor{Kind: event.ActorSystem, ID: "council"}, artifact.Artifact{
		ID: "art_" + newID(), Kind: "deliverable-checks", Title: "Deliverable checks (plan audit)",
		Content: content, SourceAgent: "council", Status: "proposed", Created: time.Now(),
	})
	a.annotateTodosWithDeliverables(ctx, s.ID, checks) // show each step's expected deliverable in the panel
}

// checkValidateEnabled gates the deliverable-check review pass (default ON; MAGI_CHECK_VALIDATE=0 for
// an A/B baseline that uses the authored checks as-is).
func checkValidateEnabled() bool { return !envOff("MAGI_CHECK_VALIDATE") }

const validateChecksSystem = "You review the executable deliverable `checks` a planning council authored, BEFORE " +
	"they are used to gate a task. Each check is {step, deliverable, command, expect}: the `command` runs and, if " +
	"`expect` is present, the command's output must MATCH that regular expression (no `expect` = exit-code-only). " +
	"Return ONLY a JSON array of the checks, REPAIRED where flawed and DROPPING any that cannot be made valid. Apply:\n" +
	"- SELF-CONSISTENCY (most important): the command's output must be ABLE to match its `expect`. A transform that " +
	"reshapes the output away from `expect` is a bug that false-fails forever — e.g. `sort -u` collapses duplicate " +
	"lines, so `pip show grpcio grpcio-tools | grep '^Version:' | sort -u | awk '{print $2}'` yields ONE version, " +
	"but an expect of `^1\\.73\\.0 1\\.73\\.0$` (two) can NEVER match. FIX by removing the offending transform, or " +
	"BETTER convert to an EXIT-CODE check (drop `expect`): chain the conditions with `&&` and `grep -q` (e.g. " +
	"`pip show grpcio 2>/dev/null | grep -q '^Version: 1.73.0' && pip show grpcio-tools 2>/dev/null | grep -q '^Version: 1.73.0'`).\n" +
	"- PORTABLE: the command may use ONLY tools guaranteed present (coreutils, grep/test, python3, the task's own " +
	"toolchain). Replace `ss`/`netstat`/`lsof` with a dependency-free python socket connect.\n" +
	"- EXERCISES the deliverable: a bare file-existence/size check for something that must BEHAVE or produce a " +
	"correct value is too weak; keep/author a check that RUNS it and asserts the outcome.\n" +
	"- Preserve each check's `step` label exactly. Keep `expect` ONLY when it reliably matches correct output; " +
	"otherwise drop `expect` and rely on the exit code. Do NOT invent new checks or change what a check verifies — " +
	"only repair HOW it verifies. Return [] if none survive. JSON array only, no prose, no code fence."

// validateChecks runs a tool-free review over the plan-audit's deliverable checks, repairing or
// dropping ones whose command cannot satisfy its own expect, uses a missing tool, or only asserts a
// file exists. Best-effort: on a disabled flag, an empty set, a transport failure, or an unparseable
// reply it returns the input UNCHANGED, so the review never blocks a plan.
func (a *App) validateChecks(ctx context.Context, agent AgentSpec, s session.Session, checks []council.DeliverableCheck) []council.DeliverableCheck {
	if !checkValidateEnabled() || len(checks) == 0 {
		return checks
	}
	in, err := json.Marshal(checks)
	if err != nil {
		return checks
	}
	model := s.Model.Model
	if agent.Model != (session.ModelRef{}) {
		model = agent.Model.Model
	}
	raw := a.specMineCall(ctx, agent, s.ID, "check-audit", model, validateChecksSystem, string(in))
	out, ok := parseChecksArray(raw)
	if !ok || len(out) == 0 { // unusable review → keep the authored checks rather than drop the contract
		return checks
	}
	a.recordCheckAudit(ctx, s.ID, checks, out)
	return out
}

// recordCheckAudit persists what the check review changed — not just a count — so a rejected or
// repaired check is inspectable after the fact (why a step gated the way it did). It emits a
// reviewable "check-audit" artifact carrying the FULL before/after check sets, and a progress line
// naming the deliverables that were dropped or had their verifying command rewritten. A check is
// "kept as-is" iff its exact command survives; anything else (dropped OR repaired) is reported.
// No-op when nothing changed.
func (a *App) recordCheckAudit(ctx context.Context, sid session.SessionID, before, after []council.DeliverableCheck) {
	afterCmd := make(map[string]bool, len(after))
	for _, c := range after {
		afterCmd[strings.TrimSpace(c.Command)] = true
	}
	var changed []string
	for _, c := range before {
		if afterCmd[strings.TrimSpace(c.Command)] {
			continue // survived verbatim → kept
		}
		d := strings.TrimSpace(c.Deliverable)
		if d == "" {
			d = clipLine(strings.TrimSpace(c.Command), 60)
		}
		changed = append(changed, d)
	}
	if len(changed) == 0 && len(before) == len(after) {
		return // review ran but left every check untouched — nothing to report
	}
	content, _ := json.Marshal(map[string][]council.DeliverableCheck{"before": before, "after": after})
	a.emitArtifact(ctx, sid, event.Actor{Kind: event.ActorSystem, ID: "council"}, artifact.Artifact{
		ID: "art_" + newID(), Kind: "check-audit", Title: "Deliverable check audit (repaired/dropped)",
		Content: content, SourceAgent: "council", Status: "proposed", Created: time.Now(),
	})
	msg := fmt.Sprintf("check-audit: %d → %d checks", len(before), len(after))
	if len(changed) > 0 {
		msg += " — dropped/repaired: " + clipLine(strings.Join(changed, "; "), 240)
	}
	a.emitToolProgress(sid, plannerActor, "", "check-audit", msg)
}

// parseChecksArray extracts the first balanced JSON array from a review reply and unmarshals it into
// deliverable checks. A check with no command is dropped (nothing to run).
func parseChecksArray(raw string) ([]council.DeliverableCheck, bool) {
	s := strings.TrimSpace(raw)
	i, j := strings.IndexByte(s, '['), strings.LastIndexByte(s, ']')
	if i < 0 || j <= i {
		return nil, false
	}
	var cs []council.DeliverableCheck
	if json.Unmarshal([]byte(s[i:j+1]), &cs) != nil {
		return nil, false
	}
	var out []council.DeliverableCheck
	for _, c := range cs {
		if strings.TrimSpace(c.Command) != "" {
			out = append(out, c)
		}
	}
	return out, true
}

// cachedChecks returns this turn's per-step executable deliverable checks (set by the
// plan-audit council), or nil when none were derived. Read by the step gate.
func (a *App) cachedChecks(sid session.SessionID) []council.DeliverableCheck {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.deliverableChecks
	}
	return nil
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
	var b strings.Builder
	for ev := range stream {
		if ev.Type == port.ProviderText {
			b.WriteString(ev.Text)
		}
	}
	return strings.TrimSpace(b.String())
}
