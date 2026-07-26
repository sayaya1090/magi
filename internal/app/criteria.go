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
	"- SCOPE by step: set `step` to that step's 1-based position in the plan order (\"3\"), so it gates only that " +
	"step. The other authoring prompt shows `step` as a string; either shape is read, but keep them consistent.\n" +
	"- EXERCISE the deliverable (precondition is not proof): reaching the artifact — a file exists, a port accepts a " +
	"connection, a module imports, a build succeeds, a process is alive — is a precondition, NOT proof of the contract; " +
	"a non-functional stub passes all of them. When the step's artifact must DO something (answer a request, return a " +
	"value, transform an input, produce an output), invoke that behavior through the same interface its consumer uses " +
	"and assert on the RESULT (call the endpoint and assert the returned value, run the program on an input and compare " +
	"its output), choosing the weakest input that still forces the real code path so a stub that merely exists or opens " +
	"the port FAILS. WHERE that invocation runs is fixed by RECORD AND READ below — the step runs it and saves the " +
	"output, the check asserts on the saved output — but never weaken WHAT is asserted to a mere existence probe. " +
	"A command that SUCCEEDED is the same kind of precondition — a configure/build/install exiting 0 " +
	"with a flag on its command line proves the flag was ACCEPTED, not that it took EFFECT — so when the step's " +
	"deliverable is the effect a setting is supposed to cause, run whatever consumes the setting and assert the " +
	"resulting artifact appears AT THE LOCATION the task names, never that the setting or the command is in place.\n" +
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
	"download/move/delete it (a check that re-does the work traps the run in a redo loop). The check runs in a " +
	"READ-ONLY SHELL that BLOCKS mutating commands (build drivers and compilers, package installers, `rm`/`mv`, " +
	"archive create/extract, `git` write subcommands), and a blocked check yields NO verdict — its step lands " +
	"unverified anyway.\n" +
	"- RECORD AND READ (the default shape): the STEP runs, the CHECK reads. Whenever proving the deliverable means " +
	"RUNNING something — a build, a test command, the produced program on an input, a server round-trip — that run " +
	"is the STEP's work: the step performs it once and saves the REAL output to a result file at a fixed path, and " +
	"the check READS that file (`grep -q '<expected outcome>' <result file>`, or a python3 parse of it). Say in the " +
	"`deliverable` text that the step must save that file, and name the SAME path in both. The behaviour is still " +
	"proven — the run happened and its actual output is what you assert — but the check cannot be refused by the " +
	"read-only shell, cannot re-do the work each gate cycle, and fails honestly through `grep`'s exit status. Run a " +
	"command DIRECTLY in a check only to INSPECT what already exists without changing it (`test -s f`, `grep -q pat " +
	"f`, `tar -tzf f.tgz`).\n" +
	"- A pure investigation/read-only step (it writes no artifact) needs NO check — do NOT invent one for it.\n" +
	"Do NOT alter or drop the existing checks, and do NOT change what any check verifies. JSON array only, no prose, no code fence."

