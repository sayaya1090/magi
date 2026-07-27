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

const coverageFillSystem = "You author deliverable `checks` that verify a plan's per-step outputs, FILLING GAPS. " +
	"You are given the plan STEPS (numbered in order), the TASK, and the checks authored SO FAR. Some steps that " +
	"PRODUCE a deliverable currently have NO check, so the completion gate cannot verify them. Return a JSON array = the " +
	"existing checks UNCHANGED, PLUS one NEW check for EACH producing step that lacks one.\n" +
	"A CHECK IS DATA, NOT A COMMAND: it is {step, deliverable, source, assert}, and the gate READS `source` and " +
	"applies `assert` to it itself, with no shell in the path — so a check can never re-do the step's work, mutate " +
	"anything, or fail because a tool is missing. `assert` must be drawn from this FIXED vocabulary:\n" +
	"  nonempty          — `source` exists and is not blank\n" +
	"  matches <regexp>  — the content of `source` matches this regular expression\n" +
	"  absent <regexp>   — the content of `source` does NOT match it\n" +
	"  equals <path>     — `source` has the same content as that other file\n" +
	"  port_open <port>  — something is listening on that port right now (`source` unused)\n" +
	"  process_alive     — the pid written in `source` is running right now\n" +
	"For every NEW check:\n" +
	"- SCOPE by step: set `step` to that step's 1-based position in the plan order (\"3\"), so it gates only that " +
	"step. The other authoring prompt shows `step` as a string; either shape is read, but keep them consistent.\n" +
	"- RECORD AND READ: whenever proving the deliverable needs something RUN — a build, a test command the task " +
	"names, the produced program on its input, a server round-trip — that run belongs to the STEP, which performs it " +
	"ONCE and redirects its REAL output to a fixed path in the workspace; the check's `source` is that SAME path. Say " +
	"in the `deliverable` text that the step must save that file and that it must be the command's own redirected " +
	"output, never hand-written, and name the identical path in both.\n" +
	"- EXERCISE the deliverable (precondition is not proof): reaching the artifact — a file exists, a port accepts a " +
	"connection, a module imports, a build succeeds, a process is alive — is a precondition, NOT proof of the contract; " +
	"a non-functional stub passes all of them. When the step's artifact must DO something (answer a request, return a " +
	"value, transform an input, produce an output), have the step invoke that behavior through the same interface its " +
	"consumer uses, record the result, and assert on THAT — choosing the weakest input that still forces the real code " +
	"path so a stub that merely exists or opens the port FAILS. A command that SUCCEEDED is the same kind of " +
	"precondition — a configure/build/install exiting 0 with a flag on its command line proves the flag was ACCEPTED, " +
	"not that it took EFFECT — so when the step's deliverable is the effect a setting is supposed to cause, have the " +
	"step run whatever consumes the setting and assert the resulting artifact appears AT THE LOCATION the task names.\n" +
	"- NECESSITY: assert only what the task itself states. Never pin a version, path, timestamp or incidental " +
	"attribute the task did not specify — over-specification false-fails correct work and never converges.\n" +
	"- A pure investigation/read-only step (it writes no artifact) needs NO check — do NOT invent one for it.\n" +
	"Do NOT alter or drop the existing checks, and do NOT change what any check verifies. JSON array only, no prose, no code fence."

