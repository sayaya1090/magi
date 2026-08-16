package builtin

// Advisory annotations on bash results: deterministic scans that surface what an
// exit code alone hides — a crash printed under a masked exit, a pure exit-code-
// masking tail, or a `&`-detached command whose instant exit 0 only means
// "started". Annotate-only by contract: nothing here reclassifies a result or
// blocks a call. Gated by MAGI_EXITCODE_BODYSCAN (see bodyscanEnabled).

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/envflag"
)

// bodyscanEnabled gates the exit-0 body-scan annotation (MAGI_EXITCODE_BODYSCAN,
// default ON). Off (=0/off/false/no) reproduces the exact pre-scan behavior for a
// clean A/B baseline.
func bodyscanEnabled() bool { return envflag.Enabled("MAGI_EXITCODE_BODYSCAN", true) }

// crashSignatures are lines whose presence in a body is worth hoisting to the head of the result.
// High-precision on purpose: the Go ones must be paired with a goroutine dump, so a command that
// merely prints "panic:" as data is not quoted back at anyone.
var crashSignatures = []string{
	"Traceback (most recent call last):", // Python
	"Exception in thread ",               // JVM
	"panic: ",                            // Go — paired below
	"fatal error: ",                      // Go runtime — paired below
}

// maskedFailureNote QUOTES a crash signature found in the body of an exit-0 result, at the head
// where it cannot be clipped away.
//
// It used to add what that means — "a failing command may have had its exit code masked … Do not
// treat this as success without an independent check" — and that part is gone. The model can read a
// traceback; being told what to conclude from one is the same overreach as calling a SIGPIPE'd stage
// a failure. What magi contributes is PLACEMENT, not reading: the body is clipped for the council
// and long for the model, and a signature four hundred lines down is a fact neither of them
// reliably sees. So it is repeated at the top, verbatim, with nothing added.
//
// Never on a non-zero exit — the status already says something happened, and the note exists for
// the case where it does not.
func maskedFailureNote(exit int, body string) string {
	if exit != 0 {
		return ""
	}
	goDump := strings.Contains(body, "\ngoroutine ")
	for _, sig := range crashSignatures {
		if !strings.Contains(body, sig) {
			continue
		}
		if (sig == "panic: " || sig == "fatal error: ") && !goDump {
			continue
		}
		return "[note: the status above is 0, and the output contains `" + strings.TrimSpace(sig) + "`.]"
	}
	return ""
}

// zeroTestSignatures are runner outputs that say the run DISCOVERED NOTHING. An exit 0 above one
// of these is the emptiest success there is — observed live (the itm→item rename, 2026-08-16):
// `python3 -m unittest test_inventory.py` exited 0 having collected zero pytest-style test
// functions, and two council members cited "tests pass" on its strength. The runner printed the
// truth; it just sat below the clip where nobody read it.
var zeroTestSignatures = []string{
	"Ran 0 tests",       // unittest
	"no tests ran",      // pytest's empty-session summary
	"collected 0 items", // pytest's collection line for the same
}

// zeroTestsNote hoists a zero-tests summary to the head of an exit-0 result, where it cannot be
// clipped away. Like maskedFailureNote it contributes PLACEMENT plus the one structural fact a
// status cannot carry: a runner that found nothing exits clean, so this exit says nothing about
// the suite.
func zeroTestsNote(exit int, body string) string {
	if exit != 0 {
		return ""
	}
	for _, sig := range zeroTestSignatures {
		if strings.Contains(body, sig) {
			return "[note: the status above is 0, and the output says `" + sig + "` — the runner " +
				"discovered no tests, so this exit proves nothing about the suite.]"
		}
	}
	return ""
}

