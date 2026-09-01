package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math"
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
	// force-stops — a read-only turn may legitimately never mutate a file, and ending a turn is
	// an intervention this project does not make. So speech is the whole lever, and the stalled
	// nudge RE-ARMS: it fires again after each further noProgressNudge window with still no
	// mutation, for as long as that stays true.
	//
	// It used to stop after three. That cap was written when a force-stop was still expected to
	// land the run, so going quiet handed off to something; the force-stop is gone and the cap
	// handed off to nothing. Measured live (large-scale-text-editing, 2026-08-01): the budget was
	// spent by the 64th call, and the agent then wrote byte-identical content 57 more times in
	// silence. A turn that is genuinely gathering information does not reach this at all — a
	// first-seen inspection counts as forward motion and restarts the window — so what keeps
	// hearing it is a turn that keeps circling, which is who it is for.
	noProgressNudge = 12
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
	mu sync.Mutex
	// touches answers what a call does to a file: the path from its own arguments, and whether it
	// writes. A function rather than a name list because a tool can now say so itself — an editor
	// plugin's edit tool is called mcp__jetbrains__edit and by name is nothing.
	touches func(name string, args json.RawMessage) (fileTouch, bool)
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
	// created holds absolute paths that did not exist when a call in this run named them and do
	// exist after it -- the run's own output. The irreversible-command gate reads it: a delete of
	// something the run itself made is undoing its own work, not destroying somebody's, however
	// far outside the workspace it sits. Observed cost of not having it (sanitize-git-repo,
	// 2026-08-26): the agent copied the repo to a sibling backup, sanitized the original, and was
	// then refused the `rm -rf` of its own backup -- which still held every secret the task asked
	// it to remove.
	created map[string]bool
	// can ALSO restore the STEP-based idle window on a self-revert. Without this, an oscillating
	// edit loop (rewrite→prior-state→rewrite) refills the step-based "idle" trigger on every swing
	// even though retractProgress restored the tool-call counter — so a reasoning-heavy churn (few
	// tool calls, mostly analysis + oscillating rewrites) dodges BOTH stall paths and burns to the
	// external wall clock (custom-memory-heap-crash: 125+ steps rewriting the same stub to timeout).
	// spend the one-shot budget only on a REAL deliverable version. Without it a revert loop past
	// the nudge threshold re-fires "act now" on every swing (the restored counter is already in the
	// nudge window, and the re-arm cleared the latch) — and a repeated nudge is exactly what pushes
	// a weak model to keep thrashing (see noteEdit on the once-per-file warning). The counter named
	// here used to be stepsSinceMut, a step-based idle window; it is gone, and sinceProgress is the
	// one that remains.
	calls int // total tool calls this run (idle-resubmission detection)
	// nudgedBlocked records that the "blocked"-kind re-grounding fired. It fires ONCE per run and
	// nothing follows it: the force-stop that used to (stuck()) was removed on measurement. See
	// shouldNudge for what that leaves.
	nudgedBlocked bool
	lastStallAt   int // sinceProgress value at the last stalled nudge, for spacing the re-arm
	stallNudges   int // how many stalled nudges this run has fired — the repeats shrink (see loop_gates)
	// progressSinceNudge is the structural "the agent made forward motion since the last stalled
	// nudge" signal: set true by EITHER a real mutation (mutated) OR a NOVEL (first-seen this
	// epoch) non-inspect exercising command (noteBashExec) — both are genuine progress. It is set
	// false when a stalled nudge fires and on a structural recovery (resetStall). Read by the
	// nudge text, so a re-arm can say whether anything moved since the last one.
	progressSinceNudge bool

	// changed records this turn's file edits (before/after content) as the council's
	// evidence of what the AGENT actually changed — reconstructed from its own write/edit
	// tools, not git, so a human/external/bash change is never mis-attributed to the agent.
	changed     map[string]*fileChange
	changeOrder []string // first-seen order, for stable rendering
	// readSpans is which lines of each path magi has already handed over, so re-opening a window
	// it already delivered is not mistaken for gathering new information (noteReadCoverage).
	readSpans map[string][]lineSpan
	// shownLines is the digest of each line magi has HANDED to the agent, per path — what the
	// agent believes line N holds. It exists to answer the one question an `at`/`to` edit anchor
	// cannot ask for itself: is this still the line I read?
	//
	// magi used to render that check into the read gutter as `N#hh|content`, so the model carried
	// the hash and sent it back in the anchor. That gutter is gone (see builtin/hashline.go): the
	// `#hh|` prefix read as a pipe-delimited data column and models parsed a phantom field out of
	// CSV/TSV/fixed-width files, which cost whole tasks. Removing it also removed the guard behind
	// it, and `checkAnchor` has been a bounds check ever since — it can tell you line 50 exists, and
	// nothing about whether it still holds what you read.
	//
	// Keeping the record HERE restores the guard with no model-visible surface: no gutter, no new
	// argument, no prompt cost. The agent is refused only when the anchored line has actually
	// drifted, and the refusal says to re-read.
	//
	// Not refreshed by the agent's own edits, deliberately. An edit returns a diff, not the file,
	// so it shows the agent nothing — and an edit that changes the line COUNT shifts every anchor
	// below it, which is the exact failure this catches.
	shownLines map[string]map[int]uint64

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
	// rewriteNoted records, per file, the version count at which the circling note last spoke, so
	// the cadence is "how far since the last time" rather than a modulo on a number that does not
	// advance by one. contentHist can gain TWO entries in a call — the fold of an externally
	// changed `before`, then the new content — so a count tested with `% every == 0` can step over
	// its window and never land in it again. magi's own gofmt hook rewrites a .go file's bytes
	// after every successful write, which makes that fold fire on EVERY call: measured, 14 calls
	// across 27 states produced no note at all. A guard silenced exactly in the case it exists for
	// is worse than no guard, because the silence reads as an all-clear.
	rewriteNoted map[string]int
}

