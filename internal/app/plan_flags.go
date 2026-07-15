package app

import (
	"os"
	"strings"
)

// Bench A/B env knobs for the planner. Each reader parses one MAGI_* env var into a
// bool so a paired ON/OFF run can measure a mechanism in isolation; see each doc for the
// arm it flips and its default. Split out of planner.go for cohesion (behavior unchanged).

// refineDisabled is a bench A/B knob (mirrors MAGI_MAX_PLAN_DEPTH): MAGI_REFINE=0 downgrades
// refine steps to solo, reproducing the pre-refine baseline (every sub-goal flattened inline)
// so a paired refine-ON/OFF comparison can run on the same task. Default on.
func refineDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_REFINE"))) {
	case "0", "off", "false", "no":
		return true
	}
	return false
}

// stepContextDisabled is a bench A/B knob (mirrors MAGI_REFINE): MAGI_STEP_CONTEXT=0 turns
// OFF the compact context brief injected into delegate hand-offs and read-only fan-out — the
// children then run fully context-free (the pre-brief baseline), so a paired ON/OFF run can
// measure whether the brief helps. Default on. It gates ONLY the brief; it never copies the
// parent conversation (that stays refine's job) — so even ON, delegate/fan-out get an
// overall-goal line plus sibling boundaries/outputs, not the parent's reasoning history.
func stepContextDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_STEP_CONTEXT"))) {
	case "0", "off", "false", "no":
		return true
	}
	return false
}

// adaptDisabled turns OFF the REACTIVE (as-needed) failure re-decomposition — the ADaPT
// mechanism where a step that fails is retried by decomposing it further. MAGI_ADAPT=0 leaves
// only PLANNED decomposition (the up-front hierarchical plan) plus the stall safety net
// (redecomposeStuck): a failed delegate/refine node backtracks after one shot instead of
// spawning informed retries / re-decomposition. This is the recursion-policy A/B knob — with it
// off, magi is closer to HTN-style hierarchical planning with just-in-time sub-planning than to
// ADaPT proper. Default on (=current reactive behavior) so the flag flips the arm, not the base.
func adaptDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_ADAPT"))) {
	case "0", "off", "false", "no":
		return true
	}
	return false
}

