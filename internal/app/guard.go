package app

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/change"
)

// Loop-guard thresholds. They catch an agent (orchestrator or subagent) that
// repeats the SAME action without progress long before MaxSteps would, so a
// stuck weak model fails fast instead of grinding for minutes.
const (
	repeatLimit   = 2 // identical tool calls allowed before the next is blocked
	blockedBudget = 6 // total blocked repeats in a run before forcing a stop
	// nudgeThreshold sits below blockedBudget: when blocked repeats reach it, the agent
	// is clearly thrashing, so before force-stopping we inject ONE corrective re-grounding
	// (re-read the task, change approach) — a stuck weak model often just needs redirecting.
	nudgeThreshold = 3
	// noProgressNudge catches the OTHER stall the repeat guard misses: an agent that runs
	// many DIFFERENT commands (so no fingerprint repeats and `blocked` stays 0) yet changes
	// nothing — the "productive-looking non-progress" loop (echo/cat/ls restating the same
	// conclusion) that otherwise burns to MaxSteps. It counts tool calls since the last real
	// file mutation; set high so genuine multi-step investigation isn't nudged. It never
	// force-stops (a read-only turn may legitimately never mutate a file), so unlike the
	// blocked path it has no backstop — a single ignored nudge would let the agent thrash to
	// MaxSteps. So the stalled nudge RE-ARMS: it fires again after each further noProgressNudge
	// window with still no mutation, up to maxStallNudges, then goes quiet.
	noProgressNudge = 12
	// maxStallNudges caps the re-armed no-progress nudges per run: enough to redirect an agent
	// that ignored the first one or two, few enough that a genuinely read-heavy turn is not
	// spammed. A real file mutation re-arms the window (see mutated).
	maxStallNudges = 3
	// bannerSpinNudge/bannerSpinStop catch the "completion-banner spin": a weak model that has
	// declared the task done keeps emitting a no-op banner (`echo "TASK COMPLETED"`, `true`, …)
	// as a TOOL CALL every turn, so len(toolCalls) is never 0 and the run never reaches the
	// finish/council gate — it churns to the harbor wall clock and returns reward=None (ungraded
	// waste). bannerSpin counts CONSECUTIVE pure no-op banners (see isNoOpBanner/noteSpin); any
	// real action resets it. Thresholds are calibrated on 216 reward-graded Terminal-Bench runs:
	// passing runs (reward=1) peaked at a banner streak of 8, so a force-stop at 9 STRICTLY
	// exceeds every observed pass streak — zero false-positive by construction — while catching
	// the 9 failing runs that spun 9..23 banners to the wall clock. The nudge fires earlier (5)
	// as a one-shot teach-the-end-turn hint; it only messages, so a pass run that briefly spins
	// is unharmed.
	bannerSpinNudge = 5
	bannerSpinStop  = 9
)