// ensureStepCoverage guarantees every producing plan step has at least one deliverable check. The
// plan audit authors delib.Checks with no coverage guarantee — a weak model emits one check for a
// many-step plan, and runStepGate only verifies steps that appear in the check set, so the rest land
// unverified. When the authored checks cover fewer distinct steps than the plan has, a single gap-fill
// pass authors checks for the uncovered producing steps (read-only steps are told to be skipped, so an
// unfillable gap simply returns the same set). A fill that comes back unparseable or adds no coverage
// is re-asked ONCE with a reminder naming what went wrong — at most two calls, never a loop. The 0-step
// solo path passes a single synthetic step, so it gets one check for its objective the same way.
// Best-effort: a disabled flag, no gap, a transport failure, or a second reply that still adds no
// distinct step coverage returns the input UNCHANGED. A reply that DOES add coverage is merged with the authored set rather than
// replacing it (unionChecks), so the fill can only ever add — it never weakens or blocks the contract
// it was called to complete.
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

	// An attempt is the side call PLUS the merge and coverage judgment it lives or dies by — the two
	// are inseparable here, since "the reply parsed" says nothing about whether the gap closed. The raw
	// reply travels with it because every failure note has to be able to show what actually came back.
	type fillAttempt struct {
		raw      string
		out      []council.DeliverableCheck
		parsed   bool
		merged   []council.DeliverableCheck
		newCov   map[int]bool
		restored int
	}
	run := func(system string) fillAttempt {
		f := fillAttempt{}
		f.raw = a.specMineCall(ctx, agent, s.ID, "check-coverage", model, system, input)
		f.out, f.parsed = parseChecksArray(f.raw)
		if f.parsed {
			// Restore whatever the reply lost instead of discarding the reply over it. The fill is told
			// to return the authored checks unchanged plus new ones, but a weak model rewrites the set
			// from scratch and comes back with fewer — and the old guard (`len(out) < len(checks)`) threw
			// the whole reply away on that count alone, judging a gap-FILLING pass by size rather than by
			// coverage. Observed: 4 authored checks all attached to step 1 of a 4-step plan, the fill
			// returned 3 spread across the plan, and the count guard preferred the 4 (`gap NOT filled
			// (1/4 step(s) covered by 4 check(s))`). Merging keeps the authored contract a subset of the
			// result by construction, so the one thing that guard protected is now structural and
			// acceptance turns purely on coverage.
			f.merged, f.restored = unionChecks(f.out, checks)
			f.newCov = coveredSteps(f.merged)
		}
		return f
	}
	gained := func(f fillAttempt) bool { return f.parsed && len(f.newCov) > len(covered) }
	// describe names WHICH of the two failures an attempt was, in the terms they actually differ by.
	// The retry note and the final shortfall both need it and must agree, so it is written once.
	describe := func(f fillAttempt) string {
		if !f.parsed {
			// Show the reply, not just its length. This failure was observed three runs running and
			// could not be diagnosed from the log: the side call leaves no session record, so the
			// only way to tell "the model answered with prose" from "it wrapped the array in an
			// object" from "an element had the wrong shape" is to print what came back.
			return fmt.Sprintf("the fill reply did not parse as a checks array (%d chars) :: %s",
				len(f.raw), planParseExcerpt(f.raw))
		}
		// Name the step labels it DID carry. "Added nothing that attaches" has two very different
		// causes — the fill returned nothing at all, or it returned checks whose step labels fall
		// outside 1..len(steps) or are not numeric — and only the labels distinguish them.
		labels := make([]string, 0, len(f.out))
		for _, c := range f.out {
			labels = append(labels, fmt.Sprintf("%q", strings.TrimSpace(c.Step)))
		}
		why := fmt.Sprintf("the fill returned %d check(s) but none attach to an uncovered step; their step labels were [%s] and the plan has %d step(s)",
			len(f.out), strings.Join(labels, " "), len(steps))
		if len(f.out) == 0 {
			// With no checks there are no labels, so the message above degenerates to "returned
			// 0, labels []" — accurate and useless. Three very different events land here and
			// only the reply tells them apart: the model answered with an empty array, or it
			// authored checks with an empty `command` (parsed, then dropped as unrunnable), or
			// it answered with prose that happened to contain a bare `[]`.
			why += " :: " + planParseExcerpt(f.raw)
		}
		return why
	}

	f := run(coverageFillSystem)
	if !gained(f) {
		// Re-ask ONCE. A confirmed gap that the fill did not close leaves plan steps with no gate at
		// all, and until now a single degraded reply ended the pass — the same defect the check-audit
		// retry fixed in the review pass, in the pass that FEEDS it. Both shapes get the reminder that
		// names what went wrong; neither retries again, so this is bounded at two calls.
		reminder := coverageJSONOnlyReminder
		if f.parsed {
			reminder = coverageAttachReminder(uncoveredStepNums(covered, len(steps)), len(steps))
		}
		a.emitToolProgress(s.ID, plannerActor, "", "check-coverage",
			fmt.Sprintf("check-coverage: %s — re-asking once", describe(f)))
		retry := run(coverageFillSystem + "\n\n" + reminder)
		if !gained(retry) {
			// Honest about the ambiguity: after a re-ask that listed the uncovered steps by number, a
			// second empty answer is as likely to mean "those steps produce nothing checkable" (the
			// prompt tells it to skip pure investigation steps) as it is to mean the fill failed. Either
			// way those steps land unverified, which is what the note reports.
			shortfall(describe(retry) + " — and this was the re-ask, which listed the uncovered step(s) by " +
				"number; either they produce nothing checkable or the fill could not do it")
			return checks
		}
		f = retry
	}
	note := fmt.Sprintf("check-coverage: %d→%d checks, %d→%d step(s) covered (%d plan step(s))",
		len(checks), len(f.merged), len(covered), len(f.newCov), len(steps))
	if f.restored > 0 { // the fill dropped authored checks and they were put back — say so, it is not a no-op
		note += fmt.Sprintf("; restored %d authored check(s) the fill had dropped", f.restored)
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-coverage", note)
	return f.merged
}