// detachIndex reports where a command detaches something with `&`, or -1. The shell's exit for a
// detached job arrives before the child has done anything, so this is what makes an exit 0 mean
// "started" rather than "finished".
//
// It used to require the `&` to be the command's LAST character. Observed live:
//
//	cd /app/ocaml && make world 2>&1 &
//	sleep 60 && ps aux | grep "make"
//
// The build is detached on the first line and the reported exit 0 belongs to the grep on the
// second, so the one command whose outcome the agent cared about had no status anywhere — and the
// note that exists to say exactly that never fired, because the `&` was not last.
//
// Tokenizing, not reading: a `&` detaches when it is outside quotes, is not inside a heredoc body,
// is not half of `&&`, and is not the target side of an fd redirect (`2>&1`, `>&2`). Nothing here
// asks what the command means.
//
// The heredoc half was missing when this shipped and cost a live false positive within the hour:
// `cat > /tmp/debug_crash.cpp << 'EOF'` followed by C++ containing `&` was called a detach four
// times, and the relaunch warning went on to name the program `}` — a closing brace read out of the
// source as if it were a command. A heredoc body is data the shell hands to a program verbatim,
// exactly like a quoted run, and neither is shell text.
func detachIndex(command string) int {
	var quote byte
	off := 0
	lines := strings.Split(command, "\n")
	for li := 0; li < len(lines); li++ {
		at, intros := scanShellLine(lines[li], &quote)
		if at >= 0 {
			return off + at
		}
		off += len(lines[li]) + 1 // +1 for the newline Split removed
		// Each heredoc this line opened swallows lines up to its OWN terminator, in the order the
		// shell reads them — one `<<` per line was what let a second body be read as shell text.
		for _, h := range intros {
			for li+1 < len(lines) && !h.terminates(lines[li+1]) {
				li++
				off += len(lines[li]) + 1
			}
			if li+1 < len(lines) {
				li++
				off += len(lines[li]) + 1
			}
		}
	}
	return -1
}

// bgLaunched tracks, per session, the program names already detached with a shell
// `&` tail, so a relaunch of the same program gets a stronger warning: the agent is
// about to race its own in-flight install (lock contention, duplicate downloads)
// instead of awaiting it. Session-keyed (each subagent has its own), process-lifetime.
var bgLaunched = struct {
	mu sync.Mutex
	m  map[string]map[string]bool // sessionID -> program set
}{m: map[string]map[string]bool{}}

// backgroundTailNote flags an exit-0 result whose command was `&`-detached: the exit
// says "started", not "finished" — with a stronger variant when the same program was
// already detached earlier in this session and never awaited. Advisory only, and it
// points at the tool's REAL affordances for long commands (background=true +
// bash_output, or wait_for) so the model has a concrete alternative to relaunching.
func backgroundTailNote(exit int, command string, sid session.SessionID) string {
	at := detachIndex(command)
	if exit != 0 || at < 0 {
		return ""
	}
	// The program is the one BEFORE the `&`, which is the piece that got detached — not whatever
	// the agent went on to run in the foreground afterwards.
	prog := bgProgram(command[:at])
	dup := false
	if prog != "" {
		bgLaunched.mu.Lock()
		set := bgLaunched.m[string(sid)]
		if set == nil {
			set = map[string]bool{}
			bgLaunched.m[string(sid)] = set
		}
		dup = set[prog]
		set[prog] = true
		bgLaunched.mu.Unlock()
	}
	if dup {
		return "[note: `" + prog + "` was ALREADY started in the background with `&` earlier in this run and its completion was never confirmed — launching another copy races the in-flight one (lock contention, a duplicate server that squats the port so callers hit the STALE copy). Don't stack another: for a server, free the port first with port_owner{port:N,kill:true} then start ONE with background=true (poll bash_output); for a one-shot job, wait for the first with wait_for.]"
	}
	return "[note: this command detached work with `&` — the exit above belongs to whatever ran in the FOREGROUND, so it says nothing about the detached part, which magi has no job entry, exit, or per-stage status for. Poll it (background=true + bash_output) or wait for it (wait_for) instead of assuming it finished or launching it again.]"
}

// bgProgram extracts the meaningful program name from an `&`-detached command for
// the relaunch warning: last `&&`/`;` segment, first pipeline stage, first token that
// isn't an env assignment or a wrapper (sudo/nohup/env/timeout <n>). Heuristic — a
// miss just downgrades the duplicate warning to the generic note.
func bgProgram(command string) string {
	s := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(command), "&"))
	for _, sep := range []string{"&&", ";"} {
		if i := strings.LastIndex(s, sep); i >= 0 {
			s = s[i+len(sep):]
		}
	}
	if i := strings.Index(s, "|"); i >= 0 {
		s = s[:i]
	}
	fields := strings.Fields(s)
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		switch {
		case strings.Contains(f, "="): // VAR=val prefix
			continue
		case f == "sudo" || f == "nohup" || f == "env":
			continue
		case f == "timeout" && i+1 < len(fields): // skip the duration argument too
			i++
			continue
		default:
			return f
		}
	}
	return ""
}