// runGuard detects no-progress loops within a single run by fingerprinting each
// tool call (name + canonical args). It is shared across the concurrent and
// sequential tool-execution paths, so it carries its own lock.
//
// The fingerprint includes a mutation epoch: a successful file write/edit bumps the
// epoch, which resets all repeat counts. Repeating a command is only a no-progress
// loop if nothing changed between calls — so re-running a test after editing the file
// under test (the correct thing to do) is allowed, instead of being blocked blind.
type runGuard struct {
	mu      sync.Mutex
	seen    map[string]int
	results map[string]string // last result content per fingerprint, for echo on block
	lastMut map[string]string // path → last mutation signature, to ignore idempotent rewrites
	epoch   int               // bumped on each real file mutation; part of the fingerprint
	blocked int
	// sinceProgress counts tool calls since the last real file mutation (epoch bump). It
	// powers the no-progress nudge: unlike `blocked` (which needs an EXACT repeat), it rises
	// even when the agent varies its commands, catching a stall that changes nothing.
	sinceProgress int
	// execSinceMut counts bash commands that actually EXERCISE the deliverable (run a program
	// or test — anything whose leading verb is not an inspect-only builtin; see isInspectOnly)
	// SINCE the last real file mutation. Like a tester PASS, execution evidence is version-
	// stamped: a mutation resets it to 0, so "did you run anything against the CURRENT
	// deliverable" is answerable structurally. epoch>0 with execSinceMut==0 is the language-
	// agnostic "produced/changed a deliverable then declared done without running it" signal
	// (see unverifiedDeliverable) that replaced the English-only fabrication phrase scan.
	execSinceMut int
	// waitSinceMut counts bash commands that only WAITED or POLLED (isWaitCommand — a delay or an
	// external-readiness probe, ANY exit code) SINCE the last real mutation. It powers stallIsWait:
	// a no-progress window dominated by these is an agent blocked on the environment, not thrashing
	// the task, so the stuck-recovery coder spawn is suppressed for it (a coder cannot speed an
	// external wait). Like sinceProgress/execSinceMut it is version-scoped: a real mutation resets it.
	waitSinceMut int
	// prevSince/prevStallAt snapshot sinceProgress/lastStallAt just before mutated() zeroes them,
	// so retractProgress can restore the climb when a later content check reveals that "mutation"
	// was a self-revert (churn, not forward progress) — otherwise an implement↔revert oscillation
	// resets the counter each swing and never trips the stall force-stop.
	prevSince     int
	prevStallAt   int
	calls         int  // total tool calls this run (idle-resubmission detection)
	nudgedBlocked bool // "blocked"-kind re-grounding fired (once; stuck() force-stops if it persists)
	stallNudges   int  // count of "stalled"-kind re-groundings fired this run (capped at maxStallNudges)
	lastStallAt   int  // sinceProgress value at the last stalled nudge, for spacing the re-arm
	// stallConverge enables the D18a re-arm collapse (set by the loop from MAGI_STALL_CONVERGE;
	// zero value = off, so a test opts in explicitly). progressSinceNudge is the structural
	// "the agent made forward motion since the last stalled nudge" signal: set true by EITHER a
	// real mutation (mutated) OR a NOVEL (first-seen this epoch) non-inspect exercising command
	// (noteBashExec) — both are genuine progress. It is set false when a stalled nudge fires and
	// on a structural recovery (resetStall). false at a re-arm point means the prior nudge was
	// ignored (no mutation AND no novel exercise since), so the remaining nudge budget collapses.
	// A mutation MUST count as motion here: mutated() restarts the stall window (lastStallAt=0),
	// so a window CAN climb back to threshold after an early mutation — the naive "window climbed
	// ⇒ no mutation" premise is false, and treating a mutation as non-motion would collapse an
	// agent that edited a file in direct response to the nudge (the opposite of the intent).
	stallConverge      bool
	progressSinceNudge bool

	// bannerSpin counts CONSECUTIVE pure no-op completion banners (isNoOpBanner) since the last
	// real action. It is ORTHOGONAL to epoch/sinceProgress: a fabricating agent that interleaves
	// deliverable rewrites bumps the epoch (resetting the stall counter) but a rewrite is not a
	// banner, so it also resets bannerSpin to 0 — the spin only survives an UNBROKEN banner run,
	// which is exactly the keep-alive dodge. nudgedSpin fires the one-shot re-grounding once.
	bannerSpin int
	nudgedSpin bool

	// changed records this turn's file edits (before/after content) as the council's
	// evidence of what the AGENT actually changed — reconstructed from its own write/edit
	// tools, not git, so a human/external/bash change is never mis-attributed to the agent.
	changed       map[string]*fileChange
	changeOrder   []string        // first-seen order, for stable rendering
	exercisedFile map[string]bool // authored files an EXERCISING command has named (exec-evidence)

	// recalled bounds recall_context: re-hydrating compacted detail re-inflates context
	// (which can re-trigger compaction), so a turn may recall each topic once and only up
	// to recallBudget times total — otherwise a recall→compact→recall loop could spin.
	recalled    map[string]bool
	recallCount int

	// contentHist records, per file, the sequence of content states (by hash) the file
	// has passed through this turn — index 0 is the pre-turn baseline, then one entry per
	// edit. It powers self-regression detection: an edit that returns a file to a state it
	// already held this turn means the agent is undoing its own earlier change (the silent
	// self-revert that no other guard catches).
	contentHist map[string][]uint64
	// regressWarned marks files already flagged for self-regression this turn, so an
	// oscillating agent (A→B→A→B…) is warned once per file, not on every swing — a repeated
	// nudge could itself push a weak model to keep thrashing.
	regressWarned map[string]bool

	// failedStates is the tabu list: a deliverable content-signature (deliverableSigLocked over
	// the agent's own edits this turn) maps to a short snippet of the exercise error observed at
	// that exact state. It is populated when an EXERCISING command (a test/program run, not an
	// inspect-only builtin) FAILS — recording "this precise set of file contents was already
	// tried and did not work". noteEdit catches a byte-revert to ANY earlier state; the tabu list
	// is the higher-precision complement: it fires only when an edit reproduces a state whose
	// test is known to fail, so an agent circling back to a proven-bad approach is told so
	// instead of re-running the same failing loop. tabuWarned makes the warning once-per-signature
	// (a repeated nudge could itself push a weak model to keep thrashing).
	failedStates map[uint64]string
	tabuWarned   map[uint64]bool
}

// fileChange is one file's before/after content captured around an agent edit this turn.
type fileChange struct {
	path   string
	before string // content just before the FIRST edit this turn ("" if newly created)
	after  string // content just after the LATEST edit
}