// uncoveredStepNums lists the 1-based plan steps that no check attaches to, in plan order.
func uncoveredStepNums(covered map[int]bool, total int) []string {
	out := make([]string, 0, total)
	for i := 1; i <= total; i++ {
		if !covered[i] {
			out = append(out, fmt.Sprint(i))
		}
	}
	return out
}

// coverageJSONOnlyReminder is appended to the fill system prompt after a reply that carried no
// parseable checks array — the same "strip all prose" nudge the planner and the check review use.
const coverageJSONOnlyReminder = "YOUR PREVIOUS REPLY COULD NOT BE PARSED. It carried no JSON array of checks — " +
	"prose, a wrapping object, or an unterminated array. Reply with the JSON array ONLY: nothing before the `[`, " +
	"nothing after the `]`, no explanation and no code fence."

// coverageAttachReminder is appended after a reply that parsed but closed no gap. That has one cause
// worth stating plainly: a check gates a step only through its `step` field, so checks with labels
// outside the plan's range, non-numeric labels, or labels repeating an already-covered step are
// authored work that gates nothing. Naming the uncovered steps by number turns an abstract instruction
// ("cover every producing step") into a list to answer. The escape hatch is kept open on purpose —
// inventing a check for a step that writes nothing is worse than leaving it uncovered.
func coverageAttachReminder(uncovered []string, total int) string {
	n := fmt.Sprint(total)
	return "YOUR PREVIOUS REPLY ADDED NO COVERAGE. These plan steps still have NO check: " +
		strings.Join(uncovered, ", ") + " — step numbers are 1-based positions in the plan order, and this plan has " +
		n + " step(s). A check gates a step ONLY through its `step` field: a label outside 1.." + n + ", a " +
		"non-numeric label, or a label repeating an already-covered step attaches to nothing, however good the " +
		"command is. Return the existing checks UNCHANGED plus one NEW check for EACH listed step that PRODUCES an " +
		"artifact, with `step` set to that step's number. If a listed step is pure investigation and writes nothing, " +
		"it genuinely needs no check — leave it out rather than inventing one."
}

