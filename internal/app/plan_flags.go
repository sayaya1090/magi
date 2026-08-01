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

// councilDebateEnabled gates the disagreement-triggered rebuttal round: after the
// members vote independently, a SPLIT (both done and continue) triggers one round
// where each sees the others' rationales and may hold or revise, then a re-tally.
// Motivated by the observed variance in council judgment (the same deliverable
// approved 3-0 on one run, rejected 0-3 on another) — a coin-flip majority stands
// when members disagree for contradictory reasons or one catches a defect the
// others miss. Unanimous votes skip it (no extra call). Default ON;
// MAGI_COUNCIL_DEBATE=0 restores the independent-vote-only tally (A/B knob).
func councilDebateEnabled() bool { return !envOff("MAGI_COUNCIL_DEBATE") }

// councilKeepEnabled asks each member to ALSO name what the work already gets right through its
// lens, alongside whatever it wants changed. It is advisory and never touches the decision or the
// tally; it exists because a member that only lists faults invites the agent to redo or revert
// parts that were already correct.
//
// The whole path was built and then unreachable: the adapter asks for it, parses it, the TUI
// renders it and renderCouncilAdvice surfaces it — and nothing set the request field, so members
// were never asked and the advice was always empty. It had been wired through four deliberation
// phases once; the phases came out and the one call site that survived did not carry it. Default
// ON; MAGI_COUNCIL_KEEP=0 restores fix-only feedback (A/B knob).
func councilKeepEnabled() bool { return !envOff("MAGI_COUNCIL_KEEP") }

// ctxCompactRetryEnabled controls the reactive-compaction safety net. On (the default), when the
// provider rejects a generate request as too long (isContextOverflow), the loop force-compacts and
// re-issues instead of dying with a terminal error — recovering runs whose context outgrew the
// model's real window (e.g. an uncalibrated window constant, or unbounded growth across many
// delegate rounds). MAGI_CTX_COMPACT_RETRY=0 restores the old fail-fast for A/B. Inert unless the
// backend actually returns a context-length error.
func ctxCompactRetryEnabled() bool { return !envOff("MAGI_CTX_COMPACT_RETRY") }

// stallNoveltyEnabled gates counting a NOVEL inspect-only command (a first-seen
// fingerprint — a new grep pattern, a new file listed) as "the agent responded to the
// stalled nudge", so the D18a convergence only collapses the nudge budget when the
// post-nudge window repeats already-seen fingerprints — true head-banging. Observed
// without it: an agent told to "take a different action" pivoted through eleven
// distinct novel searches and was force-stopped mid-pivot as if it had ignored the
// redirect, with three quarters of its budget unspent. (That force-stop is gone, and
// so is the budget: the stalled nudge now re-arms for as long as nothing changes. What
// this flag still decides is whether a novel search COUNTS as responding to the nudge,
// which is what stops the re-arm from firing at an agent that is actually pivoting.)
// Default ON; MAGI_STALL_NOVELTY=0 restores the exercising-only baseline.
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