func newRunGuard() *runGuard {
	return &runGuard{
		seen: map[string]int{}, results: map[string]string{},
		lastMut: map[string]string{}, changed: map[string]*fileChange{},
		exercisedFile: map[string]bool{},
		recalled:      map[string]bool{}, contentHist: map[string][]uint64{},
		regressWarned: map[string]bool{},
		failedStates:  map[uint64]string{}, tabuWarned: map[uint64]bool{},
	}
}

// hashContent returns a fast 64-bit fingerprint of file content for the regression
// history. Collisions are astronomically unlikely and the check is only advisory, so a
// hash (bounded memory) is preferred over retaining every full content snapshot.
func hashContent(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// noteEdit records a file's post-edit content in this turn's per-file history and reports
// whether the edit RETURNED the file to a content state it already held earlier this turn —
// i.e. the agent is undoing its own prior change. `before` is the file's content captured
// just before the FIRST edit of the turn (used only to seed the pre-turn baseline); `after`
// is the content right after this edit. Returns a non-empty advisory when a regression is
// detected, else "". An idempotent rewrite (identical to the immediately-preceding state) is
// the loop guard's domain and never warns here.
//
// Notes/limits (all acceptable since the result is a non-blocking advisory): the content is
// a best-effort prefix — readForChange caps reads at changeReadCap, so a >cap file whose
// first cap bytes are unchanged can false-match (or a true revert past the cap can be
// missed). A create-then-empty (""→content→"") is reported as a real self-revert. The
// wording is a neutral observation, not a "put it back" instruction, to avoid pushing a
// weak model into an oscillation; and each file is flagged at most once per turn.
func (g *runGuard) noteEdit(path, before, after string) (warn string, regressed bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	hist, ok := g.contentHist[path]
	if !ok {
		hist = []uint64{hashContent(before)} // index 0 = pre-turn baseline
	}
	h := hashContent(after)
	if h == hist[len(hist)-1] {
		return "", false // no real change since the last state → idempotent, not a regression
	}
	// Scan states strictly before the latest: a match means the file returned to a state it
	// already held this turn (original→fix→original, or fix1→fix2→fix1 oscillation).
	for i := 0; i < len(hist)-1; i++ {
		if hist[i] == h {
			regressed = true
			break
		}
	}
	g.contentHist[path] = append(hist, h)
	if !regressed {
		return "", false // forward progress
	}
	// A revert is churn, so report regressed=true on EVERY swing (the caller withholds progress
	// credit each time). The human-facing warning, though, fires at most once per file: a repeated
	// nudge can itself push a weak model to keep thrashing.
	if g.regressWarned[path] {
		return "", true
	}
	g.regressWarned[path] = true
	return "note: this edit restored a content state this file already had earlier this turn — " +
		"if reverting your own earlier change was intentional, ignore this.", true
}

// tabuSnippetCap bounds the stored error snippet so the tabu list stays small and the
// warning fed back to the model is a hint, not a re-dump of the whole failure.
const tabuSnippetCap = 200

// deliverableSigLocked returns a 64-bit signature over the CURRENT net contents of every
// file the agent has edited this turn (path + hashContent(after), in stable path order). It
// identifies "the deliverable in exactly this state" independent of the path order edits
// arrived in. 0 when the agent has authored nothing yet (no state to tabu). Caller holds g.mu.
func (g *runGuard) deliverableSigLocked() uint64 {
	if len(g.changeOrder) == 0 {
		return 0
	}
	paths := append([]string(nil), g.changeOrder...)
	sort.Strings(paths)
	h := fnv.New64a()
	for _, p := range paths {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(strconv.FormatUint(hashContent(g.changed[p].after), 16)))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// noteExerciseFail records the CURRENT deliverable state as tabu after an exercising command
// (a test/program run — not an inspect-only builtin) failed against it, keyed by the state's
// content-signature with a short snippet of what failed. Idempotent per signature (the first
// failure's snippet is kept). A no-op when the command was inspect-only or the agent has not
// authored a deliverable yet. errText is the failing tool's result content.
func (g *runGuard) noteExerciseFail(cmd, errText string) {
	if isInspectOnly(cmd) {
		return // inspecting state is not exercising the deliverable → not a tabu-worthy failure
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	sig := g.deliverableSigLocked()
	if sig == 0 {
		return
	}
	if _, seen := g.failedStates[sig]; seen {
		return
	}
	snip := strings.TrimSpace(errText)
	if len(snip) > tabuSnippetCap {
		snip = snip[:tabuSnippetCap] + "…"
	}
	snip = strings.ReplaceAll(snip, "\n", " ")
	g.failedStates[sig] = snip
}

// checkTabu reports whether the deliverable's CURRENT state (after the latest edit) matches a
// state whose exercise already failed this turn — i.e. the agent has circled back to a
// proven-bad approach. Returns a one-shot advisory (once per signature) citing the prior
// failure, or "" when the state is new, clean, or already warned. Advisory only: it never
// blocks the edit.
func (g *runGuard) checkTabu() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	sig := g.deliverableSigLocked()
	if sig == 0 || g.tabuWarned[sig] {
		return ""
	}
	snip, bad := g.failedStates[sig]
	if !bad {
		return ""
	}
	g.tabuWarned[sig] = true
	w := "note: this edit reproduces a deliverable state whose test already failed earlier this turn"
	if snip != "" {
		w += " (prior failure: " + snip + ")"
	}
	return w + " — that approach is on the tabu list; try a different fix, not this one again."
}

// recallBudget caps re-hydrations per turn (distinct topics bypass the identical-call
// loop guard, so they need their own ceiling).
const recallBudget = 8

// allowRecall reports whether a recall_context for `topic` is permitted this turn,
// with a reason when not: each topic once, and at most recallBudget total.
func (g *runGuard) allowRecall(topic string) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.recalled[topic] {
		return false, "already recalled this topic this turn — use what was returned earlier"
	}
	if g.recallCount >= recallBudget {
		return false, "recall budget for this turn is exhausted — work with the context you have or narrow the task"
	}
	g.recalled[topic] = true
	g.recallCount++
	return true, ""
}

// check records a tool call and reports whether it should be blocked as a repeat,
// how many times this exact call has been seen at the current epoch, and the
// fingerprint (so the caller can record/echo its result).
//
// The hard block applies only to repeats whose outcome provably cannot change: re-reading
// an unchanged file, re-running an inspect-only banner, replaying the identical write. An
// EXEC bash command (build/test/run — anything not inspect-only) is exempt: its outcome
// can legitimately change through state the guard cannot see (a `sed -i` the mutation
// heuristics missed, a dependency install, a daemon coming up), so blocking the third
// identical `go build`/`go test` obstructs real fix cycles. A genuine test-rerun spin is
// still terminated by the stall layer (sinceProgress keeps climbing here → nudges →
// force-stop); it just isn't hard-blocked call-by-call.
func (g *runGuard) check(name string, args json.RawMessage) (block bool, n int, fp string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fp = name + "\x00" + strconv.Itoa(g.epoch) + "\x00" + guardArgs(name, args)
	g.calls++
	g.sinceProgress++ // reset only by a real mutation (mutated); rises across varied calls
	g.seen[fp]++
	n = g.seen[fp]
	if n > repeatLimit {
		if name == "bash" && execExemptEnabled() {
			var ba struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(args, &ba) == nil && !isInspectOnly(ba.Command) {
				return false, n, fp // exec repeat — outcome may change; stall layer owns this
			}
		}
		g.blocked++
		return true, n, fp
	}
	return false, n, fp
}

// mutated records a successful file mutation and bumps the epoch ONLY when the change to
// `path` differs from the last mutation of that path (sig = the call's canonical args).
// An idempotent rewrite (writing identical content) is not progress, so it must NOT reset
// the repeat counts — otherwise a write-the-same-thing loop would never be caught.
// mutated returns whether it actually reset the progress counters (true) or short-circuited as
// an idempotent rewrite (false). The caller uses that so retractProgress only ever undoes a reset
// this same call produced.
func (g *runGuard) mutated(path, sig string) (reset bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastMut[path] == sig {
		return false // no real change → leave the loop-guard counters intact
	}
	g.lastMut[path] = sig
	g.epoch++
	g.prevSince = g.sinceProgress // snapshot for a possible retraction (see prevSince)
	g.prevStallAt = g.lastStallAt
	g.sinceProgress = 0         // a real change is progress → restart the no-progress count
	g.lastStallAt = 0           // …and the stall-nudge window, so a fresh stall re-arms cleanly
	g.execSinceMut = 0          // a new deliverable version is unverified until something exercises IT
	g.waitSinceMut = 0          // a real change is progress → the environment-wait ratio restarts too
	g.progressSinceNudge = true // a real mutation IS forward motion → protects the re-arm from collapse
	return true
}

// retractProgress reverses the sinceProgress/lastStallAt reset from the most recent mutated()
// call, for when a later content check reveals that edit returned the file to a state it already
// held this turn — a self-revert is churn, not progress. The epoch bump and lastMut record stay
// (the loop guard still sees fresh context); only the stall accounting resumes climbing, so an
// implement↔revert oscillation can no longer dodge the stall force-stop by zeroing the counter on
// every swing. Only call this when the mutated() for the same edit returned reset==true.
func (g *runGuard) retractProgress() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sinceProgress = g.prevSince
	g.lastStallAt = g.prevStallAt
	// The mutation being retracted was a self-revert (churn), not forward motion, so it must
	// NOT keep the D18a re-arm from collapsing. mutated() set progressSinceNudge=true; clear it
	// here so an implement↔revert oscillation — whose every swing re-sets that flag — no longer
	// dodges stall-converge and lands the honest stall on the same schedule as any other stall.
	g.progressSinceNudge = false
}