// maskingTail matches a command whose FINAL list operator is a pure exit-code mask:
// `|| true`, `|| :`, `|| exit 0`, or `|| echo …`. These differ from a genuine fallback
// (`cmd || other-cmd`, which is intentional control flow and must not be flagged): true/:
// /exit 0/echo can never repair the failure, only hide it. The echo arm stops at |&;` so
// a further real command after the echo keeps the tail unmatched (under-fire on quoted
// separators is fine — the scan is advisory).
var maskingTail = regexp.MustCompile(`\|\|\s*(?:true|:|exit\s+0|echo\b[^|&;` + "`" + `]*)\s*$`)

// maskingTailNote flags an exit-0 result whose command text ends in a pure masking
// idiom: the reported exit MAY say nothing about the primary command — with or without
// crash text in the output (`false || true` fails with clean output and exit 0). It is
// the deterministic complement to maskedFailureNote's output scan, and never fires on a
// non-zero exit (the mask evidently didn't engage, or didn't matter).
//
// It states the ambiguity rather than resolving it, because a `||` tail runs ONLY when the command
// before it failed: a zero can mean the command succeeded and the tail never ran, or that it failed
// and the tail succeeded. It used to assert the second. Observed live:
// `ls boot/ocamlrun 2>&1 || echo "boot/ocamlrun not found"` returned exit 0 with `boot/ocamlrun` in
// the body and no trace of the echo — ls had succeeded — and the agent, checking whether the
// bootstrap compiler it needed was still there, was told that answer could not be trusted.
//
// stagesKnown is the one case magi does not need to warn about: it captured the per-stage statuses
// and pipeStageNote already put them in front of the model, so the ambiguity is resolved by a fact
// rather than by this sentence.
func maskingTailNote(exit int, command string, stagesKnown bool) string {
	// Matched against shell text, not raw text: a `||` inside quotes, a comment or a heredoc body
	// is not an operator (see maskNonShell).
	if exit != 0 || stagesKnown || !maskingTail.MatchString(strings.TrimSpace(maskNonShell(command))) {
		return ""
	}
	return "[note: a `|| …` tail runs only when the command before it FAILS, so this exit 0 is " +
		"either that command's (it succeeded and the tail never ran) or the tail's. magi cannot tell " +
		"which from the status alone — read the output to see which one produced it.]"
}

// swallowingPipe matches a command whose FINAL stage is a pure output truncator —
// `| tail …` or `| head …` (a single pipe, not `||`). The model's intent is benign
// (limit output volume), but the effect is the same trap as a masking tail: a pipeline's
// exit status is its LAST stage's, and tail/head almost always exit 0, so a build/test
// that CRASHED still reports exit 0 — and the truncation can drop the very verdict line
// (final "Error"/"Segfault"/"bootstrap complete") the model needs. grep/cat/awk are
// deliberately excluded: their exit code and filtered output are frequently the point.
//
// The truncator must be the command's LAST stage, so its tail may not run past a separator into
// another command — `sequencedTail`'s body has that exclusion and this one, its sibling, did not.
// Observed live (sqlite-with-gcov, 2026-07-30):
//
//	cat /etc/environment | head -1 && echo "PATH=…" > /tmp/p && cat /tmp/p
//
// matched from `| head -1` to the end of the string, and the note told the agent its exit 0 belonged
// to ` head -1 && echo "PATH=…" > /tmp/p && cat /tmp/p` — a "stage" spanning two `&&` operators,
// which is not a thing. The exit belonged to the final `cat`, a real command whose status the result
// DOES report, so the right answer was to say nothing at all. ExitCodeMasked reads this same
// predicate, so the phantom also told magi's churn accounting to discard a real command's zero.
//
// `[<>]&` is admitted so a redirect on the truncator itself (`| tail -50 2>&1`) still matches; a
// bare `&` (a detach) and `&&` do not.
var swallowingPipe = regexp.MustCompile(`(^|[^|])\|\s*(?:tail|head)\b(?:[^|&;\n]|[<>]&)*$`)