// unionChecks returns fill with every authored check it lost appended back, and how many that was.
// Identity is the trimmed command — the text that actually runs — because the fill is instructed to
// return the existing checks UNCHANGED, so a command absent from its reply was dropped rather than
// rewritten (repairing a flawed check is validateChecks' job, a separate pass). A commandless
// authored check is skipped: there is nothing to run, and every one of them would collide on "".
func unionChecks(fill, authored []council.DeliverableCheck) (out []council.DeliverableCheck, restored int) {
	out = append(make([]council.DeliverableCheck, 0, len(fill)+len(authored)), fill...)
	have := make(map[string]bool, len(out))
	for _, c := range out {
		have[strings.TrimSpace(c.Command)] = true
	}
	for _, c := range authored {
		k := strings.TrimSpace(c.Command)
		if k == "" || have[k] {
			continue
		}
		out, have[k], restored = append(out, c), true, restored+1
	}
	return out, restored
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
	"ACTUALLY produces, not the request's raw spelling. A generator whose target language forbids a character in the " +
	"source name substitutes a legal one, so the emitted file is NOT spelled like its input — and a check demanding the " +
	"input's spelling can NEVER pass and fights the toolchain (the agent renames to satisfy the grep, which breaks " +
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
	"Verify the step's stated deliverable, not an intermediate. This is ENFORCED, not advisory: the check shell " +
	"BLOCKS mutating commands at run time (build drivers and compilers, package installers, `rm`/`mv`, archive " +
	"create/extract, `git` write subcommands), and a blocked check returns NO verdict — so leaving one in place " +
	"does not merely waste time, it silently removes the gate.\n" +
	"- RECORD AND READ (the shape to repair TOWARD): the STEP runs, the CHECK reads. Whenever the proof needs " +
	"something RUN — a build, a test command, the produced program on an input, a server round-trip — that run is " +
	"the STEP's work: it performs the run once and saves the REAL output to a result file at a fixed path, and the " +
	"check READS that file (`grep -q '<expected outcome>' <result file>`, or a python3 parse of it). Rewrite an " +
	"executing check into that shape and say in its `deliverable` text that the step must save the file, naming the " +
	"SAME path in both. What is asserted does not change — the run still happens and its actual output is judged — " +
	"but the check can no longer be refused by the read-only shell, cannot re-do the work each gate cycle, and fails " +
	"honestly through `grep`'s exit status. Leave a command running DIRECTLY in a check only when it merely INSPECTS " +
	"what already exists and changes nothing (`test -s f`, `grep -q pat f`, `tar -tzf f.tgz`). Keep the " +
	"deliverable's meaning; change only how it is proven.\n" +
	"- Preserve each check's `step` label exactly — it scopes the check to its step. A cleanup/absence check " +
	"(`test ! -f a.tgz`) MUST keep its own step label; never merge it onto the same step as an existence check " +
	"(`test -s a.tgz`) for the same artifact — they are verified at different steps, and co-locating them makes a " +
	"jointly-unsatisfiable checklist. Keep `expect` ONLY when it reliably matches correct output; " +
	"otherwise drop `expect` and rely on the exit code — but ONLY when the command's exit code can actually " +
	"FAIL. A pipeline reports its LAST stage's status, so anything ending in a filter (`| head`, `| tail`, " +
	"`| tee`, `| cat`, `| sort`) exits 0 whatever the predicate found — even when the file it read does not " +
	"exist. Dropping `expect` there does not simplify the check, it makes it unfalsifiable: it will pass on " +
	"a broken deliverable and on no deliverable at all. Restructure the command so failure is its exit " +
	"status (`grep -q PATTERN FILE`, `test`, or `CMD && echo <marker>` with `expect` on the marker) instead " +
	"of leaving a check that cannot fail.\n" +
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
	if !ok || len(out) == 0 {
		out, ok = a.retryCheckAudit(ctx, agent, s, model, string(in), raw, ok, len(checks))
	}
	// Still unusable → keep the authored checks rather than drop the contract. retryCheckAudit has
	// already reported both attempts (each with its reply), so this path stays silent; the one thing
	// that must never happen here is silence overall — the authored checks would then be gated on
	// unreviewed, while the log looked exactly like a review that found nothing to repair, and the
	// side call leaves no session record to check afterwards.
	if !ok || len(out) == 0 {
		return checks
	}
	out = a.ensureRunnableChecks(ctx, agent, s, model, string(in), out)
	out = a.restoreDroppedExpects(s.ID, checks, out)
	a.recordCheckAudit(ctx, s.ID, checks, out)
	return out
}

// ensureRunnableChecks re-asks ONCE when the reviewed set still contains a check the read-only check
// shell will refuse.
//
// The review prompt already forbids a check that performs the step's work, and the shell already
// refuses one at run time. Both were in place and a check that runs a build still survived a live
// review — and the two layers combine into the worst outcome rather than a safety net: the refusal
// exits 126, which every gate reads as "no verdict" (checkUnrunnable), so the step is neither failed
// nor proven. Nothing tells the agent, either — the refusal is a transient progress line, and the
// agent's OWN shell is unwrapped, so it runs the same command, watches it succeed, and has no reason
// to call substitute_check. A step ends the run ungated while the log looks like a clean review.
//
// So the prediction is made deterministically (refusedCommandsIn) at the one moment the checks are
// still cheap to change, and the re-ask NAMES the offending commands — the same shape as the coverage
// re-ask: an abstract rule the model already ignored once becomes a specific list to answer. Bounded
// at one extra call, and the result is taken only if it actually reduces the blocked count, with the
// unblocked checks unioned back so a retry cannot quietly shrink the contract.
func (a *App) ensureRunnableChecks(ctx context.Context, agent AgentSpec, s session.Session, model, in string,
	out []council.DeliverableCheck) []council.DeliverableCheck {
	if !readOnlyChecksEnabled() || len(out) == 0 {
		return out // guard off → the command is not refused, so there is nothing to predict
	}
	blocked := blockedCheckDescs(out)
	if len(blocked) == 0 {
		return out
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
		fmt.Sprintf("check-audit: %d check(s) still run a command the check shell refuses (%s) — each returns NO "+
			"verdict, leaving its step ungated rather than failed; re-asking once", len(blocked), strings.Join(blocked, "; ")))
	raw := a.specMineCall(ctx, agent, s.ID, "check-audit", model,
		validateChecksSystem+"\n\n"+readOnlyRepairReminder(blocked), in)
	retry, ok := parseChecksArray(raw)
	if !ok || len(retry) == 0 {
		a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
			"check-audit: the re-ask returned no usable checks — keeping the reviewed set, and the blocked check(s) "+
				"will yield no verdict")
		return out
	}
	// Union the retry with the checks that were NOT blocked: the reply is told to return everything, but
	// a model answering "repair these" by returning only those would drop every working check otherwise.
	// The blocked ones are deliberately excluded from the restore — their repaired forms are the point.
	var kept []council.DeliverableCheck
	for _, c := range out {
		if len(refusedCommandsIn(c.Command)) == 0 {
			kept = append(kept, c)
		}
	}
	merged, _ := unionChecks(retry, kept)
	still := blockedCheckDescs(merged)
	if len(still) >= len(blocked) {
		a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
			fmt.Sprintf("check-audit: the re-ask did not reduce the refused check(s) (%d → %d) — keeping the reviewed "+
				"set; those step(s) land ungated", len(blocked), len(still)))
		return out
	}
	note := fmt.Sprintf("check-audit: repaired %d of %d check(s) the check shell would refuse", len(blocked)-len(still), len(blocked))
	if len(still) > 0 {
		note += " — still refused: " + strings.Join(still, "; ")
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-audit", note)
	return merged
}