// resetStall clears the no-progress/stall accounting after a structural recovery — a stall
// or council deadlock handed the remaining work to a fresh child that re-plans (see
// redecomposeStuck). The child's writes land in ITS OWN guard, not this one, so without a
// reset the parent would keep the same climbed sinceProgress and immediately re-trip the
// stall force-stop, aborting the recovery before it can integrate/verify the child's result.
// It restarts the stall window and returns the nudge budget, giving the parent a clean run to
// finish. It deliberately does NOT touch the mutation epoch, changeSet, or blocked/repeat
// counters — those record the parent's own edits and exact-repeat loops, which the recovery
// does not undo.
func (g *runGuard) resetStall() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sinceProgress = 0
	g.lastStallAt = 0
	g.stallNudges = 0
	g.progressSinceNudge = false
}

// resetRepeat clears the blocked-repeat counter (and the stall window) after a "repeat"-kind
// stuck recovery lands. resetStall alone is not enough here: stuck() returns "repeat" the instant
// blocked >= blockedBudget, so without zeroing blocked the very next guard check would re-halt the
// parent before it can integrate and verify the recovery child's work. Zeroing blocked gives the
// parent a fresh window to continue — the same fresh window resetStall grants a stall recovery.
func (g *runGuard) resetRepeat() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.blocked = 0
	g.sinceProgress = 0
	g.lastStallAt = 0
	g.stallNudges = 0
	g.progressSinceNudge = false
}

