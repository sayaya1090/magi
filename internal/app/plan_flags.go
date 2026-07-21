package app

import (
	"os"
	"strings"
)

// Bench A/B env knobs for the planner. Each reader parses one MAGI_* env var into a
// bool so a paired ON/OFF run can measure a mechanism in isolation; see each doc for the
// arm it flips and its default. Split out of planner.go for cohesion (behavior unchanged).
//
// Two value shapes, shared by every reader below: a default-ON mechanism is disabled
// only by an explicit off-value (envOff), and a default-OFF mechanism is enabled only
// by an explicit on-value (envOn). Anything else — unset, empty, or unrecognized —
// leaves the default.

// envOff reports whether the named env var holds an explicit off-value.
func envOff(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "off", "false", "no":
		return true
	}
	return false
}

// envOn reports whether the named env var holds an explicit on-value.
func envOn(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

// refineDisabled is a bench A/B knob (mirrors MAGI_MAX_PLAN_DEPTH): MAGI_REFINE=0 downgrades
// refine steps to solo, reproducing the pre-refine baseline (every sub-goal flattened inline)
// so a paired refine-ON/OFF comparison can run on the same task. Default on.
func refineDisabled() bool { return envOff("MAGI_REFINE") }

// stepContextDisabled is a bench A/B knob (mirrors MAGI_REFINE): MAGI_STEP_CONTEXT=0 turns
// OFF the compact context brief injected into delegate hand-offs and read-only fan-out — the
// children then run fully context-free (the pre-brief baseline), so a paired ON/OFF run can
// measure whether the brief helps. Default on. It gates ONLY the brief; it never copies the
// parent conversation (that stays refine's job) — so even ON, delegate/fan-out get an
// overall-goal line plus sibling boundaries/outputs, not the parent's reasoning history.
func stepContextDisabled() bool { return envOff("MAGI_STEP_CONTEXT") }

// adaptDisabled turns OFF the REACTIVE (as-needed) failure re-decomposition — the ADaPT
// mechanism where a step that fails is retried by decomposing it further. MAGI_ADAPT=0 leaves
// only PLANNED decomposition (the up-front hierarchical plan) plus the stall safety net
// (redecomposeStuck): a failed delegate/refine node backtracks after one shot instead of
// spawning informed retries / re-decomposition. This is the recursion-policy A/B knob — with it
// off, magi is closer to HTN-style hierarchical planning with just-in-time sub-planning than to
// ADaPT proper. Default on (=current reactive behavior) so the flag flips the arm, not the base.
func adaptDisabled() bool { return envOff("MAGI_ADAPT") }

// specFidelityEnabled keeps a plan faithful to the request's LITERAL contract — exact field/
// message/function names, output formats, thresholds, literal strings. Deep planning paraphrases
// the instruction, and the executor then normalizes a literal (kv-store-grpc: the request's
// `value` field became `val`, failing a grader that checks verbatim); a shallow/solo run reads the
// raw instruction directly and keeps the literal. This flag turns on three defenses (a planner
// "preserve literals" rule, a plan-time spec-fidelity note, and a verbatim SPEC anchor for the
// context-free delegate child). Default ON; MAGI_SPEC_FIDELITY=0 restores the paraphrase-only
// baseline (the A/B knob).
func specFidelityEnabled() bool { return !envOff("MAGI_SPEC_FIDELITY") }

// specMineEnabled gates the DEDICATED signature-mining side call (specmine.go): a
// tool-free elicitation that extracts, from the request's identifiers and type
// signatures, the requirements the prose leaves unsaid plus the standard idiom for
// that situation — injected as a finished note the executor consumes. Split into its
// own step because a weak executor follows "mine the signature" poorly as one clause
// among many, but consumes a completed conclusion well (the same reason criteria are
// elicited, not instructed). Default ON; MAGI_SPEC_MINE=0 removes the call (A/B knob).
func specMineEnabled() bool { return !envOff("MAGI_SPEC_MINE") }

// execEvidenceEnabled gates the exec-evidence layers: the deterministic per-artifact
// exercise ledger's pre-council nudge ("you never ran what you wrote") plus the
// council-evidence trailer listing authored-but-never-executed files. Targets the
// regression signature where a syntactically complete but never-run deliverable is
// approved (headless-terminal, large-scale; cross-confirmed on another model).
// Non-blocking by design: one nudge, then the fact rides as evidence — the earlier
// BLOCKING evidence gates were removed for bench regression, and this deliberately
// is not one. Default ON; MAGI_EXEC_EVIDENCE=0 restores the baseline (A/B knob).
func execEvidenceEnabled() bool { return !envOff("MAGI_EXEC_EVIDENCE") }

// councilDebateEnabled gates the disagreement-triggered rebuttal round: after the
// members vote independently, a SPLIT (both done and continue) triggers one round
// where each sees the others' rationales and may hold or revise, then a re-tally.
// Motivated by the observed variance in council judgment (the same deliverable
// approved 3-0 on one run, rejected 0-3 on another) — a coin-flip majority stands
// when members disagree for contradictory reasons or one catches a defect the
// others miss. Unanimous votes skip it (no extra call). Default ON;
// MAGI_COUNCIL_DEBATE=0 restores the independent-vote-only tally (A/B knob).
func councilDebateEnabled() bool { return !envOff("MAGI_COUNCIL_DEBATE") }

// councilKeepEnabled asks each council member to ALSO report what the report already gets
// right through its lens — advisory "keep this, don't redo/revert it" surfaced above the fix
// feedback when the turn continues. It never affects the decision or tally. It targets two
// observed weak-model failures: reverting a correct edit because nothing affirmed it (the
// ocaml merge-check fix), and re-verifying already-working parts to exhaustion because nothing
// said they were settled (the kv-store finish spin). Default OFF (A/B): MAGI_COUNCIL_KEEP=1
// enables it; the extra prompt/note tokens ship only when on, keeping the baseline unchanged.
func councilKeepEnabled() bool { return envOn("MAGI_COUNCIL_KEEP") }

// checkpointFirstEnabled turns on test-first ordering: when a task states HOW its
// completion is checked (a snippet, command, function call, or I/O contract), the
// agent is told to FIRST materialize that as a runnable checkpoint in the workdir —
// synthesizing inputs from the spec, including any counter-example it names — and
// then implement until the checkpoint passes, rather than reasoning about a
// verifiable artifact symbolically and never running it (regex-log: a regex rewritten
// 7× with re.findall never once executed). A behavioral nudge on top of the existing
// end-of-turn unverifiedDeliverable backstop, not a replacement. It self-limits to tasks
// that actually state an executable check and steps aside for prose-only conditions, so a
// clean run is not misdirected. Default ON; MAGI_CHECKPOINT_FIRST=off restores the baseline
// that never orders/injects the checkpoint (the A/B knob).
func checkpointFirstEnabled() bool { return !envOff("MAGI_CHECKPOINT_FIRST") }

// stepVerifyEnabled turns on the per-step deliverable contract: the plan-audit council
// authors executable checks for each step's expected deliverable at PLAN time, the solo
// loop runs them deterministically at its finish boundary (runVerifyCmd + regex/exit
// predicate), a passing check deterministically checks off its todo, and when every check
// passes the termination council's open-ended continue path is skipped (the contract was
// settled at planning — no new demands after). A real check FAILURE injects the failing
// command's output as a one-shot continuation nudge; a clean run injects nothing.
//
// Default OFF (regression bisect, 2026-07-16): a weak model authors TRIVIAL checks
// ("file exists", "import succeeds"), all of them pass, and the all-pass path then
// SKIPS the termination council entirely — the live council-skip false-done family
// (cancel-async-tasks ended "council round 0: done — 0 done / 0 continue" with the
// real cancellation edge case never interrogated). MAGI_STEP_VERIFY=1 re-enables.
func stepVerifyEnabled() bool { return envOn("MAGI_STEP_VERIFY") }

// recoveryRunCapEnabled caps stuck-recovery re-decomposition to fire at most once per RUN
// TREE rather than once per depth level: a recovery child is seeded as already-recovered, so
// it cannot trigger its OWN redecomposeStuck (no coder→coder cascade down the depth levels).
// Off (the default), multiple recovery executors are allowed per run tree — each stuck level
// re-arms its own lifeline, bounded by MaxPlanDepth. Set MAGI_RECOVERY_RUNCAP=on to restore
// the one-executor-per-run-tree cap.
func recoveryRunCapEnabled() bool { return envOn("MAGI_RECOVERY_RUNCAP") }

// stuckDecomposeEnabled changes what stuck-recovery (redecomposeStuck) DOES when an agent is
// force-stopped stuck: instead of re-handing the WHOLE task to one fresh child, it decomposes the
// task into an explicit multi-unit TODO list and drives the units ONE AT A TIME — each unit handed
// to a fresh child seeded with the FULL parent context (CloneContext) but scoped to just that one
// unit, its result integrated, and the todo checked off before the next unit starts. This forces
// incremental forward progress: a small scoped unit re-fixates far less than the monolith, and
// units that already landed persist even if a later one stalls. It ALSO widens the recovery gate to
// the "repeat" stall kind (a loop-guard block spiral), which the whole-task re-hand-off could not
// help (it would just re-fixate).
//
// Default ON (2026-07-21): the decomposing recovery is what rescues a pure search/read
// loop — the fix-ocaml-gc and custom-memory-heap-crash bench failures were both an agent
// spiralling on grep/read with zero edits, exactly the fixation the whole-task re-hand-off
// cannot break. It was OFF (2026-07-16 regression bisect) because a "repeat" block during a
// legitimate long wait (compile-compcert: make in background, bash_output polls hard-blocked)
// triggered a full re-plan plus per-unit child spawns MID-BUILD, burning the run's wall clock.
// That regression is now closed at the source: isPollTool (guard.check) counts bash_output /
// wait_for polls toward the environment-wait ratio EVEN WHEN hard-blocked, so a build-poll
// spiral reads as a wait (stallIsWait true) and the existing wait-guard suppresses the recovery
// for the "repeat" kind too — while a genuine fixation (no polls, just re-reads) still recovers.
// MAGI_STUCK_DECOMPOSE=off restores the whole-task re-hand-off (A/B baseline).
func stuckDecomposeEnabled() bool { return !envOff("MAGI_STUCK_DECOMPOSE") }

// implicitAcceptEnabled tells the planner that a task's real acceptance conditions are usually
// stricter than the instruction prose: the exact output tokens/formats the prose only gestures at,
// the standard domain semantics it never spells out (cleanup must still run on cancellation; a
// headless build must not link display libraries), and the edge cases it implies but never lists
// (malformed/empty/boundary inputs, error paths). When on, the planner is asked to surface those
// unstated-but-conventional conditions and fold them into the relevant steps' deliverables, so the
// plan targets the real contract rather than the literal sentence. Complements the orient grounding
// (files present) with domain convention, and pairs with checkpoint-first (the surfaced edge cases
// become the checkpoint's cases). Framed as general edge-case rigor, not a hidden benchmark grader,
// so it applies identically off-bench. Default ON; MAGI_IMPLICIT_ACCEPT=off restores the
// literal-sentence baseline (the A/B knob).
func implicitAcceptEnabled() bool { return !envOff("MAGI_IMPLICIT_ACCEPT") }

// orientEnabled turns on the explore-first grounding pass (maybeOrient): once per session, at the
// first cold write-capable top-level turn, the deterministic build/verify anchors and layout of
// the workspace (repoContext) are landed in the MAIN agent's context BEFORE the planner runs, so
// both the executor and the planner (which reads the session window) start grounded in the real
// environment instead of the instruction prose alone. The facts land in the conversation the
// executor keeps — not just the planner prompt — matching the "reason with full context"
// principle. Facts, not speculative instructions (contrast the reverted attempt ledger), so a
// clean run is not misdirected. Default ON; MAGI_ORIENT=off restores the un-grounded baseline
// (the A/B knob).
func orientEnabled() bool { return !envOff("MAGI_ORIENT") }

// asyncExplorersEnabled routes a top-level, read-only-only plan's explorer fan-out through the
// BACKGROUND dispatch path (a.dispatch) instead of the synchronous runExplorers, so the
// orchestrator loop parks in its bg-wait — staying responsive to user interjections — while the
// ~85s exploration runs, rather than blocking the whole turn before the loop even starts. Only a
// plan with NO write step (delegate/refine) is eligible; a mixed plan keeps the synchronous
// executeSteps path so a write step still sees prior explorer findings in its brief (ordering
// dependency). Default ON; MAGI_ASYNC_EXPLORERS=off restores the fully-synchronous preflight (the
// A/B knob).
func asyncExplorersEnabled() bool { return !envOff("MAGI_ASYNC_EXPLORERS") }

// sharedRefineEnabled runs a plan's sequentially-dependent refine phases in ONE shared child
// session (the first phase creates it via clone; later phases REUSE it, so each sees its
// predecessors' actual work) rather than giving each phase its own spawn-time clone of the
// parent — the fix for tightly-coupled phases missing each other's outputs. Default ON;
// MAGI_REFINE_SHARED=0 restores the legacy per-phase clone-at-spawn baseline (the A/B knob).
func sharedRefineEnabled() bool { return !envOff("MAGI_REFINE_SHARED") }

// planConvergeEnabled gates the plan-audit convergence judgment (D17): when the council
// rejects a plan and the planner re-plans, judge whether the revision actually addressed
// the concern and stop the loop early on an unproductive (ignored-the-concern) revision,
// rather than bounding purely on the round count. Default ON; MAGI_PLAN_CONVERGE=0 restores
// the round-count-only behavior (the PlanRevised diff is still emitted, but with no verdict).
func planConvergeEnabled() bool { return !envOff("MAGI_PLAN_CONVERGE") }

// soloAuditEnabled extends the plan-audit council — and the per-step deliverable criteria
// and executable checks it authors (storePlanCriteria/storePlanChecks) — to a SINGLE-step
// plan. Normally the audit is gated to a >=2-step procedure (maybePlanPreflight): a 1-step
// plan skips it entirely, so it authors NO criteria and NO checks. The completion gate then
// has no plan-time contract to verify and falls back to the termination council's plausibility
// vote over clipped happy-path tool evidence — which is not a literal-spec or edge-case checker
// (cancel-async-tasks: a lone "create run.py" step was voted done with the cancellation cleanup
// path never exercised). With this on, a 1-step plan gets the same audit and deliverable
// contract a multi-step one does; the async-explorer path and note injections already run for a
// 1-step plan, so this only adds the missing audit+contract. Default ON; MAGI_SOLO_AUDIT=off
// restores the >=2-step-only audit (the A/B knob).
func soloAuditEnabled() bool { return !envOff("MAGI_SOLO_AUDIT") }

// waitGuardEnabled gates the environment-wait recovery suppression: when a stall force-stop is
// reached but the no-progress window is dominated by waiting/polling (guard.stallIsWait — sleep,
// ping, nc, an `until … do sleep … done` readiness loop), the stuck-recovery coder spawn at the
// loop.go stall gate is suppressed. A coder cannot speed an external wait (a rebooting VM, a
// service starting), so redecomposing it is futile AND harmful: with no delegatable executor it spawns
// coder→coder whose child timeout is misreported as the whole run's context-deadline. Suppressing
// only the spawn leaves the honest stall stop intact (delivered→clean finish, or stall_guard), so
// an endless wait is still capped. Default ON; MAGI_WAIT_GUARD=off restores the unconditional
// recovery spawn (the A/B knob).
func waitGuardEnabled() bool { return !envOff("MAGI_WAIT_GUARD") }

// execExemptEnabled gates the loop guard's exec-repeat exemption AND the
// redirect-less bash-mutation epoch bump (both landed together in f3d1fbc): when on
// (default), an identical exec bash call (build/test/any script) is never
// hard-blocked — its outcome can change through state the guard cannot see, and the
// stall layer owns genuine spins — and `sed -i`/`patch`/install-style commands count
// as mutations that re-key the repeat fingerprints. MAGI_GUARD_EXEC_EXEMPT=off
// restores the pre-f3d1fbc baseline (every identical call blocked past repeatLimit,
// only redirect/heredoc/tee bash counted as mutation) — the A/B knob for whether the
// exemption's longer fix-cycles help or hurt.
func execExemptEnabled() bool { return !envOff("MAGI_GUARD_EXEC_EXEMPT") }

// stallConvergeEnabled gates the stalled-nudge convergence (D18a): the no-progress "stalled"
// nudge re-arms up to maxStallNudges times keyed purely on the sinceProgress count, without
// checking whether the redirect actually changed anything. When a re-arm's window produced no
// structural forward motion — neither a real mutation NOR a NOVEL exercising command
// (progressSinceNudge stays false) — the nudge was ignored, so collapse the remaining nudge
// budget and let the stall force-stop land the honest outcome now instead of burning more
// no-progress windows. It only ACCELERATES the same terminal landing (stuck()=="stall"); it
// never forces a pass and never fires while the agent is making progress (a mutation sets
// progressSinceNudge=true and restarts the window, so a post-nudge edit re-arms normally). Default
// ON; MAGI_STALL_CONVERGE=0 restores the fixed maxStallNudges re-arm.
func stallConvergeEnabled() bool { return !envOff("MAGI_STALL_CONVERGE") }

// criteriaContextEnabled gates showing the session's completion criteria to the WORKING
// agent in every step's volatile context. The criteria already exist — the plan council
// derives them from the task and stores them (storePlanCriteria) — but their only consumer
// was the termination council, which judges the FINISHED work against them. The worker
// never saw the contract it would be judged by, so a long run could optimize a cheap
// proxy the whole way (observed: 20 `make`s and ONE testsuite run in a 3-hour turn whose
// grading criterion was the testsuite). Showing the judge's own contract to the worker is
// self-derived wiring, not external information. Default ON; MAGI_CRITERIA_CONTEXT=0
// restores the judge-only baseline for A/B.
func criteriaContextEnabled() bool { return !envOff("MAGI_CRITERIA_CONTEXT") }

// stallNoveltyEnabled gates counting a NOVEL inspect-only command (a first-seen
// fingerprint — a new grep pattern, a new file listed) as "the agent responded to the
// stalled nudge", so the D18a convergence only collapses the nudge budget when the
// post-nudge window repeats already-seen fingerprints — true head-banging. Observed
// without it: an agent told to "take a different action" pivoted through eleven
// distinct novel searches and was force-stopped mid-pivot as if it had ignored the
// redirect, with three quarters of its budget unspent. The hard bound is unchanged:
// after maxStallNudges spent, a further windowful of anything-but-mutation still
// lands the honest stall. Default ON; MAGI_STALL_NOVELTY=0 restores the
// exercising-only baseline.
func stallNoveltyEnabled() bool { return !envOff("MAGI_STALL_NOVELTY") }

// divergeEnabled gates the planner's diverge→triage→commit clause (divergeClause in
// plan_prompts.go): under uncertainty, enumerate distinct hypotheses and kill them with
// cheap probes before committing the budget — breadth first, then depth. Model-facing
// prompt text, so the bench (not intent) decides whether it stays; default ON,
// MAGI_DIVERGE=0 restores the baseline planner contract.
func divergeEnabled() bool { return !envOff("MAGI_DIVERGE") }
