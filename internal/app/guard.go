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
	// execSinceMut counts bash commands that actually EXERCISE the deliverable (run a program
	// or test — anything whose leading verb is not an inspect-only builtin; see isInspectOnly)
	// SINCE the last real file mutation. Like a tester PASS, execution evidence is version-
	// stamped: a mutation resets it to 0, so "did you run anything against the CURRENT
	// deliverable" is answerable structurally. epoch>0 with execSinceMut==0 is the language-
	// agnostic "produced/changed a deliverable then declared done without running it" signal
	// (see unverifiedDeliverable) that replaced the English-only fabrication phrase scan.
	execSinceMut int
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
	g.execSinceMut = 0  // a new deliverable version is unverified until something exercises IT
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

// noteBashWrite records a bash command that AUTHORS a file — a heredoc (`<<`), a `tee`, or
// a `>`/`>>` redirect to a real path — and bumps the mutation epoch for it. It excludes
// file-descriptor duplications (`2>&1`, `>&2`) and `/dev/*` sinks (`>/dev/null`), which
// capture or discard a command's output rather than produce a deliverable (see
// redirectsToFile); read-only commands (a bare `grep`/`cat`) also return false and do not
// bump. It returns whether the command authored a file.
func (g *runGuard) noteBashWrite(cmd string) bool {
	if !redirectsToFile(cmd) {
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
// is real execution evidence for the CURRENT deliverable version.
func (g *runGuard) noteBashExec(cmd string) {
	if isInspectOnly(cmd) {
		return
	}
	g.mu.Lock()
	g.execSinceMut++
	g.mu.Unlock()
}

// unverifiedDeliverable reports the structural, language-agnostic fabrication signal that
// replaced the English-only phrase scan: the agent produced or changed a deliverable this
// turn (epoch>0) yet has run NO command exercising the CURRENT version (execSinceMut==0).
// That is exactly "wrote/edited a result, then declared done without ever running it" —
// whether the artifact confesses in prose (any language) or not. It is ADVISORY: a task
// with genuinely nothing to run (write one config file) also matches, so callers feed it to
// the council and a one-shot nudge, never a hard block. The hard, language-agnostic
// completion authority remains the review-gate tester, which actually runs the verification
// (see parseTesterVerdict + the fresh-evidence gate in loop.go).
func (g *runGuard) unverifiedDeliverable() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.epoch > 0 && g.execSinceMut == 0
}

// inspectOnlyCmds are shell builtins/coreutils whose job is to PRINT or INSPECT state,
// never to run a program-under-test. A bash command built only from these cannot verify a
// deliverable — it can restate that a file exists or echo a success banner, the exact
// "looks-verified" churn a fabricating agent emits (measured on Terminal-Bench: an agent
// ran `ls`/`echo`/`exit 0`/`true` dozens of times without once executing its own module).
// The set is deliberately small and CLOSED — POSIX inspection verbs — the opposite of an
// open-ended confession-phrase list. Anything not here (python, pytest, go, ./run, make, a
// script) counts as execution, so the bias is toward NOT flagging; this is only an advisory.
var inspectOnlyCmds = map[string]bool{
	"true": true, "false": true, ":": true, "exit": true, "echo": true, "printf": true,
	"ls": true, "pwd": true, "cd": true, "cat": true, "head": true, "tail": true,
	"wc": true, "stat": true, "file": true, "which": true, "type": true, "test": true,
	"[": true, "[[": true, "sleep": true, "clear": true, "dirname": true, "basename": true,
	"realpath": true, "readlink": true, "tee": true, // tee authors content, it does not run a program
}

// isInspectOnly reports whether EVERY segment of cmd (split on the shell operators &&, ||,
// ;, |, &, and newlines) is inspect-only — i.e. the whole command runs nothing that could
// exercise a deliverable. A segment is execution if its first token is PATH-QUALIFIED
// (contains `/`, e.g. `./test`, `/usr/bin/foo`, `bin/run`) — a path always runs a program,
// never a shell inspection builtin, so this is checked before the builtin-name lookup and
// keeps a binary that happens to be named `test`/`sleep` from reading as the builtin.
// Otherwise the bare name is looked up in inspectOnlyCmds. This is a heuristic tokenizer that
// does not honor quoting, which is fine: it only feeds the advisory unverifiedDeliverable
// signal. An empty command counts as inspect-only (it ran nothing).
func isInspectOnly(cmd string) bool {
	segs := splitShellSegments(stripHeredocs(cmd))
	if len(segs) == 0 {
		return true
	}
	for _, s := range segs {
		fields := strings.Fields(s)
		if len(fields) == 0 {
			continue // empty segment (e.g. a trailing operator) — nothing ran here
		}
		tok := fields[0]
		if isRedirectFragment(tok) {
			continue // a split fd-dup/redirect artifact ("1" from `2>&1`, ">f" from `&>f`), not a command
		}
		if strings.ContainsRune(tok, '/') {
			return false // a path-qualified command runs a program
		}
		if !inspectOnlyCmds[tok] {
			return false
		}
	}
	return true
}

// isRedirectFragment reports whether tok is a leftover piece of a redirect, not a command
// name — because splitShellSegments cuts on `&`, an fd-duplication like `2>&1` or `>&2` is
// torn into a segment whose first token is the redirect tail (`1`, `2`) or a redirect operator
// (`>file` from `&>file`). Such a segment ran no program, so it must not read as execution.
// A real command never begins with a digit or a redirect operator.
func isRedirectFragment(tok string) bool {
	if tok == "" {
		return true
	}
	switch tok[0] {
	case '>', '<', '&':
		return true
	}
	for i := 0; i < len(tok); i++ {
		if tok[i] < '0' || tok[i] > '9' {
			return false
		}
	}
	return true // all digits (e.g. the `1` in `2>&1`)
}

// splitShellSegments breaks a command line into its pipeline/list segments on the shell
// control operators, so each can be classified independently ("ls && python x" is NOT
// inspect-only). Two-char operators are replaced before their one-char prefixes.
func splitShellSegments(cmd string) []string {
	repl := cmd
	for _, op := range []string{"&&", "||", ";", "|", "\n", "&"} {
		repl = strings.ReplaceAll(repl, op, "\x00")
	}
	var out []string
	for _, p := range strings.Split(repl, "\x00") {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// leadingVerb returns the basename of a segment's first token, or "" if the segment is
// blank. Env-assignment or wrapper prefixes (FOO=bar, sudo, env) are left as-is, which
// classifies them as execution — the FP-safe direction, since such prefixes front real
// commands far more often than inspect-only ones.
func leadingVerb(seg string) string {
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return ""
	}
	v := fields[0]
	if i := strings.LastIndexByte(v, '/'); i >= 0 {
		v = v[i+1:]
	}
	return v
}

// redirectsToFile reports whether cmd sends output into a real file — a heredoc (`<<`), a
// pipe into `tee`, or a `>`/`>>` whose target is an actual path. It deliberately EXCLUDES
// file-descriptor duplications (`2>&1`, `>&2`, `>&-`) and `/dev/*` sinks (`>/dev/null`),
// which capture or discard a running command's output rather than author a deliverable — so
// that running tests with `pytest 2>&1` or `./run >/dev/null` is not mistaken for producing
// a new deliverable version. Heuristic; does not honor quoting, which is fine for the
// advisory epoch bump it feeds.
func redirectsToFile(cmd string) bool {
	if hasHeredoc(cmd) { // heredoc authors content
		return true
	}
	for _, seg := range splitShellSegments(cmd) {
		if leadingVerb(seg) == "tee" {
			return true
		}
	}
	for i := 0; i < len(cmd); i++ {
		if cmd[i] != '>' {
			continue
		}
		j := i
		for j < len(cmd) && cmd[j] == '>' { // consume a `>>` run as one redirect
			j++
		}
		i = j - 1 // resume scanning after this redirect operator
		k := j
		for k < len(cmd) && (cmd[k] == ' ' || cmd[k] == '\t') {
			k++
		}
		if k < len(cmd) && cmd[k] == '&' {
			continue // fd duplication (`>&1`, `>&-`) — not a file
		}
		end := k
		for end < len(cmd) && !isRedirectStop(cmd[end]) {
			end++
		}
		target := cmd[k:end]
		if target == "" || strings.HasPrefix(target, "/dev/") {
			continue // bare `>` with no visible target, or a /dev sink
		}
		return true
	}
	return false
}

// stripHeredocs removes heredoc bodies (and their terminator lines) so that content fed via
// `cmd <<TAG … TAG` is not mistaken for commands when we split on newlines to classify a bash
// call. It keeps the introducing line (which carries the real leading verb, e.g. `cat > f`).
func stripHeredocs(cmd string) string {
	if !strings.Contains(cmd, "<<") {
		return cmd
	}
	lines := strings.Split(cmd, "\n")
	out := lines[:0:0]
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		out = append(out, line)
		idx := strings.Index(line, "<<")
		if idx < 0 {
			continue
		}
		delim := heredocDelim(line[idx+2:])
		if delim == "" {
			continue // a `<<` that is not a heredoc intro (e.g. arithmetic left-shift)
		}
		for i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != delim {
			i++ // drop body line
		}
		if i+1 < len(lines) {
			i++ // drop the terminator line
		}
	}
	return strings.Join(out, "\n")
}

// hasHeredoc reports whether cmd contains a real heredoc (`cmd <<TAG`), as opposed to an
// arithmetic left-shift (`$((1<<2))`) — distinguished by whether the token after `<<` is a
// valid delimiter word (see heredocDelim).
func hasHeredoc(cmd string) bool {
	for i := 0; i+1 < len(cmd); i++ {
		if cmd[i] == '<' && cmd[i+1] == '<' && heredocDelim(cmd[i+2:]) != "" {
			return true
		}
	}
	return false
}

// heredocDelim returns the delimiter word a `<<` introduces (given the text right after the
// `<<`), or "" if this is not a heredoc. Handles `<<-` and quoted `<<'EOF'`/`<<"EOF"`. A
// heredoc delimiter is an identifier — it must begin with a letter or underscore — so an
// arithmetic shift like `1<<2` (whose "delimiter" would start with a digit) returns "".
func heredocDelim(afterLtLt string) string {
	s := strings.TrimLeft(afterLtLt, "-<") // <<- and any run of extra <
	s = strings.TrimLeft(s, " \t")         // optional space before the word
	f := strings.Fields(s)
	if len(f) == 0 {
		return ""
	}
	word := strings.Trim(f[0], "'\"")
	if word == "" {
		return ""
	}
	c := word[0]
	if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return word
	}
	return ""
}

// isRedirectStop reports whether b ends a redirect target token.
func isRedirectStop(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '|', ';', '&', '<', '>':
		return true
	}
	return false
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