const guardResultEcho = 4 << 10 // cap on the cached result echoed back on a block

// record stores a call's result content (capped) so a later blocked repeat can be
// handed the earlier output instead of a bare refusal.
func (g *runGuard) record(fp, content string) {
	if len(content) > guardResultEcho {
		cut := guardResultEcho
		for cut > 0 && !utf8.RuneStart(content[cut]) { // don't slice mid-rune
			cut--
		}
		content = content[:cut] + "\n…(truncated)"
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.results[fp] = content
}

// lastResult returns the previously recorded result for fp, if any.
func (g *runGuard) lastResult(fp string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.results[fp]
}

// recordChange captures a file's before/after content around an agent edit. The FIRST
// before for a path is kept (its pre-turn state); after is updated to the latest, so
// multiple edits to one file collapse to a single net before→after.
func (g *runGuard) recordChange(path, before, after string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.changed[path]
	if !ok {
		c = &fileChange{path: path, before: before}
		g.changed[path] = c
		g.changeOrder = append(g.changeOrder, path)
	}
	c.after = after
}

// changeSet returns this turn's file changes in first-seen order.
func (g *runGuard) changeSet() []fileChange {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]fileChange, 0, len(g.changeOrder))
	for _, p := range g.changeOrder {
		out = append(out, *g.changed[p])
	}
	return out
}

// noteBashWrite records a bash command that AUTHORS or MUTATES a file — a heredoc (`<<`),
// a `tee`, a `>`/`>>` redirect to a real path, or a redirect-less mutating command
// (`sed -i`, `patch`, `cp`, `git apply`, `pip install`, … — see mutatesFiles) — and bumps
// the mutation epoch for it. It excludes file-descriptor duplications (`2>&1`, `>&2`) and
// `/dev/*` sinks (`>/dev/null`), which capture or discard a command's output rather than
// produce a deliverable (see redirectsToFile); read-only commands (a bare `grep`/`cat`)
// also return false and do not bump. Without the mutatesFiles arm, a bash-driven fix
// cycle (sed -i → build → test, repeat) registers no progress and the loop guard blocks
// the re-run build/test as an identical no-progress repeat. It returns whether the
// command authored a file.
func (g *runGuard) noteBashWrite(cmd string) bool {
	if !redirectsToFile(cmd) && !(execExemptEnabled() && mutatesFiles(cmd)) {
		return false
	}
	// A bash command that writes a file IS real progress — the tool-agnostic twin of
	// write/edit's epoch bump. Without this, bash-heavy tasks (most Terminal-Bench
	// work) climb sinceProgress while genuinely producing files, misfiring stall
	// nudges and, post-nudge-exhaustion, the stall force-stop. mutated() keys on the
	// command text itself, so re-running the IDENTICAL write is still not progress.
	g.mutated("\x00bash", cmd)
	return true
}

