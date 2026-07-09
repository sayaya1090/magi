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
// end-of-turn unverifiedDeliverable backstop, not a replacement. Default OFF (opt-in,
// unvalidated): MAGI_CHECKPOINT_FIRST=1 enables it for an A/B arm. Mirrors
// specFidelityEnabled's env shape.
func checkpointFirstEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_CHECKPOINT_FIRST"))) {
	case "1", "on", "true", "yes":
		return true
	}
	return false
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
