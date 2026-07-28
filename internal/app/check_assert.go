package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A deliverable check written as model-authored shell can go wrong in ways no review reliably
// catches, and one live run produced three of them at once:
//
//	grep -rn P src/*.c > /tmp/analysis.txt && test -s /tmp/analysis.txt   the check CREATES the
//	                                                                      evidence it then asserts —
//	                                                                      it passes with nothing done
//	grep -q E /tmp/build.log && exit 1 || true; test $? -ne 0             `$?` after `|| true` is 0,
//	                                                                      so it exits 1 in EVERY world
//	                                                                      state — a permanent false fail
//	cd src && make world opt | grep -q '^Done\.$'                         the check re-does the step's
//	                                                                      work each gate cycle
//
// Guarding this by reading the command text does not hold: a name denylist is bypassed by `/bin/rm`,
// a redirect scan by `sh wrapper.sh`, and each new pattern is one indirection away from useless. The
// property that cannot be wrapped is structural — take away the place to put shell in.
//
// So a typed check carries DATA, not a command: `source` names a path, `assert` picks a verb from
// the closed vocabulary below, and THIS FILE builds the invocation. Arguments are passed as an argv
// array with no shell anywhere in the path, so a metacharacter in a model-supplied path or pattern
// is an ordinary byte of an argument — there is no command line for it to break out of. None of the
// three defects above is expressible: nothing here redirects, nothing here runs a model-named
// program, and the verdict is computed in Go from the content rather than from a chain of exit codes.
//
// What this deliberately gives up: the check can no longer execute the deliverable. That belongs to
// the STEP now — it performs the run and records the real output — and the check reads what was
// recorded. The contract of [check sufficiency] is unchanged (the behaviour is still driven and its
// real result still asserted); only the actor moves. The gap it opens — a step that records a
// FABRICATED result — is not closed here but by the provenance audit, which reads magi's own record
// of the tool calls that produced the file.

// checkUnrunnable reports whether an exit code means the CHECK itself could not run, as opposed to
// the deliverable failing. 127 is "command not found" (a missing tool or a wrong path); 126 is "found
// but not executable", and is also what the runner below returns for a check it cannot evaluate — an
// assertion it does not know, or no assertion at all. Both say nothing about the artifact, so every
// gate skips them rather than reworking correct code.
func checkUnrunnable(code int) bool { return code == 127 || code == 126 }

// typedReadCap bounds how much of a source file is pulled back for matching. A recorded build log
// can be tens of megabytes and the whole of it would be held in memory and scanned per gate cycle;
// the assertions are about whether a pattern occurs, which a bounded prefix answers for anything a
// step is expected to record. Truncation is reported so a miss on a huge file is never silent.
const typedReadCap = 512 << 10

// typedProbeTimeoutSec bounds the network/liveness probes below, in the probe's own terms. The outer
// per-check deadline (checkCmdTimeout, applied in runTypedCheck) still applies; this keeps a connect
// to a black-holed port from sitting there for the whole of it.
const typedProbeTimeoutSec = 3

// assertion is one parsed entry of the closed vocabulary: a verb plus its single argument.
type assertion struct {
	verb string
	arg  string
}

// parseAssertion reads `assert` as "verb" or "verb argument". The argument is taken as the REST of
// the string, unsplit, because a regular expression contains spaces far more often than not — the
// one place a Fields-based parse would silently truncate the contract.
//
// An unknown verb is a parse failure rather than a guess: guessing would run some other assertion
// than the one authored, and the caller reports it as "the check could not run" (no verdict), which
// is the honest answer and the same landing an unrunnable command already gets.
func parseAssertion(s string) (assertion, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return assertion{}, false
	}
	verb, arg := s, ""
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		verb, arg = s[:i], strings.TrimSpace(s[i+1:])
	}
	verb = strings.ToLower(verb)
	switch verb {
	case "nonempty", "process_alive":
		return assertion{verb: verb}, true // subject is `source`; no argument
	case "matches", "absent", "equals", "port_open":
		if arg == "" {
			return assertion{}, false // these are meaningless without their argument
		}
		return assertion{verb: verb, arg: arg}, true
	}
	return assertion{}, false
}