// noteBashExec records that a bash command actually EXERCISED the deliverable — it ran a
// program or test, as opposed to inspecting state. Classification is by leading verb alone
// (see isInspectOnly), INDEPENDENT of any redirect: `pytest 2>&1`, `python x.py > log`, and
// `./run > out` all count as execution even though they redirect, whereas `echo … > f` or a
// heredoc does not (its verb is inspect-only, i.e. it authors content rather than runs it).
// Everything not inspect-only — `python`, `pytest`, `go test`, `./run`, `make`, a script —
// is real execution evidence for the CURRENT deliverable version. novel (the guard.check n==1
// for this same call) marks a first-seen exercise this epoch: only a NEW exercising command is
// forward motion for the stalled-nudge convergence (re-running an already-run test on an
// unchanged deliverable is not), so it sets progressSinceNudge; execSinceMut counts every
// exercise as before (its unverifiedDeliverable semantics are unchanged).
func (g *runGuard) noteBashExec(cmd string, novel bool) {
	if isInspectOnly(cmd) {
		// A NOVEL inspection is not deliverable progress, but it IS a response to the
		// "take a different action" redirect: the agent demonstrably changed direction
		// (a new grep pattern, a new file). Counting it keeps the D18a collapse for
		// true head-banging (only already-seen fingerprints after the nudge) while a
		// genuine pivot keeps its full nudge budget. execSinceMut is deliberately NOT
		// bumped — inspection remains non-exercise for unverifiedDeliverable.
		if novel && stallNoveltyEnabled() {
			g.mu.Lock()
			g.progressSinceNudge = true
			g.mu.Unlock()
		}
		return
	}
	g.mu.Lock()
	g.execSinceMut++
	if novel {
		g.progressSinceNudge = true // a first-seen exercising command is forward motion
	}
	// Per-artifact exercise ledger (exec-evidence layer 1): an EXERCISING command that
	// names an authored file marks that file as having actually been run/loaded at
	// least once. Inspection commands returned above never reach here, so `cat x.py`
	// does not count as running x.py.
	for path := range g.changed {
		if g.exercisedFile[path] {
			continue
		}
		if base := filepath.Base(path); base != "" && strings.Contains(cmd, base) {
			g.exercisedFile[path] = true
		}
	}
	g.mu.Unlock()
}

// runnableExt lists extensions whose files plausibly EXECUTE (a program the turn
// should have run at least once before claiming done). Docs/config/data files are
// excluded on purpose: "authored but never executed" is only meaningful for code.
var runnableExt = map[string]bool{
	".py": true, ".sh": true, ".js": true, ".ts": true, ".go": true, ".c": true,
	".cc": true, ".cpp": true, ".rs": true, ".rb": true, ".pl": true, ".php": true,
	".java": true, ".mjs": true,
}

// unexercisedArtifacts returns this turn's authored runnable files that no
// exercising command ever named — the deterministic "written but never run" fact
// the exec-evidence nudge and the council evidence trailer are built on. A file
// can also become exercised retroactively (written after an earlier mention is
// impossible — mentions are only recorded for already-authored files — so a late
// write followed by no run stays listed, which is exactly the failure mode).
func (g *runGuard) unexercisedArtifacts() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, p := range g.changeOrder {
		c := g.changed[p]
		if c == nil || strings.TrimSpace(c.after) == "" {
			continue // deletion/emptied — nothing to run
		}
		if !runnableExt[strings.ToLower(filepath.Ext(p))] {
			continue
		}
		if !g.exercisedFile[p] {
			out = append(out, p)
		}
	}
	return out
}

// noteBashWait records that a bash command only WAITED or POLLED (isWaitCommand) — a delay or an
// external-readiness probe — since the last mutation. It is called for EVERY bash call regardless
// of exit code: a poll to a not-yet-ready endpoint FAILS (a down host, a refused connection) and
// that failing poll is exactly the wait we must count, so gating on success would miss the common
// case. It bumps no epoch and resets no progress counter — waiting is not progress — so the stall
// force-stop still eventually caps an endless wait; it only feeds stallIsWait's recovery-suppression.
func (g *runGuard) noteBashWait(cmd string) {
	if !isWaitCommand(cmd) {
		return
	}
	g.mu.Lock()
	g.waitSinceMut++
	g.mu.Unlock()
}

// stallIsWait reports whether the current no-progress window is dominated by waiting/polling — at
// least half of the sinceProgress calls since the last mutation were wait commands. It gates the
// stuck-recovery coder spawn (loop.go): a coder cannot speed an external wait, so a wait-dominated
// stall must NOT trigger the redecompose cascade, which under delegate-off spawns coder→coder and
// whose child timeout gets misreported as the whole run's context-deadline. Suppressing recovery
// still lets the honest stall stop land (delivered→clean finish, or stall_guard), so an unbounded
// wait remains capped — this removes only the futile, harmful recovery, not the backstop.
func (g *runGuard) stallIsWait() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sinceProgress > 0 && g.waitSinceMut*2 >= g.sinceProgress
}

