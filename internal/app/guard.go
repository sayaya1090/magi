package app

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/change"
	"github.com/sayaya1090/magi/internal/core/selfcheck"
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

	// changed records this turn's file edits (before/after content) as the council's
	// evidence of what the AGENT actually changed — reconstructed from its own write/edit
	// tools, not git, so a human/external/bash change is never mis-attributed to the agent.
	changed     map[string]*fileChange
	changeOrder []string // first-seen order, for stable rendering

	// bashWrites holds this turn's bash commands that WRITE a file (a redirect, heredoc, or
	// tee). Files created via bash never populate `changed` (no path arg, not a fileModifier),
	// so a bash-authored deliverable would bypass the fabrication scan entirely. Recording the
	// write commands lets the scan see the content the model fabricated inline in the heredoc.
	bashWrites []string

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
		recalled: map[string]bool{}, contentHist: map[string][]uint64{},
		regressWarned: map[string]bool{},
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
func (g *runGuard) check(name string, args json.RawMessage) (block bool, n int, fp string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	fp = name + "\x00" + strconv.Itoa(g.epoch) + "\x00" + canonicalArgs(args)
	g.calls++
	g.sinceProgress++ // reset only by a real mutation (mutated); rises across varied calls
	g.seen[fp]++
	n = g.seen[fp]
	if n > repeatLimit {
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
	g.sinceProgress = 0 // a real change is progress → restart the no-progress count
	g.lastStallAt = 0   // …and the stall-nudge window, so a fresh stall re-arms cleanly
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

// bashWriteCap bounds how many bash write-commands are retained for the fabrication scan.
// A turn that writes many files via bash still only needs a handful sampled to catch a
// confession; the cap keeps memory bounded on a pathological command-spamming run.
const bashWriteCap = 64

// noteBashWrite records a bash command that WRITES to a file — one containing a redirect
// (`>`), a heredoc (`<<`), or `tee`. Read-only commands (a bare `grep`/`cat`) are skipped
// so a benign search for a marker phrase can't trip the fabrication scan; only commands
// that emit content into a file, the actual fabrication vector, are kept.
func (g *runGuard) noteBashWrite(cmd string) bool {
	if !strings.Contains(cmd, ">") && !strings.Contains(cmd, "<<") && !strings.Contains(cmd, "tee ") {
		return false
	}
	g.mu.Lock()
	if len(g.bashWrites) < bashWriteCap {
		g.bashWrites = append(g.bashWrites, cmd)
	}
	g.mu.Unlock()
	// A bash command that writes a file IS real progress — the tool-agnostic twin of
	// write/edit's epoch bump. Without this, bash-heavy tasks (most Terminal-Bench
	// work) climb sinceProgress while genuinely producing files, misfiring stall
	// nudges and, post-nudge-exhaustion, the stall force-stop. mutated() keys on the
	// command text itself, so re-running the IDENTICAL write is still not progress.
	g.mutated("\x00bash", cmd)
	return true
}

// scanFabrication runs the self-admitted-fabrication scan over BOTH the files the agent
// wrote this turn (via write/edit) and its file-writing bash commands (via noteBashWrite),
// so a deliverable is caught whether it was authored with an edit tool or a bash heredoc.
// It returns a label for the offending source and the matched line, or ("","") when clean.
//
// This is a best-effort, English-only PRE-FLAG, not the authority on completion (see the
// SCOPE note on selfcheck.FabricationMarkers): it feeds the council as evidence and can
// trigger an early nudge, but a confession in another language or a confident false claim
// slips past it. The language-agnostic completion authority is the review gate's tester,
// which actually runs the verification and returns PASS/FAIL, gating finish via the
// fresh-evidence check in the loop.
func (g *runGuard) scanFabrication() (label, snippet string) {
	if p, snip := scanFabricationClaim(g.changeSet()); p != "" {
		return p, snip
	}
	g.mu.Lock()
	writes := append([]string(nil), g.bashWrites...)
	g.mu.Unlock()
	for _, cmd := range writes {
		if _, line := selfcheck.FabricationMarker(cmd); line != "" {
			return "a bash command", line
		}
	}
	return "", ""
}

// scanFabricationClaim inspects the AFTER content of files the agent WROTE this turn for a
// self-admitted-fabrication marker (see selfcheck.FabricationMarkers). It returns the file
// path and the matched line (trimmed, bounded) of the first hit, or ("","") when clean.
// Only the agent's own writes are scanned — files it merely read are irrelevant, and a
// marker there would not be its confession. The marker set is shared with the report tool
// via internal/core/selfcheck so the two enforcement points never drift apart.
func scanFabricationClaim(changes []fileChange) (path, snippet string) {
	for _, c := range changes {
		// Test doubles legitimately say "this simulates…" — a mock is not a confession.
		if selfcheck.TestArtifactPath(c.path) {
			continue
		}
		if _, line := selfcheck.FabricationMarker(c.after); line != "" {
			return c.path, line
		}
	}
	return "", ""
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
	if g.stallNudges < maxStallNudges && g.sinceProgress-g.lastStallAt >= noProgressNudge {
		g.stallNudges++
		g.lastStallAt = g.sinceProgress
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

// buildCouncilChanges renders the turn's file changes as the council's evidence: a per-file
// "### path" header followed by the before→after line diff. Files whose diff is empty are
// skipped. The caller caps the total.
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