// runCheck is the single entry point every gate uses to obtain a check's (output, exit code), and the
// ONLY way a check is ever evaluated: every check goes through the typed runner below. Callers are
// unchanged otherwise — they still layer their own checkUnrunnable/-1 policy and their own Passes call
// on the result.
//
// A check with no `assert` is not run as a command — there is no command path left. It is reported and
// yields no verdict (126), the same landing an unknown verb gets, because a check that names nothing
// to assert proves nothing either way. The authoring prompts emit `source`/`assert`, and the check
// audit converts a stray command-shaped check into that form before it is ever stored; this is the
// floor under both, and it is deliberately visible in the log rather than silent.
func (a *App) runCheck(ctx context.Context, sid session.SessionID, workdir string, c council.DeliverableCheck) (string, int) {
	// A check that names a DEAD turn's scratch reads a directory that no longer exists, so its
	// absence says nothing about the deliverable. 126, not a failure: what is wrong is the check,
	// so the step lands ungated rather than falsely failed. (This turn's own scratch is fine — it
	// has the same lifetime as the check, and it is where the check contract's "redirect the
	// genuine output" belongs, precisely so that file does not land in the deliverable tree.)
	//
	// At the floor rather than at authoring, so it covers every way a check arrives: the plan audit,
	// the coverage fill, and a worker's substitute_check.
	if why := a.scratchSourceRefusal(sid, c.Source); why != "" {
		a.emitToolProgress(sid, plannerActor, "", "check-typed",
			fmt.Sprintf("check-typed: the check for %q %s — no verdict for this check",
				clipLine(strings.TrimSpace(c.Deliverable), 80), why))
		return "the check reads a temporary path: " + why, 126
	}
	if strings.TrimSpace(c.Assert) == "" {
		a.emitToolProgress(sid, plannerActor, "", "check-typed",
			fmt.Sprintf("check-typed: the check for %q carries no `assert` (%s) — nothing to evaluate, so this step "+
				"lands ungated rather than failed", clipLine(strings.TrimSpace(c.Deliverable), 80),
				strings.Join(typedVerbs(), ", ")))
		return "the check carries no assertion", 126
	}
	return a.runTypedCheck(ctx, sid, workdir, c)
}

