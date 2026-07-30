package app

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/change"
)

// Thresholds for NOTICING that an agent is repeating itself without progress. They used to
// refuse the call and then end the run; now they only decide when to SAY so. The counting is
// unchanged — what changed is that the answer is a sentence to the agent rather than a verdict
// about it.
const (
	repeatLimit = 4 // identical tool calls before the repeat is worth remarking on (raised from 2:
	//                   a legitimate check→act→re-check→act rhythm tripped it too eagerly)
	// nudgeThreshold: at this many counted repeats the agent is clearly circling, so ONE corrective
	// re-grounding is injected (re-read the task, change approach). It fires once — repeating the
	// same nudge at a model that is already thrashing tends to deepen the thrash.
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
	lastMut map[string]string // path → last mutation signature, to ignore idempotent rewrites
	epoch   int               // bumped on each real file mutation; part of the fingerprint
	blocked int
	// sinceProgress counts tool calls since the last real file mutation (epoch bump). It
	// powers the no-progress nudge: unlike `blocked` (which needs an EXACT repeat), it rises
	// even when the agent varies its commands, catching a stall that changes nothing.
	sinceProgress int
	// prevSince/prevStallAt snapshot sinceProgress/lastStallAt just before mutated() zeroes them,
	// so retractProgress can restore the climb when a later content check reveals that "mutation"
	// was a self-revert (churn, not forward progress) — otherwise an implement↔revert oscillation
	// resets the counter each swing and never trips the stall force-stop.
	prevSince   int
	prevStallAt int
	// can ALSO restore the STEP-based idle window on a self-revert. Without this, an oscillating
	// edit loop (rewrite→prior-state→rewrite) refills the step-based "idle" trigger on every swing
	// even though retractProgress restored the tool-call counter — so a reasoning-heavy churn (few
	// tool calls, mostly analysis + oscillating rewrites) dodges BOTH stall paths and burns to the
	// external wall clock (custom-memory-heap-crash: 125+ steps rewriting the same stub to timeout).
	// spend the one-shot budget only on a REAL deliverable version. Without it a revert loop past
	// the nudge threshold re-fires "act now" on every swing (the restored stepsSinceMut is already
	// in the nudge window, and the re-arm cleared the latch) — and a repeated nudge is exactly what
	// pushes a weak model to keep thrashing (see noteEdit on the once-per-file warning).
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

	// changed records this turn's file edits (before/after content) as the council's
	// evidence of what the AGENT actually changed — reconstructed from its own write/edit
	// tools, not git, so a human/external/bash change is never mis-attributed to the agent.
	changed     map[string]*fileChange
	changeOrder []string // first-seen order, for stable rendering
	// readSpans is which lines of each path magi has already handed over, so re-opening a window
	// it already delivered is not mistaken for gathering new information (noteReadCoverage).
	readSpans map[string][]lineSpan

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
	// regressCount counts, per file, how many times a write returned it to a content state it
	// already held this turn. It is a COUNT rather than a flag because the report is the only
	// channel left: the guard no longer stops anything, so a swing that goes unmentioned is a
	// swing the agent never learns about. Observed live — after the first note was spent, a file
	// went L→M→L→M→L over five more writes in silence while the byte-identical branch beside it
	// spoke every time, so the weaker signal repeated and the stronger one did not.
	regressCount map[string]int
}

// fileChange is one file's before/after content captured around an agent edit this turn.
type fileChange struct {
	path   string
	before string // content just before the FIRST edit this turn ("" if newly created)
	after  string // content just after the LATEST edit
}