// blockedCheckDescs names each check whose command the read-only shell would refuse, as
// "step N `command` (refused: name)" — the step label and the command are both needed for the reply to
// know which check to repair, and the refused name is what makes the verdict arguable rather than a
// bare assertion.
func blockedCheckDescs(checks []council.DeliverableCheck) []string {
	var out []string
	for _, c := range checks {
		names := refusedCommandsIn(c.Command)
		if len(names) == 0 {
			continue
		}
		step := strings.TrimSpace(c.Step)
		if step == "" {
			step = "?"
		}
		out = append(out, fmt.Sprintf("step %s `%s` (refused: %s)", step,
			clipLine(strings.TrimSpace(c.Command), 90), strings.Join(names, ", ")))
	}
	return out
}

// readOnlyRepairReminder is appended to the review prompt for the single re-ask. It states the
// consequence the model cannot observe (a refused check is skipped, not failed), lists the offenders,
// and names the one repair that always applies: the run belongs to the STEP, which saves its real
// output, and the check reads what it saved.
func readOnlyRepairReminder(blocked []string) string {
	return "YOUR PREVIOUS REPLY LEFT " + fmt.Sprint(len(blocked)) + " CHECK(S) THAT THE CHECK SHELL REFUSES TO RUN:\n" +
		"- " + strings.Join(blocked, "\n- ") + "\n" +
		"This is not a style preference. The shell that executes checks shadows the commands that build, install, " +
		"fetch, move or delete, so each of those checks exits 126 and the gate records NO verdict for it — the step " +
		"lands neither proven nor failed, and nothing in the log looks wrong. Return EVERY check again, each refused " +
		"one repaired into the shape that always works — RECORD AND READ: the STEP runs, the CHECK reads.\n" +
		"  · The run you put in the check is the STEP's work: say in that check's `deliverable` text that the step " +
		"must perform it once and save the REAL output to a result file at a fixed path, and make the `command` a " +
		"READ of that file (`grep -q '<expected outcome>' <result file>`, or a python3 parse of it). Name the SAME " +
		"path in the deliverable text and in the command.\n" +
		"  · What you assert does not change — the run still happens and its actual output is judged. Only the check " +
		"stops re-doing it.\n" +
		"  · If the proof needs nothing run at all, read the artifact directly instead (`test -s`, `grep -q`, list an " +
		"archive rather than extracting it).\n" +
		"Do NOT drop a check to satisfy this — dropping leaves the step ungated, which is exactly the outcome being " +
		"repaired. Leave every other check unchanged. JSON array only, no prose, no code fence."
}