// runTypedCheck evaluates one typed check and returns it in the (output, exit code) shape the gates
// already speak: 0 passed, 1 failed, 126 the check itself could not run (an unknown verb, or a
// missing subject — no verdict, so the step lands ungated rather than failed), -1 no platform.
//
// A source file that is ABSENT is a FAILURE, not an unrunnable check. The step was supposed to
// record it; that it is not there is a fact about the deliverable and the gate should hear it. This
// is the one place the typed shape reports more than the shell one did — `test -s missing` and
// `cat missing` were indistinguishable from a broken check before.
func (a *App) runTypedCheck(ctx context.Context, sid session.SessionID, workdir string, c council.DeliverableCheck) (string, int) {
	if a.plat == nil {
		return "no platform to run verification", -1
	}
	// Per-check deadline. The shell path bounded each check run in runVerifyCmd; the typed path
	// reaches the platform directly, so the bound has to be re-applied here or a check that BLOCKS
	// strands the turn until its wall clock. It is still reachable: `cat` on a fifo or a device file
	// never returns, and process_alive's probe reads its pid file the same way. A deadline kill
	// surfaces as "could not start/finish" (126) rather than a failing deliverable, which is the
	// honest verdict — a check that never answered says nothing about the artifact.
	if d := checkCmdTimeout(); d > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, d)
		defer cancel()
	}
	as, ok := parseAssertion(c.Assert)
	if !ok {
		a.emitToolProgress(sid, plannerActor, "", "check-typed",
			fmt.Sprintf("check-typed: %q is not an assertion this runner knows (%s) — no verdict for this check",
				clipLine(strings.TrimSpace(c.Assert), 80), strings.Join(typedVerbs(), ", ")))
		return fmt.Sprintf("unknown assertion %q", strings.TrimSpace(c.Assert)), 126
	}
	src := strings.TrimSpace(c.Source)

	switch as.verb {
	case "port_open":
		n, err := strconv.Atoi(strings.Fields(as.arg)[0])
		if err != nil || n < 1 || n > 65535 {
			return fmt.Sprintf("port_open needs a port number, got %q", as.arg), 126
		}
		return a.runTypedProbe(ctx, workdir, portOpenProbe, strconv.Itoa(n), strconv.Itoa(typedProbeTimeoutSec))
	case "process_alive":
		if src == "" {
			return "process_alive needs `source` set to a pid file", 126
		}
		return a.runTypedProbe(ctx, workdir, processAliveProbe, src)
	}

	// Everything left is an assertion ABOUT A FILE the step recorded.
	if src == "" {
		return fmt.Sprintf("%s needs `source` set to the path the step recorded", as.verb), 126
	}
	body, out, code := a.readForCheck(ctx, workdir, src)
	if code != 0 {
		return out, code
	}
	switch as.verb {
	case "nonempty":
		if strings.TrimSpace(body) == "" {
			return fmt.Sprintf("%s is empty", src), 1
		}
		// BEFORE the provenance question, because provenance cannot answer this one: it asks who in
		// THIS run wrote these bytes, and on a file the run never opened the answer is "nobody" —
		// which it reads as nothing to report, and passes. That is the exact hole this closes.
		//
		// A file that was already there and already non-empty makes `nonempty` true before the run
		// starts. Observed live: a step whose deliverable was "Fixed free-list sweep implementation
		// saved" checked `nonempty` on ocaml/runtime/finalise.c — a file that ships with the
		// repository, and not even the one the sweep code is in. It passed one second after a read,
		// with zero writes anywhere in the run, and the step was recorded complete; the actual fix
		// landed in shared_heap.c thirty-eight minutes later.
		if a.staleEvidence(ctx, sid, workdir, src) {
			a.emitToolProgress(sid, plannerActor, "", "check-untouched", fmt.Sprintf(
				"check-untouched: %s is not empty, but nothing in this run wrote to it and it is older "+
					"than the run — it was already "+
					"there, so `nonempty` was true before the step ran and says nothing about the work. "+
					"No verdict for this check.", src))
			return fmt.Sprintf("%s is not empty, but nothing in this run touched it — "+
				"`nonempty` was already true before the step ran", src), 126
		}
		// `nonempty` is the cheapest assertion to satisfy — any text at all does it — so a pass is
		// exactly where to ask where the bytes came from. And here, unlike the verbs that carry a
		// pattern, the answer decides the VERDICT rather than annotating it: an assertion that only
		// requires "something is here", on a file whose something came out of the reply, is a check
		// that proves nothing either way. That is the documented meaning of 126 — no verdict, the
		// step lands ungated rather than failed — and it is the honest reading, because there is
		// nothing in a composed file that could distinguish a real recording from a typed one.
		//
		// Observed live, one second apart: `echo "Bootstrap completed successfully - no crash in
		// build" > /app/crash.log`, then this check flipping to pass on a step whose deliverable was
		// "bootstrap crash reproduced", while the build was segfaulting.
		//
		// Ungated rather than failed is what keeps the legitimate case whole: when the deliverable
		// IS the file the worker wrote, this refuses to CREDIT it, and refuses to reject it too.
		if note := a.auditSourceProvenance(ctx, sid, src, as); note != "" {
			a.emitToolProgress(sid, plannerActor, "", "check-provenance", "check-provenance: "+note)
			return fmt.Sprintf("%s: %d bytes — %s", src, len(body), note), 126
		}
		return fmt.Sprintf("%s: %d bytes", src, len(body)), 0
	case "matches", "absent":
		// Reuse the check's own matcher so a typed pattern behaves EXACTLY as a command check's
		// `expect` does — including the line-wise retry that keeps an anchored pattern from being a
		// permanent false failure against output that ends in a newline.
		hit := council.DeliverableCheck{Expect: as.arg}.Passes(body, 0)
		if as.verb == "absent" {
			if hit {
				return fmt.Sprintf("%s contains %q, which must be absent", src, clipLine(as.arg, 80)), 1
			}
			// `absent` is satisfied by a file being left alone, so a pass on a file NOTHING in this
			// run touched is about the file's history and not about the step — it would have read the
			// same before the run started. No verdict rather than a pass; ungated, not failed.
			if untouchedGateEnabled() && !a.pathTouched(ctx, sid, src) {
				a.emitToolProgress(sid, plannerActor, "", "check-untouched", fmt.Sprintf(
					"check-untouched: %s does not contain %q, but nothing in this run wrote to that file — "+
						"`absent` is satisfied by leaving a file alone, so this says nothing about the work. "+
						"No verdict for this check.", src, clipLine(as.arg, 60)))
				return fmt.Sprintf("%s does not contain %q, but nothing in this run touched it — "+
					"`absent` proves nothing here", src, clipLine(as.arg, 80)), 126
			}
			// The file WAS produced by this run — but by a command magi never saw finish. Then the
			// pattern's absence is about how far the log got, not about the work (check_unfinished.go).
			if untouchedGateEnabled() {
				if why := a.unfinishedRunNote(ctx, sid, src); why != "" {
					a.emitToolProgress(sid, plannerActor, "", "check-unfinished", fmt.Sprintf(
						"check-unfinished: %s does not contain %q, but %s. A pattern that has not been "+
							"reached yet is not a pattern that is absent. No verdict for this check.",
						src, clipLine(as.arg, 60), why))
					return fmt.Sprintf("%s does not contain %q, but %s", src, clipLine(as.arg, 80), why), 126
				}
			}
			// A worker that composed the file makes `absent` pass by writing something else — the
			// pattern is missing because nothing real was ever recorded here.
			return a.withProvenance(ctx, sid, src, as,
				fmt.Sprintf("%s does not contain %q", src, clipLine(as.arg, 80))), 0
		}
		if !hit {
			return fmt.Sprintf("%s does not match %q — content: %s", src, clipLine(as.arg, 80),
				clipLine(strings.TrimSpace(body), 200)), 1
		}
		// It matched — so this is the moment to ask WHERE the matching text came from. A pass is the
		// only outcome the provenance question changes: a check that already failed cannot have been
		// fooled by a fabricated record.
		return a.withProvenance(ctx, sid, src, as,
			fmt.Sprintf("%s matches %q", src, clipLine(as.arg, 80))), 0
	case "equals":
		other, oout, ocode := a.readForCheck(ctx, workdir, as.arg)
		if ocode != 0 {
			return oout, ocode
		}
		if strings.TrimSpace(body) != strings.TrimSpace(other) {
			return fmt.Sprintf("%s differs from %s", src, as.arg), 1
		}
		// Same reading as `nonempty` above, needing BOTH sides: two files the run never touched were
		// equal before it started, and an equality that predates the work is not evidence of it. One
		// side changed is enough to make the comparison about this run.
		if a.staleEvidence(ctx, sid, workdir, src) && a.staleEvidence(ctx, sid, workdir, as.arg) {
			a.emitToolProgress(sid, plannerActor, "", "check-untouched", fmt.Sprintf(
				"check-untouched: %s equals %s, but nothing in this run wrote to either and both are older "+
					"than the run — they were "+
					"already equal before the step ran. No verdict for this check.", src, as.arg))
			return fmt.Sprintf("%s equals %s, but nothing in this run touched either — "+
				"they were already equal before the step ran", src, as.arg), 126
		}
		return a.withProvenance(ctx, sid, src, as, fmt.Sprintf("%s equals %s", src, as.arg)), 0
	}
	return fmt.Sprintf("unhandled assertion %q", as.verb), 126 // unreachable: parseAssertion gates the set
}