// noteSpin updates the consecutive completion-banner counter for one executed tool call. A
// bash call whose command is a pure no-op banner (isNoOpBanner) increments bannerSpin; ANY
// other tool call — a non-banner bash, a write/edit, a read, todowrite, … — is a real action
// and resets it to 0. It is called once per executed call from execute.go (blocked calls
// return before that point, so they never count). cmd is "" for non-bash tools.
func (g *runGuard) noteSpin(name, cmd string) {
	g.mu.Lock()
	if name == "bash" && isNoOpBanner(cmd) {
		g.bannerSpin++
	} else {
		g.bannerSpin = 0
	}
	g.mu.Unlock()
}

// unverifiedDeliverable reports the structural, language-agnostic fabrication signal that
// replaced the English-only phrase scan: the agent produced or changed a deliverable this
// turn (epoch>0) yet has run NO command exercising the CURRENT version (execSinceMut==0).
// That is exactly "wrote/edited a result, then declared done without ever running it" —
// whether the artifact confesses in prose (any language) or not. It is ADVISORY: a task
// with genuinely nothing to run (write one config file) also matches, so callers feed it to
// the council and a one-shot nudge, never a hard block.
func (g *runGuard) unverifiedDeliverable() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.epoch > 0 && g.execSinceMut == 0
}

// stuck reports why the run should be force-stopped: "repeat" (the same action
// blocked past blockedBudget), "stall" (all stall nudges spent AND another full
// no-progress window elapsed with still no mutation — the agent is ignoring
// redirection and would otherwise wander to MaxSteps, which at the 240 default
// means ~200 unguarded steps), or "" to keep going. A real mutation resets
// sinceProgress and lastStallAt (see mutated), so productive work never trips it.
func (g *runGuard) stuck() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocked >= blockedBudget {
		return "repeat"
	}
	// Ordered before "stall" on purpose: a spin force-stop must SKIP the redecompose recovery
	// (which is gated on kind=="stall" in loop.go) — handing a spinning agent to a fresh child
	// only risks the same banner spin in the child. Returning "spin" here lands the run directly.
	if g.bannerSpin >= bannerSpinStop {
		return "spin"
	}
	if g.stallNudges >= maxStallNudges && g.sinceProgress-g.lastStallAt >= noProgressNudge {
		return "stall"
	}
	return ""
}

// callCount returns the total tool calls recorded this run.
func (g *runGuard) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// mutationEpoch returns the current mutation epoch — the number of real file mutations
// (edit/write AND bash file-writes via noteBashWrite) this run. It rises only on genuine
// deliverable changes (a read-only bash or an idempotent rewrite does not bump it), so it
// is the version stamp the fresh-evidence gate compares against: a tester PASS recorded at
// epoch N is stale the moment a later mutation bumps the epoch past N, forcing re-check.
func (g *runGuard) mutationEpoch() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.epoch
}

// shouldNudge reports whether the run has stalled enough to warrant a corrective
// re-grounding, and which KIND of stall it is: "blocked" (the same action repeated past
// nudgeThreshold) or "stalled" (many varied calls with no real progress, sinceProgress past
// noProgressNudge). The two kinds are independent. The blocked nudge fires once — stuck()
// force-stops the run if it keeps blocking. The stalled nudge has no force-stop backstop, so
// it RE-ARMS: it fires again after each further noProgressNudge window with still no mutation,
// capped at maxStallNudges, so a single ignored nudge does not let the agent burn to MaxSteps.
// Returns "" when no nudge is due.
func (g *runGuard) shouldNudge() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocked >= nudgeThreshold && !g.nudgedBlocked {
		g.nudgedBlocked = true
		return "blocked"
	}
	// Spin is more specific than the stalled re-arm below (a run of pure completion banners),
	// so it takes precedence: a one-shot hint to end the turn instead of echoing "done" again.
	if g.bannerSpin >= bannerSpinNudge && !g.nudgedSpin {
		g.nudgedSpin = true
		return "spin"
	}
	if g.stallNudges < maxStallNudges && g.sinceProgress-g.lastStallAt >= noProgressNudge {
		// D18a convergence: a re-arm (>=1 nudge already fired) whose window produced NO
		// forward motion since the last nudge — neither a real mutation NOR a NOVEL exercising
		// command (progressSinceNudge is false) — means the redirect was ignored. Collapse the
		// remaining nudge budget so stuck() lands the honest stall THIS iteration (its
		// stallNudges>=max and window>=threshold conditions are both met now — lastStallAt is
		// left untouched on purpose so the window stays >= threshold), instead of firing up to
		// maxStallNudges more nudges and burning that many further no-progress windows. Same
		// terminal outcome, sooner. NOTE: a mutation sets progressSinceNudge=true (and restarts
		// the window), so an agent that edited a file after the nudge re-arms normally — collapse
		// only cuts a window with genuinely no progress, never a productive redirect.
		if g.stallConverge && g.stallNudges >= 1 && !g.progressSinceNudge {
			g.stallNudges = maxStallNudges
			return ""
		}
		g.stallNudges++
		g.lastStallAt = g.sinceProgress
		g.progressSinceNudge = false // fresh window for judging the next re-arm
		return "stalled"
	}
	return ""
}