// restoreDroppedExpects puts back an `expect` the review removed from a command whose EXIT STATUS
// cannot report failure — and only then.
//
// The review is allowed to drop an expect that cannot reliably match, and it should: Passes then
// judges on the exit code alone. That is a genuine repair whenever the exit code is a real signal.
// It is not one when the command's last stage succeeds no matter what it read: `grep -n P ./F |
// head -1` and `cmd > log; echo $?` both exit 0 whether the deliverable is right, wrong, or absent.
// Dropping the expect there does not simplify the check, it makes it unfalsifiable — the silent
// mirror of the permanent false failure the review exists to catch, since the gate reports a pass.
//
// Restoring on the command being byte-identical instead — the first cut of this — was wrong in both
// directions, and the live run showed both within one check set:
//
//	test -f X && test -s X          expect `.+`   the review dropped it; `test` prints NOTHING, so
//	                                              `.+` can never match. The drop was the repair, and
//	                                              putting it back re-armed a permanent false failure.
//	find … -exec grep -l … | head   expect `.+`   the review REWROTE the command and dropped it,
//	                                              leaving exit 0 in every world state — untouched by
//	                                              an identical-command rule, and the exact defect
//	                                              this function exists for.
//
// So the test is exitCodeMasked, not sameness. A rewritten command still gets its prior expect back
// when it is masked, matched on step+deliverable — but only an UNANCHORED one: `^…$` against shell
// output that ends in a newline is the documented never-matches shape, and guessing it onto a
// command whose text changed would trade a check that cannot fail for one that cannot pass.
func (a *App) restoreDroppedExpects(sid session.SessionID, before, after []council.DeliverableCheck) []council.DeliverableCheck {
	byCmd := make(map[string]string, len(before))
	byLabel := make(map[string]string, len(before))
	for _, c := range before {
		e := strings.TrimSpace(c.Expect)
		if e == "" {
			continue
		}
		byCmd[strings.TrimSpace(c.Command)] = e
		if !strings.ContainsAny(e, "^$") {
			byLabel[checkLabelKey(c)] = e
		}
	}
	if len(byCmd) == 0 {
		return after
	}
	restored := 0
	for i := range after {
		if strings.TrimSpace(after[i].Expect) != "" || !exitCodeMasked(after[i].Command) {
			continue
		}
		e, ok := byCmd[strings.TrimSpace(after[i].Command)]
		if !ok {
			e, ok = byLabel[checkLabelKey(after[i])]
		}
		if ok {
			after[i].Expect = e
			restored++
		}
	}
	if restored > 0 {
		a.emitToolProgress(sid, plannerActor, "", "check-audit",
			fmt.Sprintf("check-audit: restored `expect` on %d check(s) whose command cannot report failure "+
				"through its exit code — judged on that alone they would pass with nothing produced", restored))
	}
	return after
}

// checkLabelKey identifies the check a review rewrote the command of: same step, same deliverable.
func checkLabelKey(c council.DeliverableCheck) string {
	return strings.TrimSpace(c.Step) + "\x00" + strings.ToLower(strings.TrimSpace(c.Deliverable))
}

// maskingStageCmds are commands that succeed on whatever they are handed. As a pipeline's last
// stage or a list's last statement they overwrite the status of everything before them, so the
// check's exit code stops carrying any information about the deliverable.
var maskingStageCmds = map[string]bool{
	"head": true, "tail": true, "cat": true, "tee": true, "sort": true, "uniq": true,
	"wc": true, "cut": true, "tr": true, "rev": true, "nl": true, "sed": true, "awk": true,
	"echo": true, "printf": true, "true": true, ":": true,
}

// exitCodeMasked reports whether a shell command's exit status has stopped being a failure signal.
//
// A pipeline reports its LAST stage and a list its LAST statement, so only the trailing command
// matters: `grep -q P F` and `test -f F` fail honestly, `… | head -1` and `…; echo $?` do not.
// Deliberately naive about quoting and substitution — it decides whether to put an expect back, so
// a wrong "masked" costs one restored assertion and a wrong "faithful" leaves today's behavior.
func exitCodeMasked(cmd string) bool {
	c := strings.TrimRight(strings.TrimSpace(cmd), ";& \t\n")
	// The trailing command is whatever follows the last separator: | || && ; newline.
	last := c
	for _, sep := range []string{"||", "&&", "|", ";", "\n"} {
		if i := strings.LastIndex(last, sep); i >= 0 {
			if t := strings.TrimSpace(last[i+len(sep):]); t != "" {
				last = t
			}
		}
	}
	fields := strings.Fields(last)
	if len(fields) == 0 {
		return false
	}
	// Strip a leading env assignment or `command`/`exec` wrapper, then take the bare name.
	name := fields[0]
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return maskingStageCmds[name]
}