// specFidelityEnabled keeps a plan faithful to the request's LITERAL contract — exact field/
// message/function names, output formats, thresholds, literal strings. Deep planning paraphrases
// the instruction, and the executor then normalizes a literal (kv-store-grpc: the request's
// `value` field became `val`, failing a grader that checks verbatim); a shallow/solo run reads the
// raw instruction directly and keeps the literal. This flag turns on three defenses (a planner
// "preserve literals" rule, a plan-time spec-fidelity note, and a verbatim SPEC anchor for the
// context-free delegate child). Default ON; MAGI_SPEC_FIDELITY=0 restores the paraphrase-only
// baseline (the A/B knob). Mirrors stepContextDisabled/adaptDisabled.
func specFidelityEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_SPEC_FIDELITY"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

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
// that never orders/injects the checkpoint (the A/B knob). Mirrors specFidelityEnabled's env shape.
func checkpointFirstEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_CHECKPOINT_FIRST"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// stepVerifyEnabled turns on the per-step deliverable contract: the plan-audit council
// authors executable checks for each step's expected deliverable at PLAN time, the solo
// loop runs them deterministically at its finish boundary (runVerifyCmd + regex/exit
// predicate), a passing check deterministically checks off its todo, and when every check
// passes the termination council's open-ended continue path is skipped (the contract was
// settled at planning — no new demands after). A real check FAILURE injects the failing
// command's output as a one-shot continuation nudge; a clean run injects nothing (no
// context pollution). Default ON; MAGI_STEP_VERIFY=0 restores the baseline that leaves
// storage, the gate, and the council path fully inert (the A/B knob). Mirrors
// specFidelityEnabled's env shape.
func stepVerifyEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_STEP_VERIFY"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// recoveryRunCapEnabled caps stuck-recovery re-decomposition to fire at most once per RUN
// TREE rather than once per depth level. redecomposeStuck's one-shot guard (turnState.recovered)
// is per-turn, and each spawned recovery child runs its own fresh turnState — so under the
// delegate-off default a depth-0 stall spawns a coder at depth 1, that coder stalls and (still
// planEligible) spawns another at depth 2, and so on until the plan-depth cap stops it with a
// stall_guard halt (observed on compile-compcert). With this on, a recovery child is seeded as
// already-recovered, so it cannot trigger its OWN redecomposeStuck: exactly one recovery
// executor is spawned per run tree. Default ON: the coder-cascade halt it prevents is a clear
// failure mode, and one recovery executor per run tree is the intended reach of
// [[stuck-recovery-decoupled]]. MAGI_RECOVERY_RUNCAP=off restores the per-depth re-decomposition
// for an A/B arm.
// recoveryRunCapEnabled is off by default: multiple recovery executors are allowed per run
// tree (each stuck level re-arms its own lifeline, bounded by MaxPlanDepth). Set
// MAGI_RECOVERY_RUNCAP=on to restore the one-executor-per-run-tree cap (the recovery child
// starts already-recovered and cannot cascade).
func recoveryRunCapEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_RECOVERY_RUNCAP"))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
}

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
// literal-sentence baseline (the A/B knob). Mirrors specFidelityEnabled's env shape.
func implicitAcceptEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_IMPLICIT_ACCEPT"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// orientEnabled turns on the explore-first grounding pass (maybeOrient): once per session, at the
// first cold write-capable top-level turn, the deterministic build/verify anchors and layout of
// the workspace (repoContext) are landed in the MAIN agent's context BEFORE the planner runs, so
// both the executor and the planner (which reads the session window) start grounded in the real
// environment instead of the instruction prose alone. The facts land in the conversation the
// executor keeps — not just the planner prompt — matching the "reason with full context"
// principle. Facts, not speculative instructions (contrast the reverted attempt ledger), so a
// clean run is not misdirected. Default ON; MAGI_ORIENT=off restores the un-grounded baseline
// (the A/B knob). Mirrors asyncExplorersEnabled's env shape.
func orientEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_ORIENT"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// asyncExplorersEnabled routes a top-level, read-only-only plan's explorer fan-out through the
// BACKGROUND dispatch path (a.dispatch) instead of the synchronous runExplorers, so the
// orchestrator loop parks in its bg-wait — staying responsive to user interjections — while the
// ~85s exploration runs, rather than blocking the whole turn before the loop even starts. Only a
// plan with NO write step (delegate/refine) is eligible; a mixed plan keeps the synchronous
// executeSteps path so a write step still sees prior explorer findings in its brief (ordering
// dependency). Default ON; MAGI_ASYNC_EXPLORERS=off restores the fully-synchronous preflight (the
// A/B knob). Mirrors specFidelityEnabled's env shape.
func asyncExplorersEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_ASYNC_EXPLORERS"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// sharedRefineEnabled runs a plan's sequentially-dependent refine phases in ONE shared child
// session (the first phase creates it via clone; later phases REUSE it, so each sees its
// predecessors' actual work) rather than giving each phase its own spawn-time clone of the
// parent — the fix for tightly-coupled phases missing each other's outputs. Default ON;
// MAGI_REFINE_SHARED=0 restores the legacy per-phase clone-at-spawn baseline (the A/B knob).
func sharedRefineEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_REFINE_SHARED"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// planConvergeEnabled gates the plan-audit convergence judgment (D17): when the council
// rejects a plan and the planner re-plans, judge whether the revision actually addressed
// the concern and stop the loop early on an unproductive (ignored-the-concern) revision,
// rather than bounding purely on the round count. Default ON; MAGI_PLAN_CONVERGE=0 restores
// the round-count-only behavior (the PlanRevised diff is still emitted, but with no verdict).
func planConvergeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_PLAN_CONVERGE"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

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
// restores the >=2-step-only audit (the A/B knob). Mirrors specFidelityEnabled's env shape.
func soloAuditEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_SOLO_AUDIT"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// waitGuardEnabled gates the environment-wait recovery suppression: when a stall force-stop is
// reached but the no-progress window is dominated by waiting/polling (guard.stallIsWait — sleep,
// ping, nc, an `until … do sleep … done` readiness loop), the stuck-recovery coder spawn at the
// loop.go stall gate is suppressed. A coder cannot speed an external wait (a rebooting VM, a
// service starting), so redecomposing it is futile AND harmful: under delegate-off it spawns
// coder→coder whose child timeout is misreported as the whole run's context-deadline. Suppressing
// only the spawn leaves the honest stall stop intact (delivered→clean finish, or stall_guard), so
// an endless wait is still capped. Default ON; MAGI_WAIT_GUARD=off restores the unconditional
// recovery spawn (the A/B knob). Mirrors soloAuditEnabled's env shape.
func waitGuardEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_WAIT_GUARD"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

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
func stallConvergeEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_STALL_CONVERGE"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}
