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

// councilDebateEnabled gates the disagreement-triggered rebuttal round: after the
// members vote independently, a SPLIT (both done and continue) triggers one round
// where each sees the others' rationales and may hold or revise, then a re-tally.
// Motivated by the observed variance in council judgment (the same deliverable
// approved 3-0 on one run, rejected 0-3 on another) — a coin-flip majority stands
// when members disagree for contradictory reasons or one catches a defect the
// others miss. Unanimous votes skip it (no extra call). Default ON;
// MAGI_COUNCIL_DEBATE=0 restores the independent-vote-only tally (A/B knob).
func councilDebateEnabled() bool { return !envOff("MAGI_COUNCIL_DEBATE") }

// ctxCompactRetryEnabled controls the reactive-compaction safety net. On (the default), when the
// provider rejects a generate request as too long (isContextOverflow), the loop force-compacts and
// re-issues instead of dying with a terminal error — recovering runs whose context outgrew the
// model's real window (e.g. an uncalibrated window constant, or unbounded growth across many
// delegate rounds). MAGI_CTX_COMPACT_RETRY=0 restores the old fail-fast for A/B. Inert unless the
// backend actually returns a context-length error.
func ctxCompactRetryEnabled() bool { return !envOff("MAGI_CTX_COMPACT_RETRY") }

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

// declareFinishEnabled requires a working turn to END BY DECLARATION — the agent calls the council
// tool with complete:true and the council accepts — rather than by falling silent. Default ON;
// MAGI_DECLARE_FINISH=0 restores the passive finish, where a turn ends whenever a step produces no
// tool call, for A/B.
func declareFinishEnabled() bool { return !envOff("MAGI_DECLARE_FINISH") }

// declareAskCap bounds how many times one turn is told to declare completion. Small on purpose: the
// ask exists for an agent that forgot the form, and repeating it at one that cannot produce it only
// spends the session's remaining time looking busy.
const declareAskCap = 3