// retryCheckAudit re-asks the review ONCE when the first reply yielded no usable check set, and
// returns what the second attempt produced (nil,false when it fails too).
//
// A single unusable reply used to end the review silently-in-effect: the authored checks were kept
// UNREVIEWED, which is safe but means the pass simply did not happen. That is how four checks that
// each rebuilt an entire compiler reached the gate — the audit answered `[]`, the run kept them, and
// every gate cycle re-ran the build. One retry is cheap (a bounded side call, only on the failure
// path) against a pass whose whole job is to catch exactly that.
//
// The two failure shapes are NOT the same defect and get different reminders. Conflating them also
// made the log lie: an `[]` reply was reported as "unusable (2 chars)" when it parsed perfectly well
// and said something specific.
//   - Unparseable: no checks array in the reply (prose, a wrapping object, a cut-off stream). Ask for
//     the bare array, exactly as the planner does after an unparseable plan.
//   - Every check dropped: valid JSON that asks to keep nothing. Honoring it would store no checks at
//     all (storePlanChecks returns early on an empty set), so the plan would land with NO executable
//     gate — strictly worse than the flawed checks the review was meant to repair. Say that, and ask
//     for repairs instead.
func (a *App) retryCheckAudit(ctx context.Context, agent AgentSpec, s session.Session, model, in, raw string, parsed bool, authored int) ([]council.DeliverableCheck, bool) {
	reminder := checkAuditJSONOnlyReminder
	why := fmt.Sprintf("reply is not a checks array (%d chars) — retrying JSON-only", len(raw))
	if parsed {
		reminder = checkAuditKeepSomeReminder
		why = fmt.Sprintf("review asked to drop all %d check(s) — retrying: dropping every check removes the gate "+
			"instead of repairing it", authored)
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
		fmt.Sprintf("check-audit: %s :: %s", why, planParseExcerpt(raw)))

	raw2 := a.specMineCall(ctx, agent, s.ID, "check-audit", model, validateChecksSystem+"\n\n"+reminder, in)
	out, ok := parseChecksArray(raw2)
	if !ok || len(out) == 0 {
		// Terminal line for this pass, and it carries the retry's reply: the caller stays silent after
		// this, so everything needed to tell the two shapes apart has to be here.
		a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
			fmt.Sprintf("check-audit: retry also unusable (%d chars, parsed=%t) — keeping the %d authored check(s) "+
				"unreviewed :: %s", len(raw2), ok, authored, planParseExcerpt(raw2)))
		return nil, false
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
		fmt.Sprintf("check-audit: retry returned %d check(s)", len(out)))
	return out, true
}

// checkAuditJSONOnlyReminder is appended to the review system prompt after a reply that carried no
// parseable checks array — the same "strip all prose" nudge the planner uses, for the same reason:
// weak models bury the array under reasoning, or ramble until the output budget truncates it.
const checkAuditJSONOnlyReminder = "YOUR PREVIOUS REPLY COULD NOT BE PARSED. It carried no JSON array of checks — " +
	"prose, a wrapping object, or an unterminated array. Reply with the JSON array ONLY: nothing before the `[`, " +
	"nothing after the `]`, no explanation and no code fence."

// checkAuditKeepSomeReminder is appended after a reply that dropped every check. It states the
// consequence, because the model has no way to know it: these checks are the only executable gate,
// so an empty answer does not remove a bad gate, it removes the gate.
const checkAuditKeepSomeReminder = "YOUR PREVIOUS REPLY DROPPED EVERY CHECK, and that is not an available answer. " +
	"These checks are the only executable gate on this plan: returning none does not remove a bad gate, it removes " +
	"the gate, and the plan then lands with nothing verified. REPAIR them instead — strengthen a check that is too " +
	"weak into one that exercises the contract; turn a check that re-does the step's work into a read-only read of " +
	"what that work produced; replace a missing tool with a portable equivalent. Drop an entry only when its " +
	"DELIVERABLE is not something this plan produces at all. If that were true of every entry there would have been " +
	"nothing to review, so an empty array will be ignored a second time and the unrepaired checks will be used as-is."

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
	text, cut := drainStream(stream)
	if cut != nil {
		a.emitToolProgress(s.ID, plannerActor, "", "criteria",
			fmt.Sprintf("criteria: the reply was CUT OFF after %d chars — %v (the contract may be incomplete)", len(text), cut))
	}
	return strings.TrimSpace(text)
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
