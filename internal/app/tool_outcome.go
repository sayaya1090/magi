package app

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
)

// bashChange is a file a bash command was about to write, and its content just before it ran.
// bashChange is a destination a bash command was about to write, with its content beforehand.
// readable says whether magi could actually read that content: a directory, or a path it could not
// open, has no content to compare, and a comparison it cannot make is one it must not report.
type bashChange struct {
	path, before string
	readable     bool
}

// toolOutcome is one finished tool call, as the loop guard needs to see it: what was called, what
// came back, and the before-snapshots taken while the call was still ahead of us.
type toolOutcome struct {
	tc           *session.ToolCall
	res          *session.ToolResult // notes are appended to it, so the caller sees them
	workdir      string
	fp           string // this call's guard fingerprint ("" = the guard is not tracking it)
	novel        bool   // first occurrence of this fingerprint in the current epoch
	toolOK       bool   // the TOOL's own success, before post-edit hooks/diagnostics flipped IsError
	changePath   string
	changeBefore string
	bashChanges  []bashChange
}

// noteToolOutcome records everything one finished tool call means to the loop guard.
//
// This is the run's whole picture of "what just happened and what does it prove", and it had grown
// inside executeTool as a run of conditionals — nine of them, each added when some supervisor
// learned to be wrong about a different call shape, and all of them reading the same handful of
// locals from four hundred lines up. Read together they are one thing: the accounting a call owes.
// Order matters and is preserved exactly — the epoch bump has to precede the revert check that
// retracts it, and the failing-command path has to run for results the success path skips.
func (a *App) noteToolOutcome(sid session.SessionID, guard *runGuard, o toolOutcome) {
	tc, res, workdir := o.tc, o.res, o.workdir
	guardFP, guardNovel, toolOK := o.fp, o.novel, o.toolOK
	changePath, changeBefore, bashChanges := o.changePath, o.changeBefore, o.bashChanges

	// Loop guard bookkeeping. It used to say results were cached here "so a later blocked repeat
	// can be handed it" — nothing has cached a result since blocking came out, and no repeat is
	// ever handed anything. What is left: on a successful file mutation, bump the epoch so
	// identical follow-up commands (e.g. re-running the test) are no longer read as a repeat.
	mutatedReset := false // did mutated() reset the progress counters THIS call?
	if guard != nil && guardFP != "" {
		if !res.IsError && fileModifiers[tc.Name] {
			mutatedReset = guard.mutated(pathArg(tc.Args), canonicalArgs(tc.Args))
			if mutatedReset {
				// The file's contents are new, so what magi has already shown of it no longer
				// describes it: reading it again is gathering information, not circling.
				guard.dropReadCoverage(relForChange(workdir, pathArg(tc.Args)))
				a.bumpProductive(sid) // a real new version — the lease's evidence this child is producing
			}
		}
		// A successful bash write bumps the epoch (the tool-agnostic twin of an edit); a
		// successful non-write, non-inspect command (python/pytest/./run …) is execution
		// evidence for the current deliverable version. Together they drive the structural
		// unverifiedDeliverable signal that replaced the fabrication phrase scan.
		if !res.IsError && tc.Name == "bash" {
			var ba struct {
				Command    string           `json:"command"`
				Background builtin.FlexBool `json:"background"`
			}
			if json.Unmarshal(tc.Args, &ba) == nil {
				_, bashReset := guard.noteBashWrite(ba.Command) // authored a file → epoch bump
				guard.noteBashExec(ba.Command, guardNovel)      // ran a program → execution evidence (independent of any redirect)
				// Same productivity signal as a file mutation: a command that AUTHORS something, or
				// an exercising command run for the first time this epoch, is work a later step can
				// build on. A repeat of a command already run this epoch is not.
				if bashReset || (guardNovel && !isInspectOnly(ba.Command)) {
					a.bumpProductive(sid)
				}
				// The write/edit path's self-revert check, now on the bash path too: a mutation whose
				// net effect returns a file to a state it already held this turn is churn, not a new
				// deliverable version. mutated() cannot see it — every bash mutation shares one slot
				// and is compared by COMMAND TEXT, so `sed -i …` and the `cp f.bak f` that undoes it
				// always look like two different changes. Observed cost of the gap: each swing zeroed
				// stepsSinceMut and sinceProgress and re-armed the act-now nudge, so after the nudge
				// fired once neither it nor the "idle" force-stop could reach its threshold again and
				// the run oscillated until the wall clock. Retract once per call — one bump was made.
				// NOT for a backgrounded command. The comparison reads the destination back the
				// moment executeTool returns, and for a background job that is the moment it was
				// LAUNCHED — the command has not run yet, so the read returns what was there
				// before and the two sides match by construction. Observed live:
				// `rm /app/run_test_interrupt.py && python3 run.py &` came back carrying "this
				// write left the file byte-for-byte as it already was — nothing changed" about a
				// command whose first act was to delete that file. It did delete it, a moment
				// later. A false "nothing changed" is worse than silence: the agent can act on it.
				// Background was already parsed here and read by nobody; this is what it is for.
				if bashReset && !bool(ba.Background) {
					regressed := false
					// compared counts the destinations magi could actually read on both sides;
					// unchanged, how many of those held the same bytes afterwards. See the
					// retraction below.
					compared, unchanged := 0, 0
					for _, bc := range bashChanges {
						rel := relForChange(workdir, bc.path)
						after, ok := readForChange(workdir, bc.path)
						if !bc.readable || !ok {
							continue // no content to compare — say nothing rather than something false
						}
						// Nothing on either side is not a rewrite of anything. Observed live:
						// `rm -rf _build && make world … || true` — _build never existed, both reads
						// came back empty, and the result carried "this write left the file
						// byte-for-byte as it already was" about a command that neither wrote nor
						// deleted a thing. The check exists to catch re-authoring identical content;
						// a path with no content on either side has no history to re-author.
						if bc.before == "" && after == "" {
							continue
						}
						compared++
						if after == bc.before {
							unchanged++
						}
						if after != bc.before {
							// The bash mutation path shares one synthetic slot in mutated(), so the
							// read-coverage drop there never names a real file. Here the destination
							// IS known and the content demonstrably differs: what magi handed over
							// of this path no longer describes it.
							guard.dropReadCoverage(rel)
						}
						warn, reverted := guard.noteEdit(rel, bc.before, after)
						if warn != "" {
							// Named here and not on the write/edit path: those results describe the
							// one path their own call carried, and the reader already has it. A
							// bash command has as many destinations as it has, and the sentence
							// noteEdit returns names none of them. Observed live:
							// `cp a.bak a && cp b.bak b && cp c.bak c && ./program` came back with
							// the same path-less sentence three times over, which reads as one
							// finding rendered thrice rather than three files each left alone.
							// A path that is GONE did not have its content restored — it was
							// deleted, and magi files absent and empty under the same hash. The
							// revert wording then reads as "you undid your own edit" about
							// ordinary cleanup. Observed live: `sed … > /tmp/sweep_function.txt`
							// four minutes earlier, then `rm -f /tmp/sweep_function.txt` — the
							// scratch file's removal came back as a restored content state.
							if bc.before != "" && !pathExists(workdir, bc.path) {
								warn = "this command deleted the file — the path is back to the " +
									"state it was in before this turn."
							}
							res.Content = appendToContent(res.Content, "\n\n[self-edit check] "+rel+": "+warn)
						}
						regressed = regressed || reverted
					}
					// A mutation whose every readable destination came back holding the bytes it
					// already held moved nothing, and the stall window must keep climbing across
					// it. mutated() has this rule — an idempotent rewrite returns reset=false —
					// but the signature it compares on the bash path is the COMMAND TEXT, and a
					// command that differs by one character is a different signature no matter
					// what it wrote. Observed live (extract-elf, 2026-07-29): eleven runs of
					// `node extract.js > out.json && python3 -c "…"` over ten minutes, each with
					// a slightly different one-liner, each regenerating out.json byte for byte,
					// each resetting the no-progress counter — while the same call's result
					// carried "this write left the file byte-for-byte as it already was" eleven
					// times over. The content comparison is the better evidence and magi had
					// already done it; this is only routing it to the counter.
					//
					// All of them, not any: one destination that really changed is real work, and
					// cancelling it because a sibling path was rewritten identically would be the
					// false stall this guard exists to avoid. compared>0 keeps a command magi
					// could not read either side of out of it entirely — unknown is not unchanged.
					if regressed || (compared > 0 && unchanged == compared) {
						guard.retractProgress()
					}
				}
			}
		}
		// A successful NON-bash read-only inspection (read/grep/glob/list/…) that is
		// first-seen this epoch is forward motion: gathering new information — e.g.
		// reading several DIFFERENT files — must not climb toward the stall nudge.
		// File modifiers go through mutated(); bash is handled above; a spawn/report is
		// its own path. Everything else here is inspection.
		if !res.IsError && tc.Name != "bash" && !fileModifiers[tc.Name] && tc.Name != "task" {
			novel := guardNovel
			// A read's fingerprint carries its offset, so shifting the window two lines makes a
			// re-read first-seen. Ask what the read actually DELIVERED instead: coverage magi has
			// already handed over is not new information, whatever offset asked for it.
			if tc.Name == "read" {
				// The tool's own arg types: the model sends "540.0" as often as 540, and a strict
				// re-parse would fail the whole struct and see an unset offset where the read saw
				// line 540 — reporting fresh coverage for a window already delivered.
				var ra struct {
					Path   string          `json:"path"`
					Offset builtin.FlexInt `json:"offset"`
					Limit  builtin.FlexInt `json:"limit"`
				}
				if json.Unmarshal(tc.Args, &ra) == nil && ra.Path != "" {
					// The window the call ASKED for is not always the one it got: an oversized
					// result is truncated (capToolResult, already applied by here) and the read
					// tool has its own caps, so a `read{path}` of a large file can request 2000
					// lines and be handed 1239. Recording the request marks the 761 lines magi
					// never showed as shown, and the next read of that region — genuinely new
					// content — is refused credit as a re-read. The gutter numbers in the result
					// are what magi actually delivered, so it counts those and falls back to the
					// request only when there are none to count.
					off, lim := int(ra.Offset), int(ra.Limit)
					if lo, n, ok := deliveredLineWindow(res.Content); ok {
						off, lim = lo, n
					}
					// Keyed the same way the bash mutation path drops coverage, so `read
					// {/app/x/y.c}` and a `sed -i` naming `x/y.c` reach the same entry.
					novel = guard.noteReadCoverage(relForChange(workdir, ra.Path), off, lim)
				}
			}
			guard.noteInspectProgress(novel)
		}
	}
	// Record the agent's before→after change for the council. Gate on the tool's own success
	// (toolOK), NOT res.IsError — a write that landed but failed gofmt/a hook is exactly the
	// broken change the council should scrutinize, and must not read as a no-op turn.
	if guard != nil && changePath != "" && toolOK && fileModifiers[tc.Name] {
		rel := relForChange(workdir, changePath)
		after, _ := readForChange(workdir, changePath)
		guard.recordChange(rel, changeBefore, after)
		// Self-regression check: warn (don't block) when this edit undoes the agent's own
		// earlier change by returning the file to a state it already held this turn. A revert is
		// not progress, so retract the counter reset mutated() just applied — otherwise an
		// implement↔revert oscillation dodges the stall force-stop by zeroing sinceProgress on
		// every swing. Retract only when THIS call's mutated() actually reset (block above gates
		// on res.IsError, this one on toolOK — they can diverge).
		if warn, regressed := guard.noteEdit(rel, changeBefore, after); warn != "" || regressed {
			if warn != "" {
				res.Content = appendToContent(res.Content, "\n\n[self-edit check] "+warn)
			}
			if regressed && mutatedReset {
				guard.retractProgress()
			}
		}
	}
}