// swallowingPipeNote NAMES whose exit code the result carries, which is the whole of what the
// reader is missing. It used to be a paragraph — the trap explained, plus advice to stop piping
// because the tool already caps output — and that paragraph is why it was gated on the model's own
// verify flag: cried on every benign `ls | head`, so it was silenced everywhere it was not asked
// for, and then it was silent on the build that mattered (observed: `make world 2>&1 | tail -100`
// with verify unset, read as a clean build three seconds in).
//
// A label needs no gate. It is one clause, it is true on `ls | head` as much as on a build, and it
// scolds nobody — so it fires always, and the advice it used to carry is now demonstrated instead
// of asserted: the result's own `output:` line names the file holding the full, untruncated output.
//
// (previous doc)
// swallowingPipeNote flags an exit-0 result whose command ends in a `| tail`/`| head` output
// truncator WHEN the model declared this call a build/test verification (verify=true). A
// pipeline's exit status is its LAST stage's, and tail/head almost always exit 0, so a
// verification piped through them reports exit 0 even when the build/test failed, and the
// truncation can drop the verdict line — the fix-ocaml-gc trap (`make world 2>&1 | tail -100`
// → exit 0 → the model mistrusted a good edit and reverted it). It fires ONLY on verify=true:
// gating on the model's own intent instead of guessing from the command avoids the false
// positives an earlier heuristic produced (it nagged on every benign `ls … | head` /
// `git diff … | head`, crying wolf on the case that matters). A verification does not need the
// pipe anyway — the bash tool already returns large output capped to its head AND tail with the
// real exit code.
func swallowingPipeNote(exit int, command string, stagesKnown bool) string {
	if exit != 0 {
		return ""
	}
	// This note says one thing: the status you are looking at belongs to the truncator. When magi
	// captured the stages that is simply false — every stage's status is reported, right above, as
	// numbers. Saying otherwise sends the agent to re-run the command without its pipe to learn
	// what it was already told. Observed live: four of five bash calls in one stretch carried "that
	// command's own status is not reported here" while magi held the stages for all of them.
	if stagesKnown {
		return ""
	}
	// Located in shell text so a `|` the model never typed as an operator cannot match, then
	// QUOTED from the original at the same indices — the masking preserves length for this.
	masked := maskNonShell(command)
	loc := swallowingPipe.FindStringIndex(strings.TrimRight(masked, " \t\n"))
	if loc == nil {
		return ""
	}
	stage := strings.TrimSpace(command[loc[0]:loc[1]])
	if stage == "" {
		return ""
	}
	return "[note: this exit 0 is `" + strings.TrimPrefix(stage, "|") + "`'s, not the command before the pipe — that command's own status is not reported here.]"
}

// sequencedTail matches a command whose FINAL sequenced segment cannot fail: a reporter
// (`echo`/`printf`/`true`/`:`) or a truncator (`tail`/`head`/`cat`). A sequence reports only its
// LAST command's status, so such a tail masks the primary command exactly as `| tail` does — the
// pipe form's sibling, and the one a model reaches for when it wants BOTH a captured log and the
// exit code: `make world > log 2>&1; echo "exit=$?" >> log; tail -30 log`. `&&` is deliberately
// NOT matched: there the tail runs only if the primary SUCCEEDED, so a failure still surfaces its
// own non-zero exit. The segment must hold no further separator so a real command after the
// reporter keeps it unmatched (under-firing on redirections like `2>&1` is fine — advisory only).
//
// A NEWLINE separates commands exactly as `;` does, and the status of the list is still the last
// one's — but only `;` used to count, so the same masking written across two lines was invisible.
// Observed live (cancel-async-tasks, 2026-07-30):
//
//	timeout 5 python3 << 'EOF' … EOF
//	echo "Exit code: $?"
//
// came back `exit 0` while the output carried `Exit code: 130`, and the `;` spelling of the very
// same command was flagged. Excluding `\n` from the segment body matters for the same reason `;`
// is excluded: without it `make x; echo hi` + a newline + `make world` matched, naming the echo as
// the last command when a real one followed it.
var sequencedTail = regexp.MustCompile(`[;\n]\s*(?:(?:tail|head|cat|echo|printf|true)\b[^;|&\n]*|:\s*)$`)