func newRunGuard() *runGuard {
	return &runGuard{
		seen:    map[string]int{},
		lastMut: map[string]string{}, changed: map[string]*fileChange{},
		readSpans: map[string][]lineSpan{},
		recalled:  map[string]bool{}, contentHist: map[string][]uint64{},
		regressCount: map[string]int{},
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
	// Nothing on either side is not a rewrite of anything, and magi cannot tell the two shapes
	// apart from content alone: `write{content:""}` on a path that did not exist CREATES a file,
	// and the same call on an already-empty one changes nothing. Saying "nothing changed" picks
	// one of those without having measured which — so say nothing. The bash path guarded this at
	// its own call site; the write/edit path did not, and reached here.
	if before == "" && after == "" {
		return "", false
	}
	h := hashContent(after)
	if h == hist[len(hist)-1] {
		// Byte-identical to what the file already held. Not a regression — nothing moved either
		// way — but the agent has no way to see that: the tool answers "wrote 215 bytes", which
		// reads as a change. Measured: one turn wrote the same 215 bytes to the same path nine
		// times over five minutes, then the same 225 bytes eight more times over thirteen, and
		// magi never said a word. So say the one thing it knows and nothing more; the reading of
		// it — deliberate re-assert, or an edit that did not take — is the agent's.
		g.contentHist[path] = hist // materialize the baseline for a file first seen here
		return "this write left the file byte-for-byte as it already was — nothing changed.", false
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
	// credit each time), and say so every time as well. The first note reads as an aside a
	// deliberate revert can ignore; a later one states the pattern and how big it has grown,
	// because by then "I meant to do that" no longer explains it. Neither asks for a particular
	// edit — the guard reports, and what to do about it is the agent's call.
	g.regressCount[path]++
	if n := g.regressCount[path]; n > 1 {
		// It used to end in "no command ran between some of those writes". That clause was first
		// asserted unconditionally, then measured from an exercise counter — and the counter was
		// fed by a verb table deciding which commands "run" something. A sentence whose truth rests
		// on a hand-maintained list of verbs is a guess wearing a measurement's clothes, and this
		// one was an accusation. What is left is counted: how often, and how many versions.
		return fmt.Sprintf("note: this file has now returned to a content state it already held "+
			"%d times this turn, moving among %d distinct versions. If cycling between versions "+
			"is not getting you there, the next thing to change is probably not this file.",
			n, distinctStates(g.contentHist[path])), true
	}
	return "note: this edit restored a content state this file already had earlier this turn — " +
		"if reverting your own earlier change was intentional, ignore this.", true
}

// distinctStates counts how many different content states a file has held this turn, so a swing
// report can say how wide the cycle is rather than only that one happened. The pre-turn baseline
// counts as a state: returning to it is the original→fix→original shape.
func distinctStates(hist []uint64) int {
	seen := make(map[uint64]bool, len(hist))
	for _, h := range hist {
		seen[h] = true
	}
	return len(seen)
}

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
// repeatEpoch is the fingerprint's "the world may have moved, ask again" term. For most tools it is
// the mutation epoch: an identical `go test` after an edit is a new question, so it must not be
// counted as a repeat of the one before it.
//
// For a file-modifying call the epoch is the wrong term, in both directions. It is too COARSE —
// every other mutation bumps it, including a scratch redirect the agent makes between two writes,
// which hands the next identical write a fresh count of 1. Measured: the same 215 bytes written to
// one path nine times in five minutes, then the same 225 bytes eight more times, each pair separated
// by a `… > /tmp/…`; the repeat block never came within reach of a loop check's own contract names
// ("replaying the identical write"). And it is too BLUNT — a write is not outcome-independent in
// general: after the file has been changed to something else, writing the original bytes back does
// change it, and must be asked afresh.
//
// What the term should track is the one thing that decides both: whether THAT PATH has changed since.
// lastMut holds exactly that — the args of the last mutation recorded for the path — so an identical
// write accumulates while the file stands still, and starts over the moment it does not.
func (g *runGuard) repeatEpoch(name string, args json.RawMessage) string {
	if fileModifiers[name] {
		return g.lastMut[pathArg(args)]
	}
	return strconv.Itoa(g.epoch)
}

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
	fp = name + "\x00" + g.repeatEpoch(name, args) + "\x00" + guardArgs(name, args)
	g.calls++
	g.sinceProgress++ // reset only by a real mutation (mutated); rises across varied calls
	g.seen[fp]++
	n = g.seen[fp]
	if n > repeatLimit {
		g.blocked++
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
	// Restore the step-based idle window too: a self-revert is not a fresh deliverable version, so
	// the "reasoning rabbit hole" counter (stepsSinceMut, drives stuck()=="idle") must keep climbing
	// across an oscillating rewrite loop instead of being zeroed on every swing — otherwise a
	// reasoning-heavy churn never trips the idle recovery and runs to the wall-clock kill.
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
// cycle (sed -i → build → test, repeat) registers no progress while genuinely producing
// files. It returns whether the command authored a file, and whether that bump reset the
// progress counters.
func (g *runGuard) noteBashWrite(cmd string) (authored, reset bool) {
	if !redirectsToFile(cmd) && !mutatesFiles(cmd) {
		return false, false
	}
	// A bash command that writes a file IS real progress — the tool-agnostic twin of
	// write/edit's epoch bump. Without this, bash-heavy tasks (most Terminal-Bench
	// work) climb sinceProgress while genuinely producing files, misfiring stall
	// nudges. mutated() keys on the
	// command text itself, so re-running the IDENTICAL write is still not progress.
	//
	// reset says whether that bump actually restarted the progress counters, so the caller can
	// hand it back (retractProgress) when a content read shows the command undid an earlier state.
	// It matters here more than on the edit path: every bash mutation shares the single "\x00bash"
	// slot and is compared by COMMAND TEXT, so two commands that undo each other — `sed -i` then
	// `cp f.bak f` — always read as two different changes no matter how many times they alternate.
	return true, g.mutated("\x00bash", cmd)
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
// noteInspectProgress credits a NOVEL read-only inspection (a first-seen read/grep/glob/
// list — a DIFFERENT file or query) as forward motion for the stall window: gathering
// new information is real progress, not the "restating the same conclusion" loop the
// stall nudge targets. Reading MANY distinct files in a row must not trip the nudge
// (the reported false stall). A REPEATED read is not novel and is left to climb — the
// read-loop guard already handles re-reading the same file. It advances lastStallAt to
// the current window position (not a full reset) so an agent that only ever inspects,
// never acting, still eventually stalls; each novel inspect just buys one more window.
func (g *runGuard) noteInspectProgress(novel bool) {
	if !novel {
		return
	}
	g.mu.Lock()
	g.lastStallAt = g.sinceProgress // window measured from here → this novel inspect reset it
	g.progressSinceNudge = true
	g.mu.Unlock()
}

// lineSpan is a half-open range of file lines [lo, hi) magi has already handed to the agent.
type lineSpan struct{ lo, hi int }

// noteReadCoverage records the line window a read delivered and reports whether MOST of that
// window was content the agent had not been shown before.
//
// It exists because "novel" for a read is decided by the call's fingerprint, and a read's
// fingerprint carries its offset. Nudging the offset by two lines therefore produced a first-seen
// call — which noteInspectProgress credits as gathering new information, resetting the stall
// window. Observed live (fix-ocaml-gc, 49 minutes, no edit and one build): reads of ONE file at
// offsets 642, 640, 640, 642 — every window already delivered several times over — bought eight
// window resets, so the stall nudge never fired once in 88 minutes. magi's own record was saying
// `already looked at more than once: shared_heap.c ×25` the whole time; the two halves of magi
// disagreed, and the half wired to the counter was the one that was wrong.
//
// MOST, not any: a window that starts two lines earlier than one already read is a re-read no
// matter where it starts, but a window with a couple of lines of overlap at its edge is the normal
// shape of paging forward through a file. Half is the point where "mostly already seen" stops
// being a judgement call. Paging a large file in sequence stays fully credited — each page is new
// — which is the false stall this credit was introduced to avoid.
//
// offset is the read tool's 1-based start (0 = from the top) and limit its line count (0 = the
// tool's own default). Both come from the model's own call; the guard invents nothing.
func (g *runGuard) noteReadCoverage(path string, offset, limit int) bool {
	if path == "" {
		return true // nothing to key coverage on — leave the call's own novelty alone
	}
	lo := offset
	if lo < 1 {
		lo = 1
	}
	if limit <= 0 {
		limit = builtin.DefaultReadLines
	}
	hi := lo + limit

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.readSpans == nil {
		g.readSpans = map[string][]lineSpan{}
	}
	seen := 0
	for _, s := range g.readSpans[path] {
		a, b := max(lo, s.lo), min(hi, s.hi)
		if b > a {
			seen += b - a
		}
	}
	g.readSpans[path] = mergeSpans(append(g.readSpans[path], lineSpan{lo, hi}))
	// seen can exceed the window only if the stored spans overlapped each other, which mergeSpans
	// prevents; the comparison is on fresh lines against half the window either way.
	return (limit-seen)*2 > limit
}

// dropReadCoverage forgets what magi has shown of one path, for a caller that knows the file's
// contents changed. mutated() does this for the write/edit path; bash mutations share a single
// synthetic slot there and have to name their destination here.
func (g *runGuard) dropReadCoverage(path string) {
	if path == "" {
		return
	}
	g.mu.Lock()
	delete(g.readSpans, path)
	g.mu.Unlock()
}

// mergeSpans normalizes a span list into disjoint, ascending ranges so coverage stays O(regions)
// rather than O(reads) — a run that pages a file a hundred times keeps one span, not a hundred.
func mergeSpans(in []lineSpan) []lineSpan {
	if len(in) < 2 {
		return in
	}
	sort.Slice(in, func(i, j int) bool { return in[i].lo < in[j].lo })
	out := in[:1]
	for _, s := range in[1:] {
		last := &out[len(out)-1]
		if s.lo <= last.hi { // touching counts as contiguous: [1,10) and [10,20) are one region
			last.hi = max(last.hi, s.hi)
			continue
		}
		out = append(out, s)
	}
	return out
}

func (g *runGuard) noteBashExec(cmd string, novel bool) {
	if isInspectOnly(cmd) {
		// A NOVEL inspection is not deliverable progress, but it IS a response to the
		// "take a different action" redirect: the agent demonstrably changed direction
		// (a new grep pattern, a new file). Counting it keeps the D18a collapse for
		// true head-banging (only already-seen fingerprints after the nudge) while a
		// genuine pivot keeps its full nudge budget. execSinceMut is deliberately NOT
		if novel && stallNoveltyEnabled() {
			g.mu.Lock()
			g.progressSinceNudge = true
			g.mu.Unlock()
		}
		return
	}
	g.mu.Lock()
	if novel {
		g.progressSinceNudge = true // a first-seen exercising command is forward motion
	}
	g.mu.Unlock()
}

// importedByStem lists the extensions whose language names a source file by its stem when loading
// it — `import run`, `require("./run")`. A compiled language that builds by path or package
// directory is absent: there the file's own name is what a command carries.
var importedByStem = map[string]bool{
	".py": true, ".rb": true, ".js": true, ".mjs": true, ".ts": true,
}

// cmdMentionsFile reports whether cmd names the file with basename `base` as a whole path
// component, not merely as a substring of a LONGER filename — `python ax.py` must NOT mark the
// authored file `x.py` as exercised, or a written-but-never-run artifact silently drops off the
// exec-evidence ledger. The base must sit bounded by a path separator, whitespace, quote, or shell
// metacharacter on each side (or the string edge), the way a real filename token appears in a
// command; a filename-body byte (letter/digit/._-) adjacent to it means it is part of another name.
func cmdMentionsFile(cmd, base string) bool {
	for from := 0; ; {
		i := strings.Index(cmd[from:], base)
		if i < 0 {
			return false
		}
		i += from
		end := i + len(base)
		beforeOK := i == 0 || !isFilenameByte(cmd[i-1])
		afterOK := end == len(cmd) || !isFilenameByte(cmd[end])
		if beforeOK && afterOK {
			return true
		}
		from = i + 1
	}
}

// isFilenameByte reports whether b can appear inside a filename token (so an adjacent one means the
// candidate base is really part of a longer name).
func isFilenameByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-' || b == '.'
}

// runnableExt lists extensions whose files plausibly EXECUTE (a program the turn
// should have run at least once before claiming done). Docs/config/data files are
// excluded on purpose: "authored but never executed" is only meaningful for code.
var runnableExt = map[string]bool{
	".py": true, ".sh": true, ".js": true, ".ts": true, ".go": true, ".c": true,
	".cc": true, ".cpp": true, ".rs": true, ".rb": true, ".pl": true, ".php": true,
	".java": true, ".mjs": true,
}

// callCount returns the total tool calls recorded this run.
func (g *runGuard) callCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.calls
}

// normalizeExerciseCmd collapses whitespace runs to single spaces and trims, so a command counts as
// "the same" across edits when it is byte-identical modulo spacing. Deliberately conservative — it
// does NOT strip volatile flags (a display filter like `| tail -5` splits the key from `| tail -3`),
// which errs toward NOT landing (a split key climbs slower), the safe direction for a force-stop.
func normalizeExerciseCmd(cmd string) string {
	return strings.Join(strings.Fields(cmd), " ")
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

// capToolResult truncates an oversized tool result, appending a note that tells the agent to narrow
// its read/command rather than silently losing data.
//
// A tool result's content is a JSON DOCUMENT — a tool builds it with json.Marshal — and cutting a
// JSON document's bytes does not leave a JSON document. Cutting the text payload `"line\nline\n…"`
// at a byte offset leaves an unterminated string literal, which the event marshaller then refuses,
// and the refusal was discarded (see appendPart): the tool call was recorded with a null payload and
// the agent received NO result at all — not an error, not a truncation, nothing.
//
// Measured: two reads, of the two files the task was about, of 64KB+ each.
//
//	03:04:58  read /app/ocaml/runtime/major_gc.c     → part.appended, data: null
//	03:06:18  read /app/ocaml/runtime/shared_heap.c  → part.appended, data: null
//
// The agent's next message each time moved on to something else, and it never found the bug.
//
// So the cut happens INSIDE the payload, and the result is re-encoded. A structured payload cannot
// be cut at all — half an object is not an object — so it is replaced by a note saying so, which is
// a true statement the agent can act on. Content that is not JSON (a plugin returning raw bytes)
// keeps the byte-wise cut it always had.
func capToolResult(b []byte) []byte {
	if len(b) <= toolResultCap {
		return b
	}
	// Both numbers in the SAME unit. It used to pair the decoded cut with len(b), the ENCODED
	// length, so a payload full of escapes was told "showing 49152 of 86403" when 86403 counted
	// bytes it never had in that form — the shown fraction came out smaller than what it actually
	// received. The caller passes the total that matches what it cut.
	marker := func(shown, total int) string {
		return fmt.Sprintf(
			"\n\n…[output truncated: showing %d of %d bytes — re-run with a narrower range or filter "+
				"(read offset/limit, grep, head/tail) to see the rest]", shown, total)
	}
	var text string
	if json.Unmarshal(b, &text) == nil {
		// The cap is on the ENCODED bytes, and encoding expands: each \n, \t and " in the payload
		// becomes two bytes. So a decoded string comfortably under the cap can encode over it —
		// which is exactly the case the old code got wrong. It clamped cut to len(text) and then
		// read text[cut], one past the end, and the panic took the whole run down with it: observed
		// live on a bare read of a 62 KiB source file whose JSON form was past 64 KiB, eight tool
		// calls into a three-hour task that then reported exit 2 and scored 0.
		//
		// So cut the decoded text, re-encode, and shrink until the encoded form fits. The expansion
		// ratio is bounded, so this converges in a handful of passes.
		cut := len(text)
		if cut > toolResultCap {
			cut = toolResultCap
		}
		for {
			// Never index at len(text) — the end of a string is already a rune boundary.
			for cut > 0 && cut < len(text) && !utf8.RuneStart(text[cut]) {
				cut--
			}
			out, err := json.Marshal(text[:cut] + marker(cut, len(text)))
			if err != nil {
				break
			}
			if len(out) <= toolResultCap || cut == 0 {
				return out
			}
			cut = cut * 3 / 4
		}
	}
	if json.Valid(b) {
		// Structured output (a JSON object/array). There is no prefix of it that is still itself, so
		// say what happened instead of handing back something that cannot be parsed.
		out, err := json.Marshal(fmt.Sprintf("[result omitted: %d bytes of structured output exceeded "+
			"the %d-byte result cap — re-run with a narrower query so the answer fits]", len(b), toolResultCap))
		if err == nil {
			return out
		}
	}
	cut := toolResultCap
	for cut > 0 && !utf8.RuneStart(b[cut]) { // don't split a multibyte rune
		cut--
	}
	m := marker(cut, len(b))
	out := make([]byte, 0, cut+len(m))
	out = append(out, b[:cut]...)
	return append(out, m...)
}

// pathExists reports whether a tool-supplied path resolves to something on disk right now. magi
// records an absent file and an empty one as the same content (""), so a command that DELETED a
// file looks, to the content history, exactly like one that returned it to an earlier state. The
// two are told apart by the same stat readForChange already makes — just kept.
func pathExists(workdir, path string) bool {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workdir, path)
	}
	_, err := os.Stat(abs)
	return err == nil
}

// changeReadCap bounds how much of a file we read to reconstruct a before/after change.
// (The LCS memory is bounded separately by change.maxDiffInputLines, so this only limits
// I/O; it's large enough to capture edits that aren't right at the top of a big file.)
const changeReadCap = 256 << 10

// readForChange reads (a capped prefix of) the file at a tool-supplied path, relative to
// workdir, for before/after change capture. "" on any error (e.g. a new or deleted file).
func readForChange(workdir, path string) (string, bool) {
	abs := path
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(workdir, path)
	}
	// A DIRECTORY is not readable content, and os.Open opens one happily — the read then yields
	// nothing and the path compares equal to itself before and after. Observed live:
	// `rm -rf boot …` had `boot` extracted as its destination, both reads came back empty, and the
	// result carried "this write left the file byte-for-byte as it already was" about a command
	// that had just deleted a directory tree. A path magi cannot read the content of is one it
	// cannot say anything true about, so it says nothing.
	st, err := os.Stat(abs)
	switch {
	case err == nil && st.IsDir():
		return "", false
	case err != nil && !os.IsNotExist(err):
		return "", false
	}
	f, err := os.Open(abs)
	if err != nil {
		// Absent is a real state: a file that does not exist yet, or one just deleted. The empty
		// string IS its content, and comparing it is how a delete reads as a change.
		if os.IsNotExist(err) {
			return "", true
		}
		return "", false
	}
	defer f.Close()
	b := make([]byte, changeReadCap)
	n, err := io.ReadFull(f, b)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", false
	}
	return string(b[:n]), true
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
	var key string
	switch name {
	case "read":
		key = "limit"
	case "bash":
		// A trailing `&& echo "…"` changes no state — it only labels the result — so two commands
		// that differ ONLY there do identical work. Keeping the label in the fingerprint made each
		// one FIRST-SEEN, and a first-seen exercising command is credited as forward motion
		// (noteBashExec → progressSinceNudge), so re-running the same verification under a new
		// label read as progress and held the stall watchdog off indefinitely. Observed: 23 runs of
		// one self-comparing verification, differing only in the echo text and a -q/-s flag, until
		// the wall clock ended the turn. Strip the label for the fingerprint only; the command still
		// runs exactly as written.
		var ba struct {
			Command string `json:"command"`
		}
		if json.Unmarshal(raw, &ba) != nil {
			return canonicalArgs(raw)
		}
		stripped := stripEchoTail(ba.Command)
		if stripped == ba.Command {
			return canonicalArgs(raw)
		}
		b, err := json.Marshal(map[string]any{"command": stripped})
		if err != nil {
			return canonicalArgs(raw)
		}
		return string(b)
	default:
		return canonicalArgs(raw)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return canonicalArgs(raw) // not an object (or malformed) — fall back
	}
	delete(m, key)
	b, err := json.Marshal(m) // sorted keys
	if err != nil {
		return canonicalArgs(raw)
	}
	return string(b)
}