// fileChange is one file's before/after content captured around an agent edit this turn.
type fileChange struct {
	path   string
	before string // content just before the FIRST edit this turn ("" if newly created)
	after  string // content just after the LATEST edit
}

// newRunGuard builds the guard for one run. touches answers what a call does to a file — handed in
// because the guard cannot reach the tool registry, and because the answer is now a tool's own
// (port.FileTool) rather than a list of three names. nil keeps the builtin names.
func newRunGuard(touches func(name string, args json.RawMessage) (fileTouch, bool)) *runGuard {
	if touches == nil {
		touches = func(name string, args json.RawMessage) (fileTouch, bool) {
			return touchesFileIn(nil, name, args)
		}
	}
	return &runGuard{
		touches: touches,
		seen:    map[string]int{},
		lastMut: map[string]string{}, changed: map[string]*fileChange{},
		readSpans: map[string][]lineSpan{},
		recalled:  map[string]bool{}, contentHist: map[string][]uint64{}, rewriteNoted: map[string]int{},
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
// noteCreated records an absolute path this run brought into being.
func (g *runGuard) noteCreated(abs string) {
	if g == nil || strings.TrimSpace(abs) == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.created == nil {
		g.created = map[string]bool{}
	}
	g.created[filepath.Clean(abs)] = true
}

// didCreate reports whether this run made the path (or something it is inside).
//
// A directory counts for what it contains: a run that made `/tmp/work` made `/tmp/work/out.txt`
// too, and a delete of either is the same act.
func (g *runGuard) didCreate(abs string) bool {
	if g == nil || strings.TrimSpace(abs) == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	abs = filepath.Clean(abs)
	for made := range g.created {
		if made == abs {
			return true
		}
		if rel, err := filepath.Rel(made, abs); err == nil && rel != ".." &&
			!strings.HasPrefix(rel, "../") && rel != "." {
			return true
		}
	}
	return false
}

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
	// The history is what magi RECORDED; the file is what it IS, and the two part company every
	// time something rewrites the path through a route magi cannot name a destination for.
	// `before` is the only one of the pair that was just read from disk, so fold it in and ask
	// every later question against what the file actually held.
	//
	// Observed live (cobol-modernization, 2026-07-30): `cp /tmp/*_orig.DAT /app/data/*.DAT`
	// restored three fixtures that `python3 program.py` had rewritten in between — a command with
	// no extractable destination, so the record still read ORIG while the files held the
	// post-transaction bytes. The restore moved all three, and every one came back saying "this
	// write left the file byte-for-byte as it already was — nothing changed". The next command,
	// running the COBOL binary over those same files, printed the post-restore result and proved
	// the copy had taken.
	if hb := hashContent(before); hb != hist[len(hist)-1] {
		hist = append(hist, hb)
	}
	h := hashContent(after)
	// Measured, not inferred: "nothing changed" is a claim about THIS command's effect, and the
	// two reads around it are what settles that. Matching some recorded state is a different
	// question, and answering it with this sentence is how the sentence came to be false.
	if before == after {
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
	// already held this turn (original→fix→original, or fix1→fix2→fix1 oscillation). The fold
	// above put `before` at the end, and the command moved off it, so the excluded entry is
	// exactly the state this command left.
	for i := 0; i < len(hist)-1; i++ {
		if hist[i] == h {
			regressed = true
			break
		}
	}
	g.contentHist[path] = append(hist, h)
	if !regressed {
		// Forward progress — every one of these is content the file has not held before, and that
		// is the point: no revert check can see this shape. But a file that takes a NEW content
		// forty times in one turn is not being written, it is being circled, and nothing here was
		// counting that. Observed live (qwen3-coder-next, a 5-7-5 haiku): 42 writes to one path
		// across twenty-five minutes, every one a different text, the model saying "I apologize for
		// the prolonged iteration" five times word for word, and the run only ended because a
		// person killed it. The council was right and said "continue" each round; what was missing
		// was anyone stating the shape of the last forty minutes.
		//
		// So it states it, and no more: the count is a fact, and whether to keep going is the
		// agent's call. regressed stays false — this is not a revert, and withholding progress
		// credit for real new content would be a lie in the other direction.
		// DISTINCT versions, not history entries. contentHist gains an entry per recorded state
		// including repeats, so a turn that reverted twice would be told it had written eleven
		// different contents when it had written ten — and the swing note beside this one already
		// counts the honest way, with distinctStates. Two notes about one history disagreeing
		// about its size is how a reader learns to trust neither. Minus the pre-turn baseline,
		// because that is what the file arrived as, not something this turn wrote.
		n := distinctStates(g.contentHist[path]) - 1
		// Since the last time this spoke, not a modulo — see rewriteNoted.
		if n >= rewriteNoteAt && n-g.rewriteNoted[path] >= rewriteNoteEvery {
			g.rewriteNoted[path] = n
			return fmt.Sprintf("note: this file has taken %d different contents this turn. If each "+
				"rewrite is meant to be the one that settles it, that is not what is happening — "+
				"the next thing to change is probably not this file.", n), false
		}
		return "", false
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

// rewriteNoteAt / rewriteNoteEvery bound when a file that keeps taking NEW content is remarked on.
//
// Eight because iterating on a file a few times is ordinary work — a draft, a fix, a fix to the
// fix — and a guard that speaks at three would be noise in every honest turn. Then every fourth,
// because the first note is an aside the agent may have a good answer to, and the fourteenth is a
// description of a turn that has stopped going anywhere.
const (
	rewriteNoteAt    = 8
	rewriteNoteEvery = 4
)

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

// forgetRecalledTopics clears the per-topic ledger after a compaction, leaving the count alone.
//
// "use what was returned earlier" is only true while what was returned is still there. A
// compaction summarizes the older messages away — including a recall's own output, which is
// ordinary conversation once it lands — and then re-indexes that region into recallable topics and
// tells the agent so ("recall_context can re-open the detail by topic"). So magi invited the call
// and refused it in the same turn, pointing at content it had just removed. Observed in the field
// (qemu-alpine-ssh, 2026-07-31): `recall_context{topic:"setup_alpine.py"}` returned 4 messages,
// and the same topic 48 events later came back "already recalled this topic this turn".
//
// recallCount is deliberately NOT reset: the budget exists to bound how much a single turn can
// re-inflate, and a compaction does not buy the turn a fresh allowance — it only makes a SECOND
// recall of an already-shed topic a legitimate request rather than a duplicate one.
func (g *runGuard) forgetRecalledTopics() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.recalled = map[string]bool{}
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
//
// Keyed by the touch's GUARD slot rather than its path: a write that names no file (port.FileTool's
// FileArg answers "" for a call that names none) would otherwise share one slot with every other
// such write, and each would read the other's signature as the world having moved.
func (g *runGuard) repeatEpoch(name string, args json.RawMessage) string {
	if touch, ok := g.touches(name, args); ok && touch.writes {
		return g.lastMut[touch.guard]
	}
	return strconv.Itoa(g.epoch)
}

// check COUNTS repeats and never blocks — the hard block this comment once described was
// removed (execute.go notes the removal; block is unconditionally false below). The counts
// still matter: they feed the nudges and the stall layer, which is what terminates a
// genuine repeat spin now.
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
// `key` differs from the last mutation recorded under it (sig = the call's canonical args).
// `key` is the caller's guard slot — a file path for a call that names one, the tool's own name
// for one that does not (see guardKey), and the synthetic "\x00bash" slot for a bash write.
// An idempotent rewrite (writing identical content) is not progress, so it must NOT reset
// the repeat counts — otherwise a write-the-same-thing loop would never be caught.
// mutated returns whether it actually reset the progress counters (true) or short-circuited as
// an idempotent rewrite (false). The caller uses that so retractProgress only ever undoes a reset
// this same call produced.
func (g *runGuard) mutated(key, sig string) (reset bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.lastMut[key] == sig {
		return false // no real change → leave the loop-guard counters intact
	}
	g.lastMut[key] = sig
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
// implement↔revert oscillation can no longer dodge the stall NUDGE by zeroing the counter on every
// swing. It said "force-stop" until this was measured: nothing force-stops a stalled run, and
// nothing will — ending a turn is an intervention this project does not make. What the climbing
// counter buys is another sentence, not a shutdown.
// Only call this when the mutated() for the same edit returned reset==true.
func (g *runGuard) retractProgress() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sinceProgress = g.prevSince
	g.lastStallAt = g.prevStallAt
	// A self-revert is not a fresh deliverable version, so the no-progress counter must keep
	// climbing across an oscillating rewrite loop instead of being zeroed on every swing. There
	// used to be a second, step-based window here (stepsSinceMut) feeding an "idle" recovery;
	// both are gone, so sinceProgress carries this alone and a reasoning-heavy churn now reaches
	// the stalled nudge rather than an idle force-stop.
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
	// The SIGNATURE folds the echo tail exactly like the fingerprint does (guardArgs): kept
	// raw, the same write under a fresh `&& echo …` label was a NEW signature every time —
	// epoch rose, sinceProgress reset, and the repeat/blocked counters were laundered by a
	// label that changes no state. The fingerprint learned this first; the signature lagged,
	// and the asymmetry was the leak.
	return true, g.mutated("\x00bash", stripEchoTail(cmd))
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

// Bounds on shownLines. A digest is 8 bytes plus map overhead, and a run can read a lot: the caps
// stop a long session from carrying a line record for every file it has ever opened. Past the cap
// the path stops being tracked, which loses the guard for it — never gains a false refusal.
const (
	maxShownLinesPerPath = 4000
	maxShownPaths        = 200
)

// noteReadLines records what magi just showed the agent, line by line, from the read result's own
// gutter. The gutter numbers are what was DELIVERED (the tool caps and the result truncator both
// cut), so parsing them is the only way to know which lines the agent actually holds.
func (g *runGuard) noteReadLines(path, delivered string) {
	if path == "" || delivered == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.shownLines == nil {
		g.shownLines = map[string]map[int]uint64{}
	}
	lines, tracked := g.shownLines[path], true
	if lines == nil {
		if len(g.shownLines) >= maxShownPaths {
			return
		}
		lines = map[int]uint64{}
		g.shownLines[path] = lines
	}
	for _, l := range strings.Split(delivered, "\n") {
		tab := strings.IndexByte(l, '\t')
		if tab <= 0 {
			continue
		}
		n, err := strconv.Atoi(l[:tab])
		if err != nil || n <= 0 {
			continue
		}
		if len(lines) >= maxShownLinesPerPath {
			tracked = false
			break
		}
		lines[n] = hashContent(l[tab+1:])
	}
	if !tracked {
		// Half a file's record is worse than none: the untracked half would answer "no record, allow"
		// while the tracked half refuses, so the guard would fire on position rather than on drift.
		delete(g.shownLines, path)
	}
}

// anchorDrifted reports whether the anchored line no longer holds what the agent was shown.
//
// It answers only when magi has a record for that exact line. No record — a file the agent never
// read, a line outside the window it read, a path past the cap — is not evidence of drift, and
// this returns false. The guard refuses on what it knows, never on what it does not.
func (g *runGuard) anchorDrifted(path string, line int, current string) bool {
	if path == "" || line <= 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	want, ok := g.shownLines[path][line]
	if !ok {
		return false
	}
	lines := strings.Split(current, "\n")
	if line > len(lines) {
		return false // past end — checkAnchor's own bounds check reports that, with better words
	}
	return hashContent(lines[line-1]) != want
}

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
// noProgressNudge). The two kinds are independent.
//
// The stalled nudge RE-ARMS: it fires again after each further noProgressNudge window with still
// no mutation, for as long as nothing changes. It used to stop after three; that cap went out with
// the force-stop it was written to defer to.
//
// The blocked nudge fires ONCE and nothing follows it. This comment used to say that stuck()
// force-stopped a run that kept blocking, and used that as the reason the blocked kind needed no
// re-arm — but stuck() was removed on measurement, so the reason went with it.
//
// No force-stop is coming back. Ending a turn is itself an intervention in the agent's flow, and
// this project would rather say something true and let the agent decide than take the turn away
// from it; the wall clock is the only hard stop. What magi has here is speech, and the budget on
// it is what keeps speech from becoming noise.
//
// So the honest description of the blocked kind is: one sentence, then silence, with the
// per-call facts (the self-edit check's "nothing changed", the exit codes, the pipeline notes)
// carrying on underneath. Measured live (large-scale-text-editing, 2026-08-01): the blocked nudge
// fired at the fourth repeat, and the agent then wrote byte-identical content 57 more times —
// every one of them answered "nothing changed" — and kept going anyway. magi said what was true
// each time. That is the whole of what it does here, by choice.
//
// Returns "" when no nudge is due.
func (g *runGuard) shouldNudge() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.blocked >= nudgeThreshold && !g.nudgedBlocked {
		g.nudgedBlocked = true
		return "blocked"
	}
	if g.sinceProgress-g.lastStallAt >= noProgressNudge {
		// There used to be a collapse here: a re-arm whose window produced no forward motion
		// silenced the rest of the budget and returned "", on the reasoning that stuck() would land
		// the honest stall this iteration rather than burning more no-progress windows — "same
		// terminal outcome, sooner".
		//
		// stuck() is gone; the force-stop it named came out on measurement. So the collapse had
		// no terminal outcome left to accelerate and simply silenced the remaining budget.
		// Observed live (large-scale-text-editing): one stall nudge, then 32 consecutive writes
		// of byte-identical content — each one answered "nothing changed" by the self-edit check
		// — and not another word from the loop. Saying it again is the only lever left, and it is
		// no longer rationed: the cap that used to bound it went out with the force-stop it was
		// written for.
		g.lastStallAt = g.sinceProgress
		g.progressSinceNudge = false // fresh window for judging the next re-arm
		g.stallNudges++
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
	// Reserve the marker's own length before cutting: appending it to a full-cap prefix put the
	// result 150 bytes OVER the cap this function exists to enforce. Measured on the three inputs
	// that reach here — non-JSON bytes, multibyte non-JSON, and a malformed JSON string (the one
	// of the three a broken producer can actually deliver) — all came back at 65686 for a 65536
	// cap. Shrinking cut can only shorten the marker (fewer digits), so one reservation is enough.
	cut := toolResultCap - len(marker(toolResultCap, len(b)))
	if cut < 0 {
		cut = 0
	}
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
// absUnder resolves a tool-supplied path against the workspace, the same way the read/stat
// helpers here do, so the created-set and the gate name the same file.
func absUnder(workdir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(workdir, path))
}

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
	// One byte past the cap, so a file that fills it can be told from one that merely reaches it.
	b := make([]byte, changeReadCap+1)
	n, err := io.ReadFull(f, b)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", false
	}
	if n > changeReadCap {
		// A prefix is not the file. Everything downstream of this read states a fact about the
		// WHOLE path — "this write left the file byte-for-byte as it already was — nothing
		// changed", or a content state it "already held" — and a comparison that stopped at
		// 256 KiB cannot support any of them: a command could rewrite byte 300000 and both sides
		// would still match. So this joins the directory case above — a path magi cannot read the
		// content of is one it cannot say anything true about, so it says nothing.
		//
		// Observed live (large-scale-text-editing, 2026-07-30): `cp /app/input.csv
		// /tmp/test_input.csv && vim … -S apply_macros.vim` on a 1,000,000-row CSV came back
		// saying the file was byte-for-byte unchanged, twice, off a 256 KiB look.
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
// Numbers decode as json.Number, not float64: two integers past 2^53 used to
// collapse to one canonical form and two DIFFERENT calls fingerprinted equal.
func canonicalArgs(raw json.RawMessage) string {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	// Decode stops at the first value where Unmarshal refused trailing bytes — keep refusing, or
	// two different payloads share one canonical form.
	if dec.More() {
		return string(raw)
	}
	v = canonNumbers(v)
	b, err := json.Marshal(v) // Go marshals map keys in sorted order
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// canonNumbers folds every integral json.Number to its integer spelling, so 540 and 540.0 — which
// a model sends interchangeably, as tool_outcome already records — stay ONE fingerprint, while
// integers past 2^53 keep the identity float64 was collapsing.
func canonNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = canonNumbers(e)
		}
		return t
	case []any:
		for i, e := range t {
			t[i] = canonNumbers(e)
		}
		return t
	case json.Number:
		if f, err := t.Float64(); err == nil && f == math.Trunc(f) && math.Abs(f) < 1<<53 {
			return json.Number(strconv.FormatInt(int64(f), 10))
		}
		return t
	default:
		return v
	}
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

// guardEpoch is the guard's mutation epoch, or zero when there is no guard — the rejection cap
// reads it to tell "rejected again after real work" from "rejected again after nothing".
func guardEpoch(g *runGuard) int {
	if g == nil {
		return 0
	}
	return g.mutationEpoch()
}

// guardChanges is the guard's change set, or nothing when there is no guard — the council tool can
// be called from a path that has none, and a nil guard must render an empty diff rather than panic.
func guardChanges(g *runGuard) []fileChange {
	if g == nil {
		return nil
	}
	return g.changeSet()
}