// typedVerbs is the vocabulary, for the message a bad `assert` gets back. Fixed order so the note
// reads the same every run.
func typedVerbs() []string {
	return []string{"nonempty", "matches <regexp>", "absent <regexp>", "equals <path>", "port_open <port>", "process_alive"}
}

// readForCheck pulls a source file back through the platform WITHOUT a shell: `cat` is invoked with
// the path as an argv element, so a path containing a space, a quote or a `;` is just a path. It
// returns (contents, diagnostic, code) where code 0 means read, 1 means the file is not readable
// (the deliverable's problem), and 126 means `cat` itself could not be launched (the check's).
func (a *App) readForCheck(ctx context.Context, workdir, path string) (string, string, int) {
	res, err := a.plat.Exec(ctx, port.Cmd{Path: "cat", Args: []string{path}, Dir: workdir, MaxOutput: typedReadCap})
	if cerr := ctx.Err(); cerr != nil {
		// The deadline (or the turn) ended the read. A killed process reports a signal exit, which is
		// indistinguishable from an unreadable file by exit code alone — so ask the context, or a
		// blocking source would be reported as a failing deliverable.
		return "", fmt.Sprintf("reading %s did not finish: %v", path, cerr), 126
	}
	if err != nil && res.ExitCode == 0 {
		// Could not even start the reader — no `cat`, or the platform refused. That says nothing about
		// the artifact, so it must not be reported as a failing deliverable.
		return "", fmt.Sprintf("cannot read %s: %v", path, err), 126
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = "unreadable"
		}
		return "", fmt.Sprintf("%s: %s — the step was to record it here", path, clipLine(msg, 160)), 1
	}
	body := string(res.Stdout)
	if len(body) >= typedReadCap {
		// Say so: a pattern that occurs past the cap would otherwise look like a genuine miss.
		body += fmt.Sprintf("\n[read capped at %d bytes]", typedReadCap)
	}
	return body, "", 0
}

