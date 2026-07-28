package app

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// checkCmdTimeout bounds a single deliverable-check command run (runVerifyCmd), so a hung/blocking
// check can't strand the turn until its wall clock. Default 120s (the bash tool's own default ceiling);
// MAGI_CHECK_TIMEOUT=<seconds> overrides, 0 disables the per-command bound.
func checkCmdTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("MAGI_CHECK_TIMEOUT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second
		}
	}
	return 120 * time.Second
}

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

// defaultCheckChurnCap is how many finish attempts whose own build/test keeps FAILING while the
// agent edits the deliverable (mutation epoch advancing) are allowed before the run lands
// gracefully UNVERIFIED with work standing. Generous on purpose: a CONVERGING check passes and
// resets the counter, so only a non-converging loop ever reaches the cap.
const defaultCheckChurnCap = 4

// checkChurnCap returns the effective cap. MAGI_CHECK_CHURN_CAP overrides it: a positive integer
// sets the cap, "0" (or a non-positive/garbage value) disables the graceful landing entirely,
// and unset uses the default. checkChurnLandEnabled reports whether the landing is active.
func checkChurnCap() int {
	v := strings.TrimSpace(os.Getenv("MAGI_CHECK_CHURN_CAP"))
	if v == "" {
		return defaultCheckChurnCap
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func checkChurnLandEnabled() bool { return checkChurnCap() > 0 }

// defaultExerciseChurnCap is how many times the SAME build/test the agent itself runs may FAIL
// across distinct edits — without that command ever passing — before a solo run lands gracefully
// UNVERIFIED with work standing (see runGuard.exerciseFail / handleStuckGuard). More generous than
// the check-churn cap: this is the agent's own iterative debugging, so a legitimately hard fix that
// takes several edit→test cycles must not be cut — only a genuinely non-converging command (still
// the same failure many edits later, never a pass) reaches it. Keyed per command, so a passing
// sibling resets nothing here — the failing command must itself keep failing.
const defaultExerciseChurnCap = 6

// exerciseChurnCap returns the effective cap. MAGI_EXERCISE_CHURN_CAP overrides it: a positive
// integer sets the cap, "0" (or a non-positive/garbage value) disables the landing entirely, and
// unset uses the default. exerciseChurnLandEnabled reports whether the landing is active.
func exerciseChurnCap() int {
	v := strings.TrimSpace(os.Getenv("MAGI_EXERCISE_CHURN_CAP"))
	if v == "" {
		return defaultExerciseChurnCap
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func exerciseChurnLandEnabled() bool { return exerciseChurnCap() > 0 }

// defaultSterileReplanCap is how many consecutive agent-initiated replans may finish NO new plan
// step — the completed-step high-water never advancing — before a solo run lands gracefully
// UNVERIFIED with work standing (see runGuard.noteReplan / handleStuckGuard). Generous: a genuinely
// hard task can legitimately need several re-decompositions, so only a re-plan loop that completes
// nothing across this many passes reaches it. The high-water resets on any real step progress, so a
// plan that keeps finishing steps never accumulates here.
const defaultSterileReplanCap = 4

// sterileReplanCap returns the effective cap. MAGI_REPLAN_CAP overrides it: a positive integer sets
// the cap, "0" (or a non-positive/garbage value) disables the landing entirely, and unset uses the
// default. sterileReplanLandEnabled reports whether the landing is active.
func sterileReplanCap() int {
	v := strings.TrimSpace(os.Getenv("MAGI_REPLAN_CAP"))
	if v == "" {
		return defaultSterileReplanCap
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func sterileReplanLandEnabled() bool { return sterileReplanCap() > 0 }

// workdirCheckpointEnabled gates the opt-in work-tree rollback (checkpoint.go): before a subagent's
// first attempt the work-tree is snapshotted into a PRIVATE scratch git-dir (never the user's own
// .git/stash/HEAD), and each retry restores that snapshot so it re-runs on a clean tree instead of
// the failed attempt's debris (the compile-compcert self-clone retry loop). Default OFF — restore
// is a destructive clean, so it stays opt-in and bench-scoped until A/B-validated.
// MAGI_WORKDIR_CHECKPOINT=1 enables it.
func workdirCheckpointEnabled() bool { return envOn("MAGI_WORKDIR_CHECKPOINT") }

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

// councilDevilEnabled gates the devil's-advocate pass. The rebuttal round only fires on a SPLIT,
// so a unanimous (no-split) DONE sails through with nobody having argued against it — the premature
// consensus a devil exists to stress-test. When on, an otherwise-unchallenged done has one
// adversarial member argue the strongest case against it; its concern is then RE-JUDGED by the
// members CRITICALLY (a spurious concern the task never required is rejected; a real missed defect
// flips them to continue) and their re-tally decides — the devil casts no binding vote. Default ON;
// MAGI_COUNCIL_DEVIL=0 restores the no-devil baseline (A/B knob).
func councilDevilEnabled() bool { return !envOff("MAGI_COUNCIL_DEVIL") }

// councilKeepEnabled asks each council member to ALSO report what the report already gets
// right through its lens — advisory "keep this, don't redo/revert it" surfaced above the fix
// feedback when the turn continues. It never affects the decision or tally. It targets two
// observed weak-model failures: reverting a correct edit because nothing affirmed it (the
// ocaml merge-check fix), and re-verifying already-working parts to exhaustion because nothing
// said they were settled (the kv-store finish spin). Default ON; MAGI_COUNCIL_KEEP=0 restores the
// baseline (no keep clause, byte-identical prompt) for A/B.
func councilKeepEnabled() bool { return !envOff("MAGI_COUNCIL_KEEP") }

// constraintGateEnabled turns ON the termination council's scope/boundary REJECTION clause — verify the
// diff/artifact against the task's stated scope limits, structural requirements, and forbidden actions,
// voting continue on a violation. Default OFF: it ADDS a rejection criterion to a council the corrected
// bench forensics showed is already prone to OVER-rejecting correct work (a task that passed the grader
// while the council never approved). So it ships opt-in and is measured on an A/B arm before it becomes
// default — shipping an unvalidated rejection-adder to an over-strict council can worsen false rejects.
// The specMine constraints EXTRACTION stays on regardless: it is preventive (surfacing a boundary so the
// executor does not cross it), not a new reason for the council to reject.
func constraintGateEnabled() bool { return envOn("MAGI_CONSTRAINT_GATE") }

// subagentWaitLeaseEnabled makes the subagent lease judge treat WAITING on a long external
// operation — a VM booting, a build compiling, a package installing, a service coming up — as a
// legitimate wait, not churn. Without it, a subagent blocked on such an operation polls with no
// deliverable motion, the judge reads that as churning, and KILLs it every lease (~2.5 min) —
// the qemu-alpine-ssh stall, where an Alpine boot never got enough runway. The subagent-lease
// counterpart of stallIsWait. Default ON (a conservative safety fix, bounded by the backstop);
// MAGI_SUBAGENT_WAIT_LEASE=0 restores the judge-everything baseline for A/B.
func subagentWaitLeaseEnabled() bool { return !envOff("MAGI_SUBAGENT_WAIT_LEASE") }

// subagentProcLeaseEnabled makes the subagent lease judge extend when the child owns a
// magi-managed background process (a bash{background:true} job) that is ACTIVELY using CPU at
// lease expiry. Foreground work is already covered by toolInFlight, but an off-tool background
// transfer/build (a long scp/tar/make the model launched as a job and stopped polling) emits no
// events and is not a poll verb, so the judge misreads it as churn and KILLs it every lease —
// the plexus #224 remote-download spin. Sampling real CPU (not the command name) is robust to a
// worker wrapping its work in a self-written script. Idle/wedged processes (CPU ~flat) do NOT
// extend, and the backstop still caps the attempt. Default ON; MAGI_SUBAGENT_PROC_LEASE=0
// restores the judge-everything baseline for A/B.
func subagentProcLeaseEnabled() bool { return !envOff("MAGI_SUBAGENT_PROC_LEASE") }

// leaseProgressEnabled renews a subagent's lease on PRODUCTIVITY rather than only on elapsed time:
// a child that changed a file or ran a first-seen exercising command inside the lease window gets a
// deterministic extension instead of a judged verdict.
//
// The lease exists to bound unproductive time, and measuring elapsed time is only a proxy for that
// — one that charges a working child and an idle one identically. Measured on a live run: ten
// workers across two steps, each given 2.5-3 minutes and 11-15 tool calls, killed while producing;
// one of the ten ever reported, and the step took twenty-eight minutes to not finish. The extension
// is per-tick and the counter must ADVANCE to earn the next one, so a child that produces once and
// then spins is judged normally, and the backstop still caps the attempt.
//
// Default ON; MAGI_LEASE_PROGRESS=0 restores the elapsed-time-only lease for A/B.
func leaseProgressEnabled() bool { return !envOff("MAGI_LEASE_PROGRESS") }

// turnProgressCheckEnabled adds a STEP-based no-deliverable-progress check to the top-level
// turn. The stall/loop guards count TOOL CALLS since the last mutation, so they miss a reasoning
// loop: an agent that streams thinking for hours issuing few/no tool calls and producing nothing
// (path-tracing-reverse burned ~4h on hand-disassembly; circuit-fibsqrt wrote 131MB of algorithm
// reasoning and never emitted gates.txt). Counting STEPS since the last mutation catches the
// rabbit hole regardless of tool-call volume, then routes it to the same nudge → stuck-recovery →
// honest-stop ladder. Waiting on a long external op is explicitly NOT a rabbit hole — the "idle"
// kind is suppressed by the same wait guards as stall recovery (stallIsWait + childWaitMajority),
// so a VM boot / build / install is never cut. Default ON; MAGI_TURN_PROGRESS_CHECK=0 restores the baseline (A/B).
func turnProgressCheckEnabled() bool { return !envOff("MAGI_TURN_PROGRESS_CHECK") }

// ctxCompactRetryEnabled controls the reactive-compaction safety net. On (the default), when the
// provider rejects a generate request as too long (isContextOverflow), the loop force-compacts and
// re-issues instead of dying with a terminal error — recovering runs whose context outgrew the
// model's real window (e.g. an uncalibrated window constant, or unbounded growth across many
// delegate rounds). MAGI_CTX_COMPACT_RETRY=0 restores the old fail-fast for A/B. Inert unless the
// backend actually returns a context-length error.
func ctxCompactRetryEnabled() bool { return !envOff("MAGI_CTX_COMPACT_RETRY") }

// recoveryRunCapEnabled caps stuck-recovery re-decomposition to fire at most once per RUN
// TREE rather than once per depth level: a recovery child is seeded as already-recovered, so
// it cannot trigger its OWN redecomposeStuck (no coder→coder cascade down the depth levels).
// Off (the default), multiple recovery executors are allowed per run tree — each stuck level
// re-arms its own lifeline, bounded by MaxPlanDepth. Set MAGI_RECOVERY_RUNCAP=on to restore
// the one-executor-per-run-tree cap.
func recoveryRunCapEnabled() bool { return envOn("MAGI_RECOVERY_RUNCAP") }

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

// leaseExternalCreditEnabled stops the per-attempt BACKSTOP from charging a child for time it spent
// blocked on an external process.
//
// The backstop is an absolute wall on one attempt (3 × SubagentTimeout = 15m by default), and
// lease_verdict short-circuits the whole ladder once it is spent — so the arm that exists for
// "legitimately blocked on a long operation it cannot speed up" is unreachable exactly when it
// matters most. Observed live: three consecutive sub-planners were handed the identical unit
// ("build the compiler following HACKING.adoc") at 15:00 intervals, each cancelled mid-build by the
// backstop, each starting over from nothing — and one of them ran `make clean` first, so the carry
// was negative. A build that takes longer than fifteen minutes was not reachable through delegation
// at all, and restarting it could not make it any faster.
//
// So the backstop measures UNPRODUCTIVE attempt time instead of elapsed attempt time, the same
// correction the lease itself already got. A window in which magi could SEE an external process
// running (a tool call in flight, or an owned background pid burning CPU) is credited back.
// Model-side silence is NOT credited, and subagentBackstopCeiling caps the attempt regardless, so a
// wedged build cannot hold a slot forever.
//
// Default ON; MAGI_LEASE_EXTERNAL_CREDIT=0 restores the elapsed-time backstop for A/B.
func leaseExternalCreditEnabled() bool { return !envOff("MAGI_LEASE_EXTERNAL_CREDIT") }

// councilAdvisoryEnabled makes the termination council ADVISE instead of decide.
//
// A tally decided the turn before, and everything that turned opinions into a verdict — the rule,
// the rounds, the convergence judgment, the rebuttal round, the devil pass — existed to make that
// verdict trustworthy. A gate that keeps a turn open cannot tell a wrong result from an unfinished
// one, so it held correct work open and let a plausible report through. magi's own record already
// says which commands ran and which succeeded, and that is what a guard should be made of: an
// observation cannot be wrong about what it observed.
//
// The dissent is not lost — it is injected for the agent, recorded on the turn, and carried in the
// unverified reason. What still forces another step is deterministic: Stop hooks, the exercise
// ledger, the fabrication signal.
//
// Default ON; MAGI_COUNCIL_ADVISORY=0 restores the voting gate for A/B.
func councilAdvisoryEnabled() bool { return !envOff("MAGI_COUNCIL_ADVISORY") }