// deliveredLineWindow reports the first line number a read result carries and how many numbered
// lines it carries, by counting the gutter the read tool writes ("N\tcontent", one per line).
//
// It exists because the coverage record is a claim about what the agent was SHOWN, and the call's
// own offset/limit is a claim about what it asked for. Those agree until something truncates:
// capToolResult cuts an oversized result to fit the 64 KiB cap, and the read tool caps what it
// takes off disk. Observed live (fix-ocaml-gc, 2026-07-30): `read{path: runtime/major_gc.c}` with
// no window asked for the default 2000 lines and was handed 49152 bytes of them, so every line
// past the cut was recorded as delivered without ever being sent.
//
// ok is false when there is no gutter to count — an empty file, or a result that is not a read's
// numbered body — and the caller keeps the requested window rather than inventing a narrower one.
func deliveredLineWindow(content json.RawMessage) (offset, count int, ok bool) {
	var s string
	if json.Unmarshal(content, &s) != nil {
		return 0, 0, false
	}
	first, n := 0, 0
	for _, line := range strings.Split(s, "\n") {
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 {
			continue
		}
		num, err := strconv.Atoi(line[:tab])
		if err != nil || num <= 0 {
			continue
		}
		if n == 0 {
			first = num
		}
		n++
	}
	if n == 0 {
		return 0, 0, false
	}
	return first, n, true
}