// echoTailRe matches ONE trailing `&& echo <literal>` (or `;`/`||`-joined) at the very end of a
// command. Only a literal argument is stripped: `echo $(…)`/`echo "$x"` can carry a substitution
// whose evaluation differs run to run, so those stay in the fingerprint.
var echoTailRe = regexp.MustCompile(`\s*(&&|\|\||;)\s*echo\s+(?:-[neE]+\s+)?(?:"[^"$` + "`" + `\\]*"|'[^']*'|[A-Za-z0-9_.,:/=+-]*)\s*$`)

// stripEchoTail removes trailing echo labels from a command for fingerprinting. Repeated so a
// `… && echo A && echo B` chain collapses too. Returns the input unchanged when there is no tail.
func stripEchoTail(cmd string) string {
	out := strings.TrimSpace(cmd)
	for {
		next := strings.TrimSpace(echoTailRe.ReplaceAllString(out, ""))
		if next == out || next == "" { // no tail left, or the command was ONLY an echo — keep it
			return out
		}
		out = next
	}
}

// guardChanges is the guard's change set, or nothing when there is no guard — the council tool can
// be called from a path that has none, and a nil guard must render an empty diff rather than panic.
func guardChanges(g *runGuard) []fileChange {
	if g == nil {
		return nil
	}
	return g.changeSet()
}