// sequencedTailNote is the same label for the `;` form, and ungated for the same reason.
//
// (previous doc)
// sequencedTailNote flags an exit-0 result whose command ends in such a segment, on a call the
// model itself declared a verification (verify=true) — the same intent gate swallowingPipeNote
// uses, which keeps it silent on the countless benign `make x; echo done` calls. The live arc it
// answers (fix-ocaml-gc): every one of 12 builds ran as `make world > log 2>&1; echo "exit=$?" >>
// log`, so the tool returned the ECHO's exit 0 with an empty body, the model read "The build
// succeeded!" off it, and — because that exit 0 also told magi's own churn counter the build had
// converged — nothing ever registered that the same build kept failing. The note points at where
// the real status actually is: the `exit=` line the model appended INSIDE the log.
func sequencedTailNote(exit int, command string) string {
	if exit != 0 {
		return ""
	}
	masked := maskNonShell(command)
	loc := sequencedTail.FindStringIndex(strings.TrimRight(masked, " \t\n"))
	if loc == nil {
		return ""
	}
	// Name the separator the command actually used. Saying "the last `;` segment" about a command
	// written across two lines points the reader at a character that is not in it.
	sep, kind := ";", "the last `;` segment"
	if masked[loc[0]] == '\n' {
		sep, kind = "\n", "the last line"
	}
	seg := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(command[loc[0]:loc[1]]), sep))
	if seg == "" {
		return ""
	}
	return "[note: this exit 0 is `" + seg + "`'s, " + kind + " — not the command before it.]"
}

// ExitCodeMasked reports whether a bash result's exit code is provably NOT the primary command's,
// judged from the command text alone: a masking `|| …` tail, a `| tail`/`| head` pipe, or a
// `;`-sequenced reporter/truncator tail. It is the same structural reading the notes above surface
// to the MODEL, exported so magi's own guards can refuse to take such an exit 0 as evidence — an
// exit code that belongs to an `echo` says nothing about whether the build converged, and must not
// clear a churn counter that exists to detect a build failing over and over. Conservative by
// construction: it reports only what the command text proves, so a plain `make world` can never be
// called masked.
//
// Deliberately NOT gated on verify, unlike the notes. Those are advice, and advice is worth gating
// on the model's own declared intent — it is what keeps them silent on the countless benign
// `mkdir x; echo done` calls rather than crying wolf. This is not advice: whether an exit code
// belongs to the trailing `echo` is a fact about the command text and does not change with what the
// caller says the command was for. Observed cost of gating it: a build submitted as
// `make … > log 2>&1; echo "exit=$?" >> log` with verify=false had its echo's exit 0 booked as the
// build converging, so one field of the model's own choosing switched off the counter that exists
// to catch that build failing over and over.
// It reads SHELL TEXT, not raw text, for the reason maskNonShell exists — and for a while it was
// the one reader that did not. Every note above was moved onto the masked form after
// `python3 -c "print('done | tail -3')"` was read as a pipeline; this predicate kept matching the
// raw string, so that same command still told magi's churn accounting that its exit 0 belonged to
// a pager. The two directions of the error are not symmetric but both are wrong: a missed mask
// lets an echo's zero clear the counter, and a phantom one refuses a real command's zero as
// evidence it converged.
func ExitCodeMasked(command string) bool {
	c := strings.TrimSpace(maskNonShell(command))
	return maskingTail.MatchString(c) || swallowingPipe.MatchString(c) || sequencedTail.MatchString(c)
}

// binaryOutputNote labels a capture that is not text. It states only what was measured — the byte
// count and where the whole of it is — and names the tools that read bytes, because the useful next
// move after "this is binary" is a different command, not a closer look at the same output.
//
// It does not suppress the bytes. A command may emit binary on purpose, and dropping output magi
// captured would trade one silence for another; the capture file holds it regardless.
func binaryOutputNote(n int, logPath string) string {
	where := "the capture file"
	if logPath != "" {
		where = logPath
	}
	return fmt.Sprintf("[note: this output is BINARY (%d bytes, it contains NUL) — what follows is not "+
		"readable text and reading it as text will mislead you. The bytes are in %s. To inspect it, "+
		"use a tool that reads bytes: `file`, `xxd | head`, `strings | head`, or `od -c | head`.]",
		n, where)
}

// timedOutNote is the body line appended when a foreground command hits its deadline. The bare
// "[timed out after Ns]" it replaces stated a number without saying whose it was, and a model that
// had set the limit ITSELF read the kill as a verdict on the work: cancel-async-tasks ran its own
// test script under `timeout:10` while the script slept 10s, got the bare line, concluded "I've
// been working too long on this", and shipped code it never once executed. The limit is a tool
// argument the caller chose and can raise (`effective` is what actually applied — the 120s default
// when none was given, or the 600s cap when the request exceeded it), so the note names its origin,
// says the command was KILLED rather than judged, and points at both ways to get more time.
func timedOutNote(effective, requested int) string {
	origin := "the default limit (no `timeout` given)"
	switch {
	// A negative was GIVEN, and saying none was is a claim about the caller's own call that magi
	// can see is false. (Zero cannot be told apart from absent through a plain int field, and is
	// not a usable duration either, so it keeps the default wording.)
	case requested < 0:
		origin = fmt.Sprintf("your `timeout` of %ds is not a usable duration, so the default applied", requested)
	case requested > maxBashTimeout:
		origin = fmt.Sprintf("your `timeout` of %ds capped at the %ds maximum", requested, maxBashTimeout)
	case requested > 0:
		origin = "your own `timeout` argument"
	}
	return fmt.Sprintf("[timed out after %ds — %s, not a system deadline. The command was KILLED at that mark, so this is NOT evidence it failed or that it was going to: whatever it was doing simply had not finished. If it needs longer, re-run it with a bigger `timeout` (up to %ds), or with background:true and poll it with bash_output.]",
		effective, origin, maxBashTimeout)
}

