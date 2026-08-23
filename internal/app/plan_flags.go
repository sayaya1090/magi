package app

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/envflag"
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
// Each reader below names its default and hands the rest to envflag, which is the one place
// that decides what counts as on and what counts as off.

// councilDebateEnabled gates the disagreement-triggered rebuttal round: after the
// members vote independently, a SPLIT (both done and continue) triggers one round
// where each sees the others' rationales and may hold or revise, then a re-tally.
// Motivated by the observed variance in council judgment (the same deliverable
// approved 3-0 on one run, rejected 0-3 on another) — a coin-flip majority stands
// when members disagree for contradictory reasons or one catches a defect the
// others miss. Unanimous votes skip it (no extra call). Default ON;
// MAGI_COUNCIL_DEBATE=0 restores the independent-vote-only tally (A/B knob).
func councilDebateEnabled() bool { return envflag.Enabled("MAGI_COUNCIL_DEBATE", true) }

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
func councilKeepEnabled() bool { return envflag.Enabled("MAGI_COUNCIL_KEEP", true) }

// councilSuiteWalkEnabled tightens the verification lens: a suite SUMMARY line — "N passed", "all
// tests green", a coverage percentage — is a count, not per-requirement evidence.
//
// Measured on headless-terminal, 2026-08-23 (sonnet): three lenses voted done 3-0 citing
// "7 passed, 5 warnings in 16.36s", and the graded suite failed test_background_commands. Every
// citation was a real tool result and the count was true; it just answered for a requirement
// nothing had asserted. See council.SuiteWalkClause for why this is not a rule about who wrote
// the test.
//
// Default OFF: it is a prompt change, and a prompt change to this council has been measured
// wrong before (the spec-fidelity lens was reverted on an A/B). MAGI_COUNCIL_SUITE_WALK=1 turns
// it on for the arm that measures it.
func councilSuiteWalkEnabled() bool { return envflag.Enabled("MAGI_COUNCIL_SUITE_WALK", false) }

// ctxCompactRetryEnabled controls the reactive-compaction safety net. On (the default), when the
// provider rejects a generate request as too long (isContextOverflow), the loop force-compacts and
// re-issues instead of dying with a terminal error — recovering runs whose context outgrew the
// model's real window (e.g. an uncalibrated window constant, or unbounded growth across many
// delegate rounds). MAGI_CTX_COMPACT_RETRY=0 restores the old fail-fast for A/B. Inert unless the
// backend actually returns a context-length error.
func ctxCompactRetryEnabled() bool { return envflag.Enabled("MAGI_CTX_COMPACT_RETRY", true) }

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
func stallNoveltyEnabled() bool { return envflag.Enabled("MAGI_STALL_NOVELTY", true) }

// declareFinishEnabled requires a working turn to END BY DECLARATION — the agent calls the council
// tool with complete:true and the council accepts — rather than by falling silent. Default ON;
// MAGI_DECLARE_FINISH=0 restores the passive finish, where a turn ends whenever a step produces no
// tool call, for A/B.
func declareFinishEnabled() bool { return envflag.Enabled("MAGI_DECLARE_FINISH", true) }

// councilRejectCapEnabled gates the rejection cap (see noteCouncilRejection): after enough
// rejected completion declarations — three in a row with no mutation between them, or eight in
// one turn — the turn lands UNVERIFIED as it stands instead of cycling declare→reject to the
// step backstop. It restores the valve the old finish-boundary gate had (CouncilMaxRounds) and
// the manual's promise that an honest "could not" is a terminal outcome. Default ON;
// MAGI_COUNCIL_REJECT_CAP=0 restores the uncapped loop for A/B.
func councilRejectCapEnabled() bool { return envflag.Enabled("MAGI_COUNCIL_REJECT_CAP", true) }

// declareAskCap bounds how many times one turn is told to declare completion. Small on purpose: the
// ask exists for an agent that forgot the form, and repeating it at one that cannot produce it only
// spends the session's remaining time looking busy.
const declareAskCap = 3

// interjectSplitEnabled gives every queued message its own disposition instead of one for the batch.
//
// The finish boundary used to merge the whole queue into a single prompt and triage that once, so a
// batch holding any work escalated as a unit and the questions in it rode along unanswered. Split,
// each message is triaged on its own: a question is answered where it is dequeued, and the work
// items are the only ones that coalesce — they arrived together and are one body of work.
//
// It also turns on the re-evaluation a new turn does over whatever is still queued, which is the
// half that needs a turn to exist: a message can be unanswerable while the work is in flight and
// answerable the moment it lands. Off restores the single batch disposition (A/B knob).
func interjectSplitEnabled() bool { return envflag.Enabled("MAGI_INTERJECT_SPLIT", true) }
