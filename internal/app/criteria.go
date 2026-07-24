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

// storeCoveredChecks fills any per-step coverage gap (ensureStepCoverage) and then stores the
// result. Callers pass the plan the checks are meant to gate: the plan audit its current procedure,
// the 0-step solo path a single synthetic step for the objective. When coverage is off or already
// complete this is exactly storePlanChecks(delib.Checks).
func (a *App) storeCoveredChecks(ctx context.Context, s session.Session, prompt string, steps []planStep, checks []council.DeliverableCheck) {
	a.storePlanChecks(ctx, s, a.ensureStepCoverage(ctx, s, prompt, steps, checks))
}

// checkValidateEnabled gates the deliverable-check review pass (default ON; MAGI_CHECK_VALIDATE=0 for
// an A/B baseline that uses the authored checks as-is).
func checkValidateEnabled() bool { return !envOff("MAGI_CHECK_VALIDATE") }

const coverageFillSystem = "You author executable deliverable `checks` that verify a plan's per-step outputs, FILLING GAPS. " +
	"You are given the plan STEPS (numbered in order), the TASK, and the checks authored SO FAR. Some steps that " +
	"PRODUCE a deliverable currently have NO check, so the completion gate cannot verify them. Return a JSON array = the " +
	"existing checks UNCHANGED, PLUS one NEW check for EACH producing step that lacks one. A check is " +
	"{step, deliverable, command, expect}: `command` runs and, if `expect` is set, its output must MATCH that regular " +
	"expression; no `expect` = exit-code-only. For every NEW check:\n" +
	"- SCOPE by step: set `step` to that step's 1-based number (matching the plan order) so it gates only that step.\n" +
	"- EXERCISE the deliverable (precondition is not proof): reaching the artifact — a file exists, a port accepts a " +
	"connection, a module imports, a build succeeds, a process is alive — is a precondition, NOT proof of the contract; " +
	"a non-functional stub passes all of them. When the step's artifact must DO something (answer a request, return a " +
	"value, transform an input, produce an output), invoke that behavior through the same interface its consumer uses " +
	"and assert on the RESULT (call the endpoint and assert the returned value, run the program on an input and compare " +
	"its output), choosing the weakest input that still forces the real code path so a stub that merely exists or opens " +
	"the port FAILS.\n" +
	"- PORTABLE: use ONLY tools guaranteed present (coreutils, grep/test, python3, the task's own toolchain). Any tool " +
	"OUTSIDE that set may be absent on the target image and exits 127 (`ss`, `netstat`, `lsof`, `pgrep`, `pidof`, `ps`, " +
	"`fuser`, `jq`, ... are examples of the class, not an exhaustive list) — a 127 then false-fails a correct deliverable " +
	"forever. Do the check with a python3 primitive instead: a port via a dependency-free socket connect, a process's " +
	"liveness via `os.kill(pid, 0)` or reading `/proc`, JSON via python's `json` — never by shelling to a process/" +
	"network/parse utility that may not be installed.\n" +
	"- IDEMPOTENT, NO STATE CHANGE (work≠check): verify the already-produced artifact READ-ONLY; NEVER create/build/" +
	"download/move/delete it (a check that re-does the work traps the run in a redo loop).\n" +
	"- A pure investigation/read-only step (it writes no artifact) needs NO check — do NOT invent one for it.\n" +
	"Do NOT alter or drop the existing checks, and do NOT change what any check verifies. JSON array only, no prose, no code fence."

// ensureStepCoverage guarantees every producing plan step has at least one deliverable check. The
// plan audit authors delib.Checks with no coverage guarantee — a weak model emits one check for a
// many-step plan, and runStepGate only verifies steps that appear in the check set, so the rest land
// unverified. When the authored checks cover fewer distinct steps than the plan has, a single gap-fill
// pass authors checks for the uncovered producing steps (read-only steps are told to be skipped, so an
// unfillable gap simply returns the same set — one extra call, never a loop). The 0-step solo path
// passes a single synthetic step, so it gets one check for its objective the same way. Best-effort: a
// disabled flag, no gap, a transport failure, or a reply that is not a coverage-increasing superset
// returns the input UNCHANGED, so the fill never weakens or blocks the authored contract.
func (a *App) ensureStepCoverage(ctx context.Context, s session.Session, prompt string, steps []planStep, checks []council.DeliverableCheck) []council.DeliverableCheck {
	if !checkCoverageEnabled() || len(steps) == 0 {
		return checks
	}
	covered := map[string]bool{}
	for _, c := range checks {
		covered[strings.TrimSpace(c.Step)] = true
	}
	if len(covered) >= len(steps) { // every step already has (at least) a check → no gap
		return checks
	}
	in, err := json.Marshal(checks)
	if err != nil {
		return checks
	}
	agent := a.agentFor(s)
	model := s.Model.Model
	if agent.Model != (session.ModelRef{}) {
		model = agent.Model.Model
	}
	task := strings.TrimSpace(prompt)
	if len(task) > 2000 { // the step list already carries the per-step detail; cap the raw task
		task = task[:2000]
	}
	input := "# Plan steps\n" + renderSteps(steps) + "\n\n# Task\n" + task + "\n\n# Checks authored so far (JSON)\n" + string(in)
	raw := a.specMineCall(ctx, agent, s.ID, "check-coverage", model, coverageFillSystem, input)
	out, ok := parseChecksArray(raw)
	if !ok || len(out) < len(checks) { // unusable / dropped existing checks → keep the authored set
		return checks
	}
	newCovered := map[string]bool{}
	for _, c := range out {
		newCovered[strings.TrimSpace(c.Step)] = true
	}
	if len(newCovered) <= len(covered) { // reply added no distinct-step coverage → nothing gained
		return checks
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-coverage",
		fmt.Sprintf("check-coverage: %d→%d checks, %d→%d step(s) covered (%d plan step(s))",
			len(checks), len(out), len(covered), len(newCovered), len(steps)))
	return out
}