// ptyGated matches a command that needs a controlling terminal to interact with: ssh /
// telnet / minicom as a command word (the `\s` after the verb excludes ssh-keygen/ssh-add/
// ssh-copy-id, which are `ssh-` with no space), or a qemu-system invocation with a serial
// console on the terminal (`-nographic`/`-serial`). These read a password or a login prompt
// from /dev/tty, which a plain pipe (the default background stdin) cannot answer.
var ptyGated = regexp.MustCompile(`(?:^|[;&|(]\s*)(?:ssh|telnet|minicom)\s|qemu-system\S*.*-(?:nographic|serial)\b`)

// ptyNeededNote steers a tty-gated command toward the interactive pty path. It fires only
// when pty is NOT already set: an ssh password prompt / serial getty login cannot be driven
// over a pipe, so without a pty the model waits out the whole timeout on a prompt it can
// never answer — the qemu-alpine-ssh failure. Advisory; the caller decides where to surface it.
//
// The steer is platform-aware, because pty:true is REJECTED where ptySupported is false
// (bgproc.go) — telling the model to re-launch that way there is an instruction it can only be
// refused for following, and it is delivered at the moment the model is already blocked. Where
// there is no pty the obligation is the same (do not sit on a prompt nothing can answer) but the
// route is to make the command non-interactive instead.
func ptyNeededNote(command string, usePTY bool) string {
	// Shell text only, for the reason maskNonShell exists: an `ssh` named inside a quoted string,
	// a comment, or a heredoc body being written to a file is not a command about to run.
	if usePTY || !ptyGated.MatchString(strings.TrimSpace(maskNonShell(command))) {
		return ""
	}
	const why = "[note: this command needs a controlling terminal — ssh reads its password from /dev/tty (not stdin), and a serial/getty login expects a tty; a plain pipe cannot drive them. "
	if !ptySupported {
		return why + "This platform has NO pty, so an interactive prompt cannot be answered here at all — make the command non-interactive instead: key-based auth, `sshpass`, or `-o BatchMode=yes -o StrictHostKeyChecking=accept-new` so it fails immediately rather than stalling on a prompt.]"
	}
	return why + "Re-launch with background:true AND pty:true, then answer prompts with bash_input and read with bash_output. (Key-based auth or `sshpass` avoids the prompt entirely.)]"
}

// ephemeralShellState matches a command that mutates shell state with the intent
// of it lasting — `export` / `source` as a command word. A bare VAR=val prefix is
// NOT matched: it scopes the single command and models use it correctly all the
// time (CGO_ENABLED=0 go build …); it's `export` that signals "for later".
var ephemeralShellState = regexp.MustCompile(`(^|[;&|(]\s*)(export|source)\s`)

// ephemeralNoted tracks, per session, whether the ephemeral-shell-state note has
// already been delivered — it is a fact about the TOOL (every call is a fresh
// shell), so once per session is enough; repeating it on every export would be
// noise. Session-keyed like bgLaunched, process-lifetime.
var ephemeralNoted = struct {
	mu sync.Mutex
	m  map[string]bool
}{m: map[string]bool{}}