// runTypedProbe runs one of the fixed probe scripts below through python3, passing its subject as an
// ARGUMENT rather than interpolating it into the source. The scripts are constants in this file, so
// what executes is fully determined here; the model contributes a port number or a path and nothing
// else. python3 is the one interpreter [deliverable-check-missing-tool] established as present where
// `ss`/`netstat`/`lsof` are not.
func (a *App) runTypedProbe(ctx context.Context, workdir, script string, args ...string) (string, int) {
	res, err := a.plat.Exec(ctx, port.Cmd{
		Path: "python3", Args: append([]string{"-c", script}, args...), Dir: workdir, MaxOutput: 8 << 10,
	})
	out := strings.TrimSpace(string(res.Stdout) + "\n" + string(res.Stderr))
	if cerr := ctx.Err(); cerr != nil {
		// Same reasoning as readForCheck: a probe killed by the deadline exits by signal, and that
		// must read as "no verdict" rather than as the port being shut or the process dead.
		return "the probe did not finish: " + cerr.Error(), 126
	}
	if err != nil && res.ExitCode == 0 {
		return "cannot run the probe: " + err.Error(), 126
	}
	if res.ExitCode != 0 {
		return out, 1
	}
	return out, 0
}

// portOpenProbe reports whether something accepts a TCP connection on the port. Liveness is the one
// contract a recorded artifact cannot prove — a log saying the server started is equally consistent
// with a server that has since died, or with a stale one from an earlier attempt still holding the
// port — so it is asserted live, here, by a probe magi owns.
const portOpenProbe = `import socket, sys
s = socket.socket()
s.settimeout(float(sys.argv[2]))
try:
    s.connect(("127.0.0.1", int(sys.argv[1])))
    print("port %s open" % sys.argv[1])
except Exception as e:
    print("port %s not open: %s" % (sys.argv[1], e))
    sys.exit(1)
finally:
    s.close()
`

// processAliveProbe reports whether the pid recorded in a file is still running. signal 0 performs
// the permission/existence check without delivering anything.
const processAliveProbe = `import os, sys
try:
    pid = int(open(sys.argv[1]).read().strip())
except Exception as e:
    print("no pid in %s: %s" % (sys.argv[1], e))
    sys.exit(1)
try:
    os.kill(pid, 0)
    print("pid %d alive" % pid)
except Exception as e:
    print("pid %d not alive: %s" % (pid, e))
    sys.exit(1)
`

// withProvenance appends the provenance finding, if any, to a PASSING check's recorded output and
// announces it on the progress stream. A pass is the only outcome the question changes: a check that
// already failed cannot have been fooled by a composed record.
func (a *App) withProvenance(ctx context.Context, sid session.SessionID, src string, as assertion, ok string) string {
	note := a.auditSourceProvenance(ctx, sid, src, as)
	if note == "" {
		return ok
	}
	a.emitToolProgress(sid, plannerActor, "", "check-provenance", "check-provenance: "+note)
	return ok + " — " + note
}
