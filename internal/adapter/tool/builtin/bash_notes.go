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
	"strings"
	"sync"

	"github.com/sayaya1090/magi/internal/core/session"
)

// bodyscanEnabled gates the exit-0 body-scan annotation (MAGI_EXITCODE_BODYSCAN,
// default ON). Off (=0/off/false/no) reproduces the exact pre-scan behavior for a
// clean A/B baseline.
func bodyscanEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MAGI_EXITCODE_BODYSCAN"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

// maskedFailureNote returns a one-line advisory when exit==0 but the output holds a
// high-precision crash/traceback signature — the fingerprint of a failure whose exit
// code was masked. It never fires on a non-zero exit (the ✗/[error] already speaks) and
// requires the Go signatures to be paired with a goroutine dump, so a command that merely
// prints "panic:"/"fatal error:" as data is not flagged. Advisory only: the result stays
// classified by its exit code; this just makes the discrepancy visible.
func maskedFailureNote(exit int, body string) string {
	if exit != 0 {
		return ""
	}
	crash := strings.Contains(body, "Traceback (most recent call last):") || // Python
		strings.Contains(body, "Exception in thread ") || // JVM
		(strings.Contains(body, "panic: ") && strings.Contains(body, "\ngoroutine ")) || // Go panic
		(strings.Contains(body, "fatal error: ") && strings.Contains(body, "\ngoroutine ")) // Go runtime
	if !crash {
		return ""
	}
	return "[note: exit 0 but the output contains a crash/traceback — a failing command may have had its exit code masked (e.g. `|| echo`, `|| true`). Do not treat this as success without an independent check.]"
}

// backgroundTail matches a command whose last character is a lone `&` — the whole
// command (or its final segment) was detached into the background, so the shell's
// exit 0 arrived before the child did anything. `&&` is a list operator, not a
// detach, and must not match.
var backgroundTail = regexp.MustCompile(`(^|[^&])&\s*$`)

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
	if exit != 0 || !backgroundTail.MatchString(strings.TrimSpace(command)) {
		return ""
	}
	prog := bgProgram(command)
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
	return "[note: this command was detached with a trailing `&` — exit 0 only means it STARTED; it is not evidence of completion or success. Poll it (background=true + bash_output) or wait for it (wait_for) instead of assuming it finished or launching it again.]"
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
// idiom: the reported exit says nothing about the primary command — with or without
// crash text in the output (`false || true` fails with clean output and exit 0). It is
// the deterministic complement to maskedFailureNote's output scan, and never fires on a
// non-zero exit (the mask evidently didn't engage, or didn't matter).
func maskingTailNote(exit int, command string) string {
	if exit != 0 || !maskingTail.MatchString(strings.TrimSpace(command)) {
		return ""
	}
	return "[note: this command ends in a `|| …` tail that masks the primary command's exit code — this exit 0 is NOT evidence the primary command succeeded. Re-run without the tail if you need its true status.]"
}

// swallowingPipe matches a command whose FINAL stage is a pure output truncator —
// `| tail …` or `| head …` (a single pipe, not `||`). The model's intent is benign
// (limit output volume), but the effect is the same trap as a masking tail: a pipeline's
// exit status is its LAST stage's, and tail/head almost always exit 0, so a build/test
// that CRASHED still reports exit 0 — and the truncation can drop the very verdict line
// (final "Error"/"Segfault"/"bootstrap complete") the model needs. grep/cat/awk are
// deliberately excluded: their exit code and filtered output are frequently the point.
var swallowingPipe = regexp.MustCompile(`(^|[^|])\|\s*(?:tail|head)\b[^|]*$`)

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
func swallowingPipeNote(exit int, command string, verify bool) string {
	if !verify || exit != 0 || !swallowingPipe.MatchString(strings.TrimSpace(command)) {
		return ""
	}
	return "[note: this exit 0 is the `tail`/`head` at the end of the pipe, NOT the build/test before it — a failed build/test would still show exit 0 here, and the truncation can hide the final error/status line. You do not need to pipe to tail/head: this tool already returns large output capped to its head AND tail with the real exit code. Re-run without the pipe to see the true status.]"
}

// sequencedTail matches a command whose FINAL `;`-sequenced segment cannot fail: a reporter
// (`echo`/`printf`/`true`/`:`) or a truncator (`tail`/`head`/`cat`). A `;` list reports only its
// LAST command's status, so such a tail masks the primary command exactly as `| tail` does — the
// pipe form's sibling, and the one a model reaches for when it wants BOTH a captured log and the
// exit code: `make world > log 2>&1; echo "exit=$?" >> log; tail -30 log`. `&&` is deliberately
// NOT matched: there the tail runs only if the primary SUCCEEDED, so a failure still surfaces its
// own non-zero exit. The segment must hold no further `;`/`|`/`&` so a real command after the
// reporter keeps it unmatched (under-firing on redirections like `2>&1` is fine — advisory only).
var sequencedTail = regexp.MustCompile(`;\s*(?:(?:tail|head|cat|echo|printf|true)\b[^;|&]*|:\s*)$`)

// sequencedTailNote flags an exit-0 result whose command ends in such a segment, on a call the
// model itself declared a verification (verify=true) — the same intent gate swallowingPipeNote
// uses, which keeps it silent on the countless benign `make x; echo done` calls. The live arc it
// answers (fix-ocaml-gc): every one of 12 builds ran as `make world > log 2>&1; echo "exit=$?" >>
// log`, so the tool returned the ECHO's exit 0 with an empty body, the model read "The build
// succeeded!" off it, and — because that exit 0 also told magi's own churn counter the build had
// converged — nothing ever registered that the same build kept failing. The note points at where
// the real status actually is: the `exit=` line the model appended INSIDE the log.
func sequencedTailNote(exit int, command string, verify bool) string {
	if !verify || exit != 0 || !sequencedTail.MatchString(strings.TrimSpace(command)) {
		return ""
	}
	return "[note: this exit 0 is the LAST `;` segment (the `echo`/`tail`/`true` at the end), NOT the build/test before it — a `;` list reports only its final command's status, and that segment cannot fail, so a build that FAILED still shows exit 0 here. If you appended the real status to a log (`echo \"exit=$?\" >> …`), that line is the verdict — read it. Otherwise re-run with the build/test as the LAST thing in the command: this tool already returns large output capped to its head AND tail with the real exit code.]"
}

// ExitCodeMasked reports whether a bash result's exit code is provably NOT the primary command's,
// judged from the command text alone (a masking `|| …` tail, or — on a call the model declared a
// verification — a `| tail`/`| head` pipe or a `;`-sequenced reporter/truncator tail). It is the
// same structural judgement the notes above surface to the MODEL, exported so magi's own guards can
// refuse to read such an exit 0 as evidence: an exit code that belongs to an `echo` says nothing
// about whether the build converged, and must not clear a churn counter that exists to detect a
// build failing over and over. Conservative by construction — it reports only what the command text
// proves, so a false "masked" verdict is not possible from a plain `make world`.
func ExitCodeMasked(command string, verify bool) bool {
	c := strings.TrimSpace(command)
	if maskingTail.MatchString(c) {
		return true
	}
	return verify && (swallowingPipe.MatchString(c) || sequencedTail.MatchString(c))
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
	if usePTY || !ptyGated.MatchString(strings.TrimSpace(command)) {
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
	if exit != 0 || !ephemeralShellState.MatchString(command) {
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