// ephemeralEnvNote flags the FIRST successful command in a session that uses
// export/source: shell state set in a bash call does not outlive it, and other
// processes never see it. The live failure this teaches against: an agent "made
// a binary available in the PATH" via `export PATH=… && sqlite3 …`, verified
// through the same prefix, and landed a deliverable a fresh process could not
// find — the whole task lost to a missing symlink. Advisory, once per session.
func ephemeralEnvNote(exit int, command string, sid session.SessionID) string {
	// Shell text only — and here a false fire costs more than noise. The note is delivered ONCE per
	// session, so an `export` inside a heredoc body, a quoted string or a comment spends the single
	// delivery on a command that set no shell state, and the real `export PATH=… && …` it exists to
	// warn about gets nothing. Measured: `cat > s.sh <<TAG … cd /x ; export PATH=/y … TAG`,
	// `foo | source bar` in a body, `python3 -c "print('a ; export B=1')"` and a trailing
	// `# note: ; export PATH=/x` all fired it.
	if exit != 0 || !ephemeralShellState.MatchString(maskNonShell(command)) {
		return ""
	}
	ephemeralNoted.mu.Lock()
	seen := ephemeralNoted.m[string(sid)]
	ephemeralNoted.m[string(sid)] = true
	ephemeralNoted.mu.Unlock()
	if seen {
		return ""
	}
	return "[note: shell state set in this call (export/source/cd) does NOT outlive it — every bash call starts a fresh shell, and other processes never see it. If something must stay available afterwards (a PATH entry, an env var, an activated environment), persist it in the filesystem — install or symlink the binary, write the config — and re-verify WITHOUT the in-call setup.]"
}

// A pipeline reports its LAST stage's status, so `make world 2>&1 | tee build.log | tail -50`
// answers 0 for a build that died at rule one. magi could see the shape of the command and say it
// did not know; bash can say what actually happened, because PIPESTATUS holds every stage.
//
// It is read out of band — written to a file the command never touches — for two reasons. The
// output must stay exactly what the command printed, or a grep over it starts matching magi's
// bookkeeping. And the exit status must stay the shell's own: turning on `pipefail` would make the
// same command report failure where it used to report success, which is the shell lying to an agent
// that reads that number to decide what to do next. So nothing changes except that magi now knows.

// withPipeStatus wraps a shell invocation so the LAST pipeline's per-stage statuses are written to a
// side file. Returns the (possibly rewritten) invocation and the file path, or an empty path when
// the shell cannot report them — dash has no PIPESTATUS, and Windows has no pipeline array at all.
func withPipeStatus(name string, args []string, tmpDir string) (string, []string, string) {
	if !strings.HasSuffix(name, "bash") || len(args) != 2 || args[0] != "-c" {
		return name, args, ""
	}
	f, err := os.CreateTemp(tmpDir, "magi-pipestatus-*")
	if err != nil {
		return name, args, ""
	}
	path := f.Name()
	_ = f.Close()
	// PIPESTATUS must be read by the very next command, so it is captured before anything else —
	// including the status assignment, which would otherwise overwrite it with its own success. The
	// pipeline's own exit is the array's last element, so re-exiting with it changes nothing.
	// PIPESTATUS can be EMPTY — a command ending in `&` completes no foreground pipeline — so the
	// trailer must survive that rather than index into nothing. Observed: `cmd &` produced
	// "bad array subscript" and exit 255, turning a working background start into a tool error.
	wrapped := args[1] + "\n__magi_ps=(\"${PIPESTATUS[@]}\")\n__magi_n=${#__magi_ps[@]}\n" +
		"if [ \"$__magi_n\" -gt 0 ]; then printf '%s ' \"${__magi_ps[@]}\" > " + shellQuote(path) +
		"; exit \"${__magi_ps[$((__magi_n-1))]}\"; fi\nexit 0\n"
	return name, []string{"-c", wrapped}, path
}

// shellQuote wraps s in single quotes for safe inclusion in a shell command.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// readPipeStatus reads the per-stage statuses back, or nil when none were captured.
func readPipeStatus(path string) []int {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []int
	for _, f := range strings.Fields(string(b)) {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil // a partial or garbled capture says nothing; do not guess from it
		}
		out = append(out, n)
	}
	return out
}