// toolResultCap bounds a single tool result fed back to the model. ~64KB ≈ 16k tokens:
// large enough for real output, small enough that one giant result (e.g. reading a 500KB
// file) can't overflow the context window past what compaction can recover.
const toolResultCap = 64 << 10

// capToolResult truncates an oversized tool result on a rune boundary, appending a note
// that tells the agent to narrow its read/command rather than silently losing data.
func capToolResult(b []byte) []byte {
	if len(b) <= toolResultCap {
		return b
	}
	cut := toolResultCap
	for cut > 0 && !utf8.RuneStart(b[cut]) { // don't split a multibyte rune
		cut--
	}
	marker := fmt.Sprintf(
		"\n\n…[output truncated: showing %d of %d bytes — re-run with a narrower range or filter "+
			"(read offset/limit, grep, head/tail) to see the rest]", cut, len(b))
	out := make([]byte, 0, cut+len(marker))
	out = append(out, b[:cut]...)
	return append(out, marker...)
}

// changeReadCap bounds how much of a file we read to reconstruct a before/after change.
// (The LCS memory is bounded separately by change.maxDiffInputLines, so this only limits
// I/O; it's large enough to capture edits that aren't right at the top of a big file.)
const changeReadCap = 256 << 10

// readForChange reads (a capped prefix of) the file at a tool-supplied path, relative to
// workdir, for before/after change capture. "" on any error (e.g. a new or deleted file).
func readForChange(workdir, path string) string {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workdir, path)
	}
	f, err := os.Open(abs)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, changeReadCap)
	n, _ := io.ReadFull(f, b)
	return string(b[:n])
}

// relForChange maps a tool path to a workdir-relative display path for change headers.
func relForChange(workdir, path string) string {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workdir, path)
	}
	if rel, err := filepath.Rel(workdir, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return path
}

// councilFileFullCap is the per-file size (bytes) up to which the council is shown the
// file's CURRENT content in full instead of a before→after diff. A member judging
// correctness needs the actual current source: over a many-rewrite turn the first-before
// vs latest-after diff clamps ("… (N more)" / "≈ large change"), and members were observed
// rejecting on that literally — "the file content as reported is incomplete (uses
// ellipsis)" — while a dead code path stayed invisible because no member ever saw the
// whole file. Diffs remain for larger files, where they are the more compact evidence.
const councilFileFullCap = 4000

// buildCouncilChanges renders the turn's file changes as the council's evidence: per file,
// the full current content when it is small (councilFileFullCap), otherwise a "### path"
// header with the before→after line diff. Files whose diff is empty are skipped. The
// caller caps the total.
func buildCouncilChanges(cs []fileChange) string {
	var b strings.Builder
	for _, c := range cs {
		d := change.LineDiff(c.before, c.after)
		if diffOnlyContext(d) {
			continue // no additions/removals (e.g. an identical rewrite) → nothing to show
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		if n := len(c.after); n > 0 && n <= councilFileFullCap {
			b.WriteString("### " + c.path + " (current content, full)\n" + c.after + "\n")
			continue
		}
		b.WriteString("### " + c.path + "\n" + d + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// diffOnlyContext reports whether a LineDiff has no real change — empty, or every non-empty
// line is context (" "-prefixed). A "+"/"-" line, the "… (N more)" clamp note, or the
// "≈ large change" summary all start non-space, so a real change is never dropped.
func diffOnlyContext(d string) bool {
	for _, ln := range strings.Split(d, "\n") {
		if ln != "" && !strings.HasPrefix(ln, " ") {
			return false
		}
	}
	return true
}

// canonicalArgs returns a stable string for tool args so logically identical
// calls fingerprint equally regardless of JSON key ordering or whitespace.
func canonicalArgs(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.Marshal(v) // Go marshals map keys in sorted order
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// guardArgs canonicalizes a call's args for the loop-guard fingerprint. It is
// stricter than canonicalArgs for tools whose no-progress repeats can hide
// behind a volatile parameter: a `read` that re-reads the SAME file head while
// only nudging `limit` (offset unchanged) is the same no-progress step, yet a
// full-args fingerprint treats limit=60/65/70 as three distinct calls, so each
// counter stays under repeatLimit and the exact-repeat block never fires — the
// model paces a read loop past the guard. Dropping `limit` collapses those onto
// one fingerprint so the block engages. Genuine paging advances `offset` (a
// different head), which is kept, so real forward reads are unaffected.
func guardArgs(name string, raw json.RawMessage) string {
	if name != "read" {
		return canonicalArgs(raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return canonicalArgs(raw) // not an object (or malformed) — fall back
	}
	delete(m, "limit")
	b, err := json.Marshal(m) // sorted keys
	if err != nil {
		return canonicalArgs(raw)
	}
	return string(b)
}
