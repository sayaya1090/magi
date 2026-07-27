package app

import (
	"encoding/json"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
)

// bashChange is a file a bash command was about to write, and its content just before it ran.
type bashChange struct{ path, before string }

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

	// Loop guard bookkeeping: cache this call's result (so a later blocked repeat can be
	// handed it) and, on a successful file mutation, bump the epoch so identical follow-up
	// commands (e.g. re-running the test) are no longer treated as a no-progress repeat.
	mutatedReset := false // did mutated() reset the progress counters THIS call?
	if guard != nil && guardFP != "" {
		guard.record(guardFP, string(res.Content))
		if !res.IsError && fileModifiers[tc.Name] {
			mutatedReset = guard.mutated(pathArg(tc.Args), canonicalArgs(tc.Args))
			if mutatedReset {
				a.bumpProductive(sid) // a real new version — the lease's evidence this child is producing
			}
		}
		// A successful bash write bumps the epoch (the tool-agnostic twin of an edit); a
		// successful non-write, non-inspect command (python/pytest/./run …) is execution
		// evidence for the current deliverable version. Together they drive the structural
		// unverifiedDeliverable signal that replaced the fabrication phrase scan.
		if !res.IsError && tc.Name == "bash" {
			var ba struct {
				Command    string          `json:"command"`
				Background json.RawMessage `json:"background"`
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
				if bashReset {
					regressed := false
					for _, bc := range bashChanges {
						rel := relForChange(workdir, bc.path)
						warn, reverted := guard.noteEdit(rel, bc.before, readForChange(workdir, bc.path))
						if warn != "" {
							res.Content = appendToContent(res.Content, "\n\n[self-edit check] "+warn)
						}
						regressed = regressed || reverted
					}
					if regressed {
						guard.retractProgress()
					}
				}
				// This exit 0 clears the command's check-churn count only if it is really the
				// command's own: an exit that structurally belongs to a trailing `echo`/`tail`/
				// `|| true` is not evidence the build converged, and reading it as a pass is how
				// a build failing over and over kept resetting the counter that exists to catch
				// exactly that. Leave the count untouched rather than climbing it — the command
				// text proves the exit says nothing, not that the build failed.
				//
				// A BACKGROUND start records nothing at all. Its success means one thing only:
				// a process now exists. The command has not run, so there is no outcome yet to
				// read as a pass — booking one here credits a build with converging before it
				// has compiled a single file. The real exit arrives later, through bash_output.
				if !rawTruthy(ba.Background) && exerciseConverged(ba.Command) {
					guard.noteExerciseResult(ba.Command, false)
				}
			}
		}
		// …and here is where that later exit is read. A background job's outcome reaches the
		// agent through bash_output as `[bg_N exited K]`, and nothing was recording it, so a
		// build that ran in the background was invisible to the churn counter in BOTH
		// directions: never a pass, never a failure, however many times it was re-run. The
		// claim is one-shot per job, so polling a finished job repeatedly cannot inflate the
		// count, and a killed job reports nothing (the agent stopped it — its exit judges
		// nothing). A non-zero exit is that command failing against the current deliverable;
		// an exit 0 clears it only when the command text does not prove the code belongs to
		// something else, exactly as on the foreground path.
		if !res.IsError && tc.Name == "bash_output" {
			var oa struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(tc.Args, &oa) == nil && oa.ID != "" {
				if cmd, exit, ok := builtin.ClaimBackgroundOutcome(oa.ID); ok {
					if exit != 0 {
						guard.noteExerciseResult(cmd, true)
					} else if exerciseConverged(cmd) {
						guard.noteExerciseResult(cmd, false)
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
			guard.noteInspectProgress(guardNovel)
		}
		// A FAILED exercising command tabus the deliverable's current state: "this exact set
		// of file contents was tried and its test failed", so a later edit that circles back
		// to it is flagged (see checkTabu). Inspect-only failures (a bad `ls`/`grep`) are not
		// deliverable evidence and are skipped inside noteExerciseFail.
		if res.IsError && tc.Name == "bash" {
			var ba struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(tc.Args, &ba) == nil {
				guard.noteExerciseFail(ba.Command, string(res.Content))
				guard.noteExerciseResult(ba.Command, true) // this build/test FAILED against the current edit → climb its check-churn count
			}
		}
		// Completion-banner spin: count consecutive pure no-op banners (echo/printf/true/:) an
		// agent spams to keep the turn alive after declaring done; ANY real action resets it. It
		// runs here for every call that reached guard bookkeeping, INCLUDING an IsError bash — a
		// failed real command is still a real action that must reset the spin. Banners are always
		// bash and never error, so they always reach this point. cmd is "" for non-bash tools (or
		// unparsable bash args) → not a banner → resets.
		spinCmd := ""
		if tc.Name == "bash" {
			var ba struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(tc.Args, &ba) == nil {
				spinCmd = ba.Command
			}
		}
		// Environment-wait signal: count a bash call that only waits/polls (any exit code — a
		// poll to a not-yet-ready endpoint FAILS while it waits) so stallIsWait can suppress the
		// futile stuck-recovery coder spawn on a stall that is really an external wait. Runs here
		// (not the success-only block above) precisely because failing polls must be counted.
		guard.noteBashWait(spinCmd)
		guard.noteSpin(tc.Name, spinCmd)
	}
	// Record the agent's before→after change for the council. Gate on the tool's own success
	// (toolOK), NOT res.IsError — a write that landed but failed gofmt/a hook is exactly the
	// broken change the council should scrutinize, and must not read as a no-op turn.
	if guard != nil && changePath != "" && toolOK && fileModifiers[tc.Name] {
		rel := relForChange(workdir, changePath)
		after := readForChange(workdir, changePath)
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
		// Tabu check: this edit may have returned the deliverable to a state whose test already
		// failed this turn (a proven-bad approach the agent is circling back to). Advisory only.
		if tabu := guard.checkTabu(); tabu != "" {
			res.Content = appendToContent(res.Content, "\n\n[tabu] "+tabu)
		}
	}
}