const validateChecksSystem = "You review the executable deliverable `checks` a planning council authored, BEFORE " +
	"they are used to gate a task. Each check is {step, deliverable, command, expect}: the `command` runs and, if " +
	"`expect` is present, the command's output must MATCH that regular expression (no `expect` = exit-code-only). " +
	"Return ONLY a JSON array of the checks, REPAIRED where flawed and DROPPING any that cannot be made valid. Apply:\n" +
	"- SELF-CONSISTENCY (most important): the command's output must be ABLE to match its `expect`. A transform that " +
	"reshapes the output away from `expect` is a bug that false-fails forever — e.g. a pipeline ending in `sort -u` " +
	"collapses two identical lines into ONE, so an `expect` written for TWO can NEVER match; a `head -1` keeps only the " +
	"first line while `expect` names a later one. FIX by removing the offending transform, or BETTER convert to an " +
	"EXIT-CODE check (drop `expect`): assert each condition directly by chaining `&&` with `grep -q`.\n" +
	"- NECESSITY (no over-demand): assert ONLY what the task's own contract requires. NEVER pin a value the task did " +
	"not itself state — a specific version, build id, exact path, timestamp, or incidental attribute — and never " +
	"demand more than the stated outcome. Over-specification false-fails a CORRECT deliverable and can never converge " +
	"on an environment that differs in that incidental. Narrow each check to the minimal condition that proves the " +
	"objective: for an installed dependency assert it is importable/usable, not an exact version, UNLESS the task pins " +
	"one; drop or loosen any pinned specific the task did not require.\n" +
	"- PORTABLE: the command may use ONLY tools guaranteed present (coreutils, grep/test, python3, the task's own " +
	"toolchain). Any tool OUTSIDE that set may be absent on the target image and exits 127 (`ss`, `netstat`, `lsof`, " +
	"`pgrep`, `pidof`, `ps`, `fuser`, `jq`, ... are examples of the class, not an exhaustive list), which false-fails a " +
	"correct deliverable forever. Replace it with a python3 primitive: a port via a dependency-free socket connect, a " +
	"process's liveness via `os.kill(pid, 0)` or reading `/proc`, JSON via python's `json`. Invoke a tool by its " +
	"BARE name so PATH resolves it (`pip3`, or `python3 -m pip`); NEVER hardcode an absolute install path like " +
	"`/usr/bin/pip3` — the same tool lives at `/usr/local/bin/pip3` or a venv/pyenv shim on another image, so an " +
	"absolute path false-fails on the machine it was not written for. Strip any leading `/usr/bin/`, `/usr/local/bin/` " +
	"from a tool the PATH already resolves.\n" +
	"- TOOL-DERIVED NAMES: when a check greps for or stats a file a code generator EMITS, use the name the tool " +
	"ACTUALLY produces, not the request's raw spelling. `protoc`/`grpc_tools` sanitize a hyphenated `.proto` into an " +
	"UNDERSCORED module — a `data-feed.proto` yields `data_feed_pb2.py`, never `data-feed_pb2.py` — so a check demanding " +
	"the hyphenated form can NEVER pass and fights the toolchain (the agent renames to satisfy the grep, which breaks " +
	"the import, then renames back: an unwinnable loop). Rewrite the check to the generator's real output name.\n" +
	"- EXERCISES the deliverable (precondition is not proof): a check that only confirms the deliverable can be " +
	"REACHED — a file exists or is non-empty, a port accepts a connection, a module imports, a build succeeds, a " +
	"process is alive — is too weak, because a non-functional stub passes every one of those. When the task states " +
	"the deliverable must DO something (answer a request, return a value, transform an input, produce an output), the " +
	"check must INVOKE that named behavior through the same interface its consumer uses and assert on the RESULT — " +
	"call the endpoint and assert the returned value, run the program on an input and compare its output to the task's " +
	"stated mapping — choosing the weakest input that still forces the real code path so a stub that merely exists or " +
	"opens the port FAILS. Do not DROP such a check for being weak; STRENGTHEN it into one that exercises the contract.\n" +
	"- IDEMPOTENT, NO STATE CHANGE (work≠check): a check must VERIFY the deliverable read-only, never PERFORM the " +
	"step's work. DROP or repair any command that CREATES/MUTATES the artifact — compress/download/build/generate/" +
	"move/delete (`tar -czf`, `scp`/`rsync`, `rm`, `mv`, a `>` redirect that writes the deliverable, `git commit`): " +
	"re-doing the work as its own check re-runs the step every gate cycle and false-fails on any transient error, " +
	"trapping the run in a redo loop. Replace with an idempotent read-only probe of the already-produced artifact at " +
	"its final path (`tar -tzf f.tgz` LIST not `-czf` CREATE, `test -s f`, run the built binary not re-build it). " +
	"Verify the step's stated deliverable, not an intermediate.\n" +
	"- Preserve each check's `step` label exactly — it scopes the check to its step. A cleanup/absence check " +
	"(`test ! -f a.tgz`) MUST keep its own step label; never merge it onto the same step as an existence check " +
	"(`test -s a.tgz`) for the same artifact — they are verified at different steps, and co-locating them makes a " +
	"jointly-unsatisfiable checklist. Keep `expect` ONLY when it reliably matches correct output; " +
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
