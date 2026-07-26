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
	st := a.stateLocked(s.ID)
	st.deliverableChecks = checks
	st.checksVer++ // signal the incremental recorder that the check set changed (re-plan mid-run)
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
	"- PORTABLE: depend ONLY on what the TARGET ENVIRONMENT guarantees — base OS (coreutils, grep/test), the task's " +
	"language runtime (python3), and the task's own toolchain. A dependency it does NOT guarantee — of ANY kind: an " +
	"external shell tool, a language library/module, a runtime, a service, a file — false-fails a correct deliverable " +
	"forever, since the check errors on the missing dependency instead of judging the artifact. Two instances of the " +
	"ONE rule: (a) an EXTERNAL shell tool outside the base set (`ss`, `netstat`, `lsof`, `pgrep`, `pidof`, `ps`, " +
	"`fuser`, `jq`, ... examples, not an exhaustive list) exits 127 — do the check with a python3 primitive: a port " +
	"via a dependency-free socket connect, process liveness via `os.kill(pid, 0)` or reading `/proc`, JSON via " +
	"python's `json`. (b) a NON-stdlib language module is absent just the same — do NOT use `pkg_resources` (removed " +
	"from modern setuptools); read a version with `importlib.metadata.version('pkg')` or the module's `__version__`, " +
	"or just assert the import works.\n" +
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
	// Count coverage by the check's REAL plan-step number (1-based, as matchStepChecks resolves it),
	// not its raw label: a check carrying an out-of-range label ("99") or a non-numeric one ("setup")
	// does not attach to any plan step, so counting it as distinct coverage would wrongly conclude the
	// plan is fully covered and suppress the gap-fill for a step that genuinely has no check.
	coveredSteps := func(cs []council.DeliverableCheck) map[int]bool {
		m := map[int]bool{}
		for _, c := range cs {
			if n := leadingInt(c.Step); n >= 1 && n <= len(steps) {
				m[n] = true
			}
		}
		return m
	}
	covered := coveredSteps(checks)
	if len(covered) >= len(steps) { // every step already has (at least) a check → no gap
		return checks
	}
	// From here a gap is CONFIRMED, so every exit below leaves plan steps ungated. Report which one
	// happened: silence used to be indistinguishable from "fully covered", and a run that landed a
	// 5-step plan behind a single check read exactly like a run that needed no fill at all.
	gap := fmt.Sprintf("%d/%d step(s) covered by %d check(s)", len(covered), len(steps), len(checks))
	shortfall := func(why string) {
		a.emitToolProgress(s.ID, plannerActor, "", "check-coverage",
			fmt.Sprintf("check-coverage: gap NOT filled (%s) — %s; those steps land unverified", gap, why))
	}
	in, err := json.Marshal(checks)
	if err != nil {
		shortfall("the authored checks could not be encoded")
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
		if !ok {
			shortfall(fmt.Sprintf("the fill reply did not parse as a checks array (%d chars)", len(raw)))
		} else {
			shortfall(fmt.Sprintf("the fill dropped existing checks (%d→%d), so it was discarded", len(checks), len(out)))
		}
		return checks
	}
	newCovered := coveredSteps(out)
	if len(newCovered) <= len(covered) { // reply added no distinct (valid) step coverage → nothing gained
		shortfall("the fill added no check that attaches to an uncovered step")
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
	"- PORTABLE: a check may depend ONLY on what the TARGET ENVIRONMENT guarantees — the base OS (coreutils, " +
	"grep/test), the language runtime the task uses (python3), and the task's OWN declared toolchain. A dependency it " +
	"does NOT guarantee — of ANY kind: an external shell tool, a language library/module, a runtime version, a " +
	"service, a file — false-fails a correct deliverable forever, because the check errors on the missing dependency " +
	"instead of judging the artifact. Two common instances of the ONE rule: (a) an EXTERNAL shell tool outside the " +
	"base set (`ss`, `netstat`, `lsof`, `pgrep`, `pidof`, `ps`, `fuser`, `jq`, ... examples, not an exhaustive list) " +
	"exits 127 — do the check with a python3 primitive: a port via a dependency-free socket connect, process liveness " +
	"via `os.kill(pid, 0)` or reading `/proc`, JSON via python's `json`. (b) a NON-stdlib language module is absent " +
	"just the same — `pkg_resources` (removed from modern setuptools) is the common trap, so read a version with " +
	"`importlib.metadata.version('pkg')` or the module's `__version__`, or just assert the import, never a " +
	"distribution lookup. Invoke a tool by its BARE name so PATH resolves it (`pip3`, or `python3 -m pip`); NEVER " +
	"hardcode an absolute install path like `/usr/bin/pip3` — the same tool lives at `/usr/local/bin/pip3` or a " +
	"venv/pyenv shim on another image, so strip any leading `/usr/bin/`, `/usr/local/bin/` from a tool the PATH " +
	"already resolves.\n" +
	"- TOOL-DERIVED NAMES: when a check greps for or stats a file a code generator EMITS, use the name the tool " +
	"ACTUALLY produces, not the request's raw spelling. `protoc`/`grpc_tools` sanitize a hyphenated `.proto` into an " +
	"UNDERSCORED module — a `data-feed.proto` yields `data_feed_pb2.py`, never `data-feed_pb2.py` — so a check demanding " +
	"the hyphenated form can NEVER pass and fights the toolchain (the agent renames to satisfy the grep, which breaks " +
	"the import, then renames back: an unwinnable loop). Rewrite the check to the generator's real output name.\n" +
	"- SEMANTICS, not source spelling (verify meaning by effect; never grep the task's prose back into the source): " +
	"a structure or behavior the task states in PROSE — a message/record with named typed fields, a function returning " +
	"a typed value, a format it must accept — is a SEMANTIC to satisfy, NOT a literal string the source file must " +
	"contain. Verify it by EXERCISING the built artifact (compile/generate and inspect the produced type, run it and " +
	"assert the typed result), never by grepping the SOURCE for the task's wording or an INVENTED notation of it — a " +
	"pseudo-syntax like `<field: type>`, a `^service X$` that forces the name alone on a line, a required brace " +
	"position. The task fixes IDENTIFIERS and VALUES verbatim (a message/service/RPC/function NAME, a port, a " +
	"filename, a pinned version) and those a check MAY assert literally; but a field's declaration syntax, a type's " +
	"spelling, and source layout are the author's to choose, so pinning them false-fails a correct artifact and forces " +
	"the agent to contort valid code toward a fabricated pattern (often one no real compiler accepts). Rewrite such a " +
	"check to assert the EFFECT — the artifact builds and the generated/runtime type has that named field — not the surface.\n" +
	"- EXERCISES the deliverable (precondition is not proof): a check that only confirms the deliverable can be " +
	"REACHED — a file exists or is non-empty, a port accepts a connection, a module imports, a build succeeds, a " +
	"process is alive, or a SETTING merely SUPPOSED to produce the deliverable is in place (a build flag configured, " +
	"an env var exported, a config value written) — is too weak, because a non-functional stub, or a configuration " +
	"that never took effect, passes every one of those. When the task states the deliverable must DO something " +
	"(answer a request, return a value, transform an input, produce an output), the check must INVOKE that named " +
	"behavior through the same interface its consumer uses and assert on the RESULT — call the endpoint and assert the " +
	"returned value, run the program on an input and compare its output to the task's stated mapping — choosing the " +
	"weakest input that still forces the real code path so a stub that merely exists or opens the port FAILS.\n" +
	"  · EFFECT, not its cause: when the deliverable is the EFFECT a configuration is supposed to cause, assert the " +
	"effect after RUNNING the step that consumes the setting (the artifact the configured build emits appears from a " +
	"fresh run), NOT that the setting is present — a flag that never took effect passes the config check and fails the " +
	"real one.\n" +
	"  · WHOLE standard, not a spot-check: when the task supplies a reference output, an expected dataset, or a " +
	"threshold to meet, assert against that WHOLE standard (the full output matches the reference, or the count/" +
	"fraction clears the task's stated bar), never a single hand-picked sample — one row that happens to match passes " +
	"a deliverable that is wrong on all the rest.\n" +
	"Do not DROP such a check for being weak; STRENGTHEN it into one that exercises the contract.\n" +
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
	"otherwise drop `expect` and rely on the exit code.\n" +
	"- SCOPE (repair, do not retarget): only repair HOW each check proves its OWN stated deliverable.\n" +
	"  · strengthening a proxy into the real behavioral assertion of that SAME deliverable — a config into the effect " +
	"it causes, a single sample into the whole reference — is the repair intended here, NOT a forbidden change.\n" +
	"  · do NOT invent a check for a DIFFERENT deliverable, or retarget a check to another step's objective.\n" +
	"Return [] if none survive. JSON array only, no prose, no code fence."

// validateChecks runs a tool-free review over the plan-audit's deliverable checks, repairing or
// dropping ones whose command cannot satisfy its own expect, uses a missing tool, or only asserts a
// file exists. Best-effort: on a disabled flag, an empty set, a transport failure, or an unparseable
// reply it returns the input UNCHANGED, so the review never blocks a plan.
func (a *App) validateChecks(ctx context.Context, agent AgentSpec, s session.Session, checks []council.DeliverableCheck) []council.DeliverableCheck {
	if !checkValidateEnabled() || len(checks) == 0 || a.providerFor(agent) == nil {
		return checks // no model wired (e.g. council-only tests) → keep the authored checks as-is
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
	// Scan every top-level balanced [...] (respecting strings), not a naive first-[/last-] span: a
	// reply that wraps the array in prose or trails reasoning with a stray ] would otherwise mis-span
	// and lose the whole audit. Take the first array that yields runnable checks; else the first that
	// unmarshals at all (a legitimately empty list); else none.
	sawValid := false
	for _, js := range balancedArrays(raw) {
		// Apply the same weak-model repairs the plan object and the salvaged steps get: a check's
		// `command` is a SHELL command, so a raw newline or tab inside that string is the likeliest
		// defect of all — and rejecting the array over it discards every check in the reply, which
		// leaves the plan with no executable contract at all (observed: a coverage fill's 380-char
		// reply thrown away whole, and the run proceeded with zero checks for five steps).
		cs, ok := unmarshalChecksLenient(js)
		if !ok {
			continue // not JSON, or not the checks shape — try the next array
		}
		sawValid = true
		var out []council.DeliverableCheck
		for _, c := range cs {
			if strings.TrimSpace(c.Command) != "" {
				out = append(out, c)
			}
		}
		if len(out) > 0 {
			return out, true
		}
	}
	return nil, sawValid
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

// checksVersion returns the monotonic version of this turn's stored deliverable-check set — bumped
// each time storePlanChecks (re)writes it. The incremental recorder fires when this changes so a
// re-plan that derives new checks mid-run gets a recording pass even without a fresh mutation/exec.
func (a *App) checksVersion(sid session.SessionID) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.stateIf(sid); ok {
		return st.checksVer
	}
	return 0
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

// unmarshalChecksLenient parses one candidate array of deliverable checks, retrying with the shared
// weak-model repairs (jsonRepairCandidates) before giving up. It exists for the same reason the plan
// and step readers are lenient — the difference here is that the payload is shell commands, so an
// unescaped control character is not an edge case but the normal shape of the data.
func unmarshalChecksLenient(js string) ([]council.DeliverableCheck, bool) {
	var cs []council.DeliverableCheck
	if unmarshalLenient(js, &cs) {
		return cs, true
	}
	return nil, false
}