// pipeStageNote reports the per-stage statuses of a pipeline whose reported exit cannot show them:
// the shell hands back the LAST stage's status, so `make world | tail -50` says 0 for a build that
// died. It states the numbers and stops.
//
// It used to read them, and that reading was the bug — three times in one hour. A stage killed by
// SIGPIPE (`… | head -50` produces 141 every time, because head exited after fifty lines) was
// called a failure. grep exiting 1 because nothing matched was called a failure. Then the exemption
// list that fixed those two needed to know WHICH verb produced which status, which meant parsing
// the command into stages, which meant deciding which pipeline PIPESTATUS had captured — and that
// question has no answer in `a | b || c | d`, where the tail runs only when the head fails.
//
// Every one of those defects existed because magi was interpreting. The model reads shell output
// for a living; it knows what `grep` returns when nothing matches, and it can see the output. What
// it CANNOT see is the status the shell threw away. So magi hands it that, as a fact, and does not
// say what it means.
//
// The note used to be gated on `exit == 0`, on the reasoning that a nonzero exit already says
// something failed, so nothing could be hidden behind it. That reasoning is itself an
// interpretation, and a wrong one: the reported status belongs to the LAST stage, so an earlier
// stage's failure has no status to appear in whether the last one succeeded or not. Observed live
// (kv-store-grpc, 2026-07-30):
//
//	ps aux | grep server.py | grep -v grep      →  exit 1   (stages 127 → 1 → 1)
//	echo hi | grep nomatch                      →  exit 1   (stages   0 → 1)
//
// Two commands, one status line, opposite facts — "the program does not exist" against "the program
// ran and matched nothing" — and magi held the number that tells them apart and dropped it. Worse
// with `2>/dev/null` on the failing stage, where the message is gone too.
//
// So the gate is the fact, not the exit: speak when a stage OTHER than the last one is nonzero,
// because that is exactly the failure the reported status cannot express. On exit 0 this is the
// same set as before (the last stage IS the 0), so nothing that used to fire stops firing, and the
// benign `cmd | grep` that simply found nothing (0 → 1) stays silent.
func pipeStageNote(exit int, stages []int) string {
	if len(stages) < 2 {
		return "" // magi captured no pipeline, so it has nothing the status does not already say
	}
	hidden := false
	for _, st := range stages[:len(stages)-1] {
		if st != 0 {
			hidden = true
		}
	}
	if !hidden {
		return "" // every stage before the last succeeded — the reported status is the whole story
	}
	parts := make([]string, len(stages))
	for i, st := range stages {
		parts[i] = strconv.Itoa(st)
	}
	return "[note: the status above is the pipeline's LAST stage. Its stages exited " +
		strings.Join(parts, " → ") + " (left to right).]"
}

// statusAnnotator says one thing about how a command's reported status should be read, or "" when
// it has nothing to say about this one. Every annotator gets the same facts — the exit the shell
// reported, the command as written, the output as it will be displayed, the session, and whether
// magi captured the pipeline's per-stage statuses — so the list can be reordered without rewriting
// call sites.
type statusAnnotator func(exit int, command, out string, sid session.SessionID, stagesKnown bool) string

// statusAnnotators is the precedence order, first match wins. Highest first: a crash in the body is
// the most specific thing that can be said about an exit 0, and "the shell state does not persist"
// is the most general. Stacking several would bury the sharpest note under the vaguest.
var statusAnnotators = []statusAnnotator{
	// The output carries a real crash/traceback while the status says success — almost always a
	// failing code swallowed by a `|| echo`/`|| true` tail.
	func(exit int, _, out string, _ session.SessionID, _ bool) string { return maskedFailureNote(exit, out) },
	// A test runner that DISCOVERED NOTHING and exited clean — the emptiest success there is.
	func(exit int, _, out string, _ session.SessionID, _ bool) string { return zeroTestsNote(exit, out) },
	// The command ends in a shell `&`: exit 0 only means the child STARTED. A weak model reads the
	// instant clean exit as progress, abandons the in-flight install/build, and relaunches it.
	func(exit int, cmd, _ string, sid session.SessionID, _ bool) string {
		return backgroundTailNote(exit, cmd, sid)
	},
	// No crash text, but the COMMAND ends in a pure masking idiom — the exit 0 is structurally
	// uninformative even when the output looks clean (`false || true` fails silently).
	func(exit int, cmd, _ string, _ session.SessionID, clean bool) string {
		return maskingTailNote(exit, cmd, clean)
	},
	// It ends in `| tail`/`| head`: the exit belongs to the truncator, not to the work.
	func(exit int, cmd, _ string, _ session.SessionID, clean bool) string {
		return swallowingPipeNote(exit, cmd, clean)
	},
	// The `;` form of the same thing — what a model writes when it wants a captured log AND the
	// exit code, and the shape that produced the most convincing false success there is.
	func(exit int, cmd, _ string, _ session.SessionID, _ bool) string { return sequencedTailNote(exit, cmd) },
	// First export/source of the session: shell state does not outlive the call, said before an
	// ephemeral setup gets mistaken for a persistent deliverable.
	func(exit int, cmd, _ string, sid session.SessionID, _ bool) string {
		return ephemeralEnvNote(exit, cmd, sid)
	},
}