// ensureStepCoverage guarantees every producing plan step has at least one deliverable check. The
// plan audit authors delib.Checks with no coverage guarantee — a weak model emits one check for a
// many-step plan, and runStepGate only verifies steps that appear in the check set, so the rest land
// unverified. When the authored checks cover fewer distinct steps than the plan has, a single gap-fill
// pass authors checks for the uncovered producing steps (read-only steps are told to be skipped, so an
// unfillable gap simply returns the same set). A fill that comes back unparseable, or that leaves ANY
// step still uncovered, is re-asked ONCE with a reminder naming what went wrong — at most two calls,
// never a loop. The 0-step solo path passes a single synthetic step, so it gets one check for its
// objective the same way.
// Best-effort: a disabled flag, no gap, a transport failure, or a second reply that still adds no
// distinct step coverage returns the input UNCHANGED. A reply that DOES add coverage is merged with the authored set rather than
// replacing it (unionChecks), so the fill can only ever add — it never weakens or blocks the contract
// it was called to complete. What it cannot do is call a PARTLY closed gap done: coverage that merely
// GREW is kept, but it is re-asked for first and then reported with the still-ungated steps named.
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
			fmt.Sprintf("check-coverage: gap NOT closed (%s) — %s; those steps land unverified", gap, why))
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
	// Two different questions, and conflating them is what let a half-filled gap read as a success.
	// gained asks whether an attempt is worth KEEPING (the merge only ever adds, so any growth beats
	// the input). closed asks whether the pass is DONE — every step gated — which is the only thing
	// that should end it without a second try.
	gained := func(f fillAttempt) bool { return f.parsed && len(f.newCov) > len(covered) }
	closed := func(f fillAttempt) bool { return f.parsed && len(f.newCov) >= len(steps) }
	// covOf is what an attempt LEFT covered, so the re-ask reminder asks for the steps still missing
	// rather than re-listing ones the first reply already gated.
	covOf := func(f fillAttempt) map[int]bool {
		if !f.parsed {
			return covered
		}
		return f.newCov
	}
	// describe names WHICH of the two failures an attempt was, in the terms they actually differ by.
	// The retry note and the final shortfall both need it and must agree, so it is written once.
	describe := func(f fillAttempt) string {
		if !f.parsed {
			// The reply itself, not just its length: this failure was observed three runs running and
			// could not be diagnosed from the log, since only what came back tells "the model answered
			// with prose" from "it wrapped the array in an object" from "an element had the wrong
			// shape". The re-ask now appends that report and persists the reply verbatim, so this says
			// what the failure WAS and lets the record carry the evidence.
			return fmt.Sprintf("the fill reply did not parse as a checks array (%d chars)", len(f.raw))
		}
		if gained(f) {
			// It attached SOMETHING and still did not finish. That is a third failure, and the two
			// messages below would both be false about it — a note saying "none attach" reads as a
			// broken reply when the reply was half right, and asks the model to fix what it did not
			// do. Name the residue instead, which is also all the re-ask still wants.
			return fmt.Sprintf("the fill covered %d→%d of %d step(s); step(s) %s still have none",
				len(covered), len(f.newCov), len(steps),
				strings.Join(uncoveredStepNums(f.newCov, len(steps)), ", "))
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

	first := run(coverageFillSystem)
	// best is the widest attempt seen. On a double failure reask hands back the FIRST reply, but
	// coverage that WAS gained must not be thrown away over the part that was not — the merge only
	// ever adds, so the widest attempt is strictly better than the input, whichever attempt it was.
	best := first
	// Re-ask ONCE. A confirmed gap that the fill did not close leaves plan steps with no gate at all,
	// and a single degraded reply used to end the pass. Note what is judged here: not "did the reply
	// parse", and not "did coverage GROW" either — but "is every step gated". Growth was the old
	// criterion and it is satisfied by one check on a six-step plan: observed live as
	// `2→4 checks, 2→4 step(s) covered (6 plan step(s))`, which ended the pass, read exactly like a
	// complete fill, and left two steps with no definition of done for the worker that ran them.
	f, ok := reask[fillAttempt]{
		pass:  "check-coverage",
		actor: plannerActor,
		ask: func(system string) (fillAttempt, string, bool) {
			r := run(system)
			if r.parsed && len(r.newCov) > len(best.newCov) {
				best = r
			}
			return r, r.raw, r.parsed
		},
		defect: func(r fillAttempt, _ bool, _ string) string {
			if closed(r) {
				return ""
			}
			return describe(r)
		},
		// The two failures are different defects, and a reminder that names the wrong one asks the
		// model to fix something it did not do: an unparsed reply needs to be told to answer in JSON,
		// while a reply that parsed and attached nothing needs the uncovered steps BY NUMBER.
		reminder: func(_ string, parsed bool) string {
			if parsed {
				return coverageFillSystem + "\n\n" +
					coverageAttachReminder(uncoveredStepNums(covOf(first), len(steps)), len(steps))
			}
			return coverageFillSystem + "\n\n" + coverageJSONOnlyReminder
		},
		probe: func(b []byte) error { var cs []council.DeliverableCheck; return json.Unmarshal(b, &cs) },
		// Honest about the ambiguity: after a re-ask that listed the uncovered steps by number, a second
		// empty answer is as likely to mean "those steps produce nothing checkable" (the prompt tells it
		// to skip pure investigation steps) as it is to mean the fill failed. Either way those steps
		// land unverified, which is what this reports.
		fallback: fmt.Sprintf("gap NOT closed (%s at the start), and this was the re-ask, which listed the "+
			"uncovered step(s) by number — either they produce nothing checkable or the fill could not do "+
			"it; whatever coverage the replies DID add is kept, and the outcome line names the step(s) "+
			"that land unverified", gap),
	}.run(ctx, a, s.ID, first, first.raw, first.parsed)
	if !ok {
		// Neither attempt gated every step. Keep the coverage that WAS won — reverting to the authored
		// set would leave steps ungated that a usable reply had just covered, and this pass can only
		// add. The note below is then the honest one: partly filled, and here is what is still open.
		f = best
		if !gained(f) {
			return checks
		}
	}
	note := fmt.Sprintf("check-coverage: %d→%d checks, %d→%d step(s) covered (%d plan step(s))",
		len(checks), len(f.merged), len(covered), len(f.newCov), len(steps))
	if f.restored > 0 { // the fill dropped authored checks and they were put back — say so, it is not a no-op
		note += fmt.Sprintf("; restored %d authored check(s) the fill had dropped", f.restored)
	}
	// A partial fill must never read like a complete one. Growth alone used to end the pass and print
	// this line, so the reader had to subtract two numbers to notice that steps were landing with no
	// definition of done — and nothing named WHICH. The gate that skips them is silent by design
	// (runStepGate only verifies steps a check attaches to), so this line is the only place it shows.
	if left := uncoveredStepNums(f.newCov, len(steps)); len(left) > 0 {
		note += fmt.Sprintf("; step(s) %s still have NO check and land unverified", strings.Join(left, ", "))
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

// coverageAttachReminder is appended after a reply that parsed and still left steps ungated — whether
// it attached nothing or only some. It opens on the RESIDUE rather than on "you added nothing", which
// would be false for a half-filled gap and would ask the model to fix something it did not do. One
// cause is worth stating plainly: a check gates a step only through its `step` field, so checks with
// labels outside the plan's range, non-numeric labels, or labels repeating an already-covered step are
// authored work that gates nothing. Naming the uncovered steps by number turns an abstract instruction
// ("cover every producing step") into a list to answer. The escape hatch is kept open on purpose —
// inventing a check for a step that writes nothing is worse than leaving it uncovered.
func coverageAttachReminder(uncovered []string, total int) string {
	n := fmt.Sprint(total)
	return "YOUR PREVIOUS REPLY LEFT PLAN STEPS UNGATED. These plan steps still have NO check: " +
		strings.Join(uncovered, ", ") + " — step numbers are 1-based positions in the plan order, and this plan has " +
		n + " step(s). A check gates a step ONLY through its `step` field: a label outside 1.." + n + ", a " +
		"non-numeric label, or a label repeating an already-covered step attaches to nothing, however good the " +
		"command is. Return the existing checks UNCHANGED plus one NEW check for EACH listed step that PRODUCES an " +
		"artifact, with `step` set to that step's number. If a listed step is pure investigation and writes nothing, " +
		"it genuinely needs no check — leave it out rather than inventing one."
}

// unionChecks returns fill with every authored check it lost appended back, and how many that was.
// Identity is what the check verifies (checkIdent: the command text, or a typed check's
// assertion+source) because the fill is instructed to return the existing checks UNCHANGED, so an
// identity absent from its reply was dropped rather than rewritten (repairing a flawed check is
// validateChecks' job, a separate pass). A check that verifies nothing at all is skipped.
func unionChecks(fill, authored []council.DeliverableCheck) (out []council.DeliverableCheck, restored int) {
	out = append(make([]council.DeliverableCheck, 0, len(fill)+len(authored)), fill...)
	have := make(map[string]bool, len(out))
	for _, c := range out {
		have[checkIdent(c)] = true
	}
	for _, c := range authored {
		k := checkIdent(c)
		if k == "" || have[k] {
			continue
		}
		out, have[k], restored = append(out, c), true, restored+1
	}
	return out, restored
}

const validateChecksSystem = "You review the deliverable `checks` a planning council authored, BEFORE they are " +
	"used to gate a task. A CHECK IS DATA, NOT A COMMAND: it is {step, deliverable, source, assert}, and the gate " +
	"READS `source` and applies `assert` to it itself, with no shell in the path. `assert` must be drawn from this " +
	"FIXED vocabulary — any other wording is not understood and the check then yields NO verdict at all:\n" +
	"  nonempty          — `source` exists and is not blank\n" +
	"  matches <regexp>  — the content of `source` matches this regular expression\n" +
	"  absent <regexp>   — the content of `source` does NOT match it\n" +
	"  equals <path>     — `source` has the same content as that other file\n" +
	"  port_open <port>  — something is listening on that port right now (`source` unused)\n" +
	"  process_alive     — the pid written in `source` is running right now\n" +
	"Return ONLY a JSON array of the checks, REPAIRED where flawed and DROPPING any that cannot be made valid. Apply:\n" +
	"- CONVERT (do this FIRST): a check still written as a shell `command`, with or without an `expect`, gates " +
	"NOTHING — commands are no longer executed. Rewrite it into {source, assert} and omit `command`/`expect` from " +
	"the object you return. The run the command performed belongs to the STEP: say in the `deliverable` text that " +
	"the step must perform that run ONCE and redirect its REAL output to a fixed path, set `source` to that path, " +
	"and move the old `expect` — or the pattern the command grepped for — into `matches`. `test -s f` or `test -f " +
	"f` becomes source `f`, assert `nonempty`. A socket/curl port probe becomes `port_open <port>`. A diff against " +
	"a reference file becomes `equals <path>`. An exit-status-only command becomes a step that appends its status " +
	"(`<cmd> > out.log 2>&1; echo \"exit=$?\" >> out.log`) and assert `matches ^exit=0$`. Keep WHAT was proven — " +
	"change only how it is expressed.\n" +
	"- NECESSITY (no over-demand): assert ONLY what the task's own contract requires. NEVER pin a value the task did " +
	"not itself state — a specific version, build id, exact path, timestamp, or incidental attribute — and never " +
	"demand more than the stated outcome. Over-specification false-fails a CORRECT deliverable and can never converge " +
	"on an environment that differs in that incidental. Narrow each check to the minimal condition that proves the " +
	"objective: for an installed dependency assert it is importable/usable, not an exact version, UNLESS the task pins " +
	"one; drop or loosen any pinned specific the task did not require.\n" +
	"- MATCHABLE: a `matches`/`absent` pattern must be one the recorded output CAN actually contain. A pattern " +
	"written for text the step never records — or for a shape the run does not produce — false-fails forever. Prefer " +
	"the smallest distinctive fragment of the real outcome over a long transcribed line, and do not anchor to a " +
	"position in the file you cannot know.\n" +
	"- TOOL-DERIVED NAMES: when a check reads or matches a file a code generator EMITS, use the name the tool " +
	"ACTUALLY produces, not the request's raw spelling. A generator whose target language forbids a character in the " +
	"source name substitutes a legal one, so the emitted file is NOT spelled like its input — and a check demanding the " +
	"input's spelling can NEVER pass and fights the toolchain (the agent renames to satisfy it, which breaks " +
	"the import, then renames back: an unwinnable loop). Rewrite the check to the generator's real output name.\n" +
	"- SEMANTICS, not source spelling (verify meaning by effect; never match the task's prose back against the source): " +
	"a structure or behavior the task states in PROSE — a message/record with named typed fields, a function returning " +
	"a typed value, a format it must accept — is a SEMANTIC to satisfy, NOT a literal string the source file must " +
	"contain. Verify it by having the step EXERCISE the built artifact and recording the result (compile/generate and " +
	"inspect the produced type, run it and capture the typed result), never by matching the SOURCE against the task's " +
	"wording or an INVENTED notation of it — a pseudo-syntax like `<field: type>`, a `^service X$` that forces the name " +
	"alone on a line, a required brace position. The task fixes IDENTIFIERS and VALUES verbatim (a message/service/RPC/" +
	"function NAME, a port, a filename, a pinned version) and those a check MAY assert literally; but a field's " +
	"declaration syntax, a type's spelling, and source layout are the author's to choose, so pinning them false-fails a " +
	"correct artifact and forces the agent to contort valid code toward a fabricated pattern (often one no real " +
	"compiler accepts). Rewrite such a check to assert the EFFECT — the artifact builds and the generated/runtime type " +
	"has that named field — not the surface.\n" +
	"- EXERCISES the deliverable (precondition is not proof): a check that only confirms the deliverable can be " +
	"REACHED — a file exists or is non-empty, a port accepts a connection, a module imports, a build succeeded, a " +
	"process is alive, or a SETTING merely SUPPOSED to produce the deliverable is in place (a build flag configured, " +
	"an env var exported, a config value written) — is too weak, because a non-functional stub, or a configuration " +
	"that never took effect, passes every one of those. When the task states the deliverable must DO something " +
	"(answer a request, return a value, transform an input, produce an output), the STEP must INVOKE that named " +
	"behavior through the same interface its consumer uses and RECORD the result, and the check must assert on that " +
	"result — the endpoint's returned value, the program's output compared to the task's stated mapping — choosing the " +
	"weakest input that still forces the real code path so a stub that merely exists or opens the port FAILS.\n" +
	"  · EFFECT, not its cause: when the deliverable is the EFFECT a configuration is supposed to cause, assert the " +
	"effect recorded after RUNNING what consumes the setting (the artifact the configured build emits appears from a " +
	"fresh run), NOT that the setting is present — a flag that never took effect passes the config check and fails the " +
	"real one.\n" +
	"  · WHOLE standard, not a spot-check: when the task supplies a reference output, an expected dataset, or a " +
	"threshold to meet, assert against that WHOLE standard (`equals` the reference file, or a `matches` on the count/" +
	"fraction clearing the task's stated bar), never a single hand-picked sample — one row that happens to match passes " +
	"a deliverable that is wrong on all the rest.\n" +
	"Do not DROP such a check for being weak; STRENGTHEN it into one that exercises the contract.\n" +
	"- Preserve each check's `step` label exactly — it scopes the check to its step. A cleanup/absence check MUST keep " +
	"its own step label; never merge it onto the same step as an existence check for the same artifact — they are " +
	"verified at different steps, and co-locating them makes a jointly-unsatisfiable checklist.\n" +
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
	a.recordCheckAudit(ctx, s.ID, checks, out)
	return out
}

// ensureRunnableChecks re-asks ONCE when the reviewed set still contains a check with no `assert`.
//
// A check is DATA now — a `source` to read and an assertion from a closed vocabulary — and the runner
// evaluates nothing else. So a check that came back still shaped as a shell command is not a weaker
// check, it is NO check: the runner reports it and returns 126, which every gate reads as "no verdict"
// (checkUnrunnable), and the step lands neither proven nor failed. Nothing in the log looks wrong.
//
// The review prompt already opens with the conversion rule, and one live review is still enough to
// leave a command behind. So the miss is detected deterministically at the one moment the checks are
// still cheap to change, and the re-ask NAMES the offenders — the same shape as the coverage re-ask: an
// abstract rule the model already passed over once becomes a specific list to answer. Bounded at one
// extra call, and the result is taken only if it actually reduces the unasserted count, with the
// already-typed checks unioned back so a retry cannot quietly shrink the contract.
func (a *App) ensureRunnableChecks(ctx context.Context, agent AgentSpec, s session.Session, model, in string,
	out []council.DeliverableCheck) []council.DeliverableCheck {
	if len(out) == 0 {
		return out
	}
	blocked := untypedCheckDescs(out)
	if len(blocked) == 0 {
		return out
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
		fmt.Sprintf("check-audit: %d check(s) came back with no `assert` (%s) — a check is data now, so each of these "+
			"evaluates to NO verdict, leaving its step ungated rather than failed; re-asking once", len(blocked),
			strings.Join(blocked, "; ")))
	raw := a.specMineCall(ctx, agent, s.ID, "check-audit", model,
		validateChecksSystem+"\n\n"+typedRepairReminder(blocked), in)
	retry, ok := parseChecksArray(raw)
	if !ok || len(retry) == 0 {
		a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
			"check-audit: the re-ask returned no usable checks — keeping the reviewed set, and the unasserted check(s) "+
				"will yield no verdict")
		return out
	}
	// Union the retry with the checks that were ALREADY typed: the reply is told to return everything, but
	// a model answering "repair these" by returning only those would drop every working check otherwise.
	// The unasserted ones are deliberately excluded from the restore — their converted forms are the point.
	var kept []council.DeliverableCheck
	for _, c := range out {
		if strings.TrimSpace(c.Assert) != "" {
			kept = append(kept, c)
		}
	}
	merged, _ := unionChecks(retry, kept)
	still := untypedCheckDescs(merged)
	if len(still) >= len(blocked) {
		a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
			fmt.Sprintf("check-audit: the re-ask did not reduce the unasserted check(s) (%d → %d) — keeping the reviewed "+
				"set; those step(s) land ungated", len(blocked), len(still)))
		return out
	}
	note := fmt.Sprintf("check-audit: converted %d of %d check(s) that carried no assertion", len(blocked)-len(still), len(blocked))
	if len(still) > 0 {
		note += " — still unasserted: " + strings.Join(still, "; ")
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-audit", note)
	return merged
}

// untypedCheckDescs names each check the runner cannot evaluate, as "step N <deliverable> (`command`)".
// The step label and the deliverable are what the reply needs to know which check to convert; the
// leftover command is included because it is the material the conversion is made FROM — it says what
// the check was trying to prove.
func untypedCheckDescs(checks []council.DeliverableCheck) []string {
	var out []string
	for _, c := range checks {
		if strings.TrimSpace(c.Assert) != "" {
			continue
		}
		step := strings.TrimSpace(c.Step)
		if step == "" {
			step = "?"
		}
		d := clipLine(strings.TrimSpace(c.Deliverable), 60)
		if cmd := strings.TrimSpace(c.Command); cmd != "" {
			d += fmt.Sprintf(" (`%s`)", clipLine(cmd, 90))
		}
		out = append(out, "step "+step+" "+d)
	}
	return out
}

// typedRepairReminder is appended to the review prompt for the single re-ask. It states the consequence
// the model cannot observe (an unasserted check is skipped, not failed), lists the offenders, and names
// the one conversion that always applies: the run belongs to the STEP, which saves its real output, and
// the check reads what it saved.
func typedRepairReminder(blocked []string) string {
	return "YOUR PREVIOUS REPLY LEFT " + fmt.Sprint(len(blocked)) + " CHECK(S) WITH NO `assert`:\n" +
		"- " + strings.Join(blocked, "\n- ") + "\n" +
		"This is not a style preference. Checks are DATA — the gate reads a `source` file and applies an `assert` " +
		"itself, and it runs no commands at all. A check with no `assert` therefore gates NOTHING: it records no " +
		"verdict, its step lands neither proven nor failed, and nothing in the log looks wrong. Return EVERY check " +
		"again, each of those converted — RECORD AND READ: the STEP runs, the CHECK reads.\n" +
		"  · The run that was in the check is the STEP's work: say in that check's `deliverable` text that the step " +
		"must perform it once and save the REAL output to a result file at a fixed path, set `source` to that path, " +
		"and put what proves success into `assert` (`matches <regexp>` for the expected outcome in that output, " +
		"`nonempty` when the file existing with content is the proof, `equals <path>` against a reference file).\n" +
		"  · For a check that only ever tested reachability: a port probe becomes `port_open <port>`, a pid check " +
		"becomes `process_alive` with `source` set to the pid file.\n" +
		"  · What you assert does not change — the run still happens and its actual output is judged. Only the " +
		"check stops being a command.\n" +
		"Do NOT drop a check to satisfy this — dropping leaves the step ungated, which is exactly the outcome being " +
		"repaired. Leave every other check unchanged. JSON array only, no prose, no code fence."
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
	out, ok := reask[[]council.DeliverableCheck]{
		pass:  "check-audit",
		actor: plannerActor,
		ask: func(system string) ([]council.DeliverableCheck, string, bool) {
			r := a.specMineCall(ctx, agent, s.ID, "check-audit", model, system, in)
			cs, parsed := parseChecksArray(r)
			return cs, r, parsed
		},
		defect: func(cs []council.DeliverableCheck, parsed bool, raw string) string {
			switch {
			case !parsed:
				return fmt.Sprintf("reply is not a checks array (%d chars)", len(raw))
			case len(cs) == 0:
				return fmt.Sprintf("review asked to drop all %d check(s), which removes the gate instead of "+
					"repairing it", authored)
			}
			return ""
		},
		reminder: func(_ string, parsed bool) string {
			if parsed {
				return validateChecksSystem + "\n\n" + checkAuditKeepSomeReminder
			}
			return validateChecksSystem + "\n\n" + checkAuditJSONOnlyReminder
		},
		probe:    func(b []byte) error { var cs []council.DeliverableCheck; return json.Unmarshal(b, &cs) },
		fallback: fmt.Sprintf("keeping the %d authored check(s) unreviewed", authored),
	}.run(ctx, a, s.ID, nil, raw, parsed)
	if !ok {
		return nil, false
	}
	a.emitToolProgress(s.ID, plannerActor, "", "check-audit",
		fmt.Sprintf("check-audit: the re-ask returned %d check(s)", len(out)))
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
	"weak into one that exercises the contract; turn a check that re-does the step's work into a read of what that " +
	"work recorded; give a check that carries no `assert` the assertion that proves its deliverable. Drop an entry only when its " +
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
		afterCmd[checkIdent(c)] = true
	}
	var changed []string
	for _, c := range before {
		if afterCmd[checkIdent(c)] {
			continue // survived verbatim → kept
		}
		d := strings.TrimSpace(c.Deliverable)
		if d == "" {
			d = clipLine(checkWhat(c), 60)
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
// deliverable checks. A check that verifies nothing — neither a command nor an assertion — is
// dropped (there is nothing to run).
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
			if checkIdent(c) != "" {
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
