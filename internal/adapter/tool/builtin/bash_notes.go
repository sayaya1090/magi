package builtin

// Advisory annotations on bash results: deterministic scans that surface what an
// exit code alone hides — a crash printed under a masked exit, a pure exit-code-
// masking tail, or a `&`-detached command whose instant exit 0 only means
// "started". Annotate-only by contract: nothing here reclassifies a result or
// blocks a call. Gated by MAGI_EXITCODE_BODYSCAN (see bodyscanEnabled).

import (
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
		return "[note: `" + prog + "` was ALREADY started in the background with `&` earlier in this run and its completion was never confirmed — launching another copy races the in-flight one (lock contention, duplicate downloads). Wait for the first: use bash with background=true and poll bash_output, or block on completion with wait_for.]"
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

// swallowingPipeNote flags an exit-0 result whose command ends in a `| tail`/`| head`
// output truncator. Distinct from maskingTailNote (which targets the clearly-wrong
// `|| true` mask), this teaches a redundancy the weak model reaches for reflexively: the
// bash tool ALREADY returns large output capped to its head AND tail with the true exit
// code, so piping to tail/head only discards that exit code and can hide the verdict.
// The live failure it targets: fix-ocaml-gc, `make world 2>&1 | tail -100` → exit 0 → the
// model could not tell its fix built, mistrusted a good edit, and reverted it.
func swallowingPipeNote(exit int, command string) string {
	if exit != 0 || !swallowingPipe.MatchString(strings.TrimSpace(command)) {
		return ""
	}
	return "[note: this exit 0 is the `tail`/`head` at the end of the pipe, NOT the command before it — a build/test that failed would still show exit 0 here, and the truncation can hide the final error/status line. You do not need to pipe to tail/head to limit output: this tool already returns large output capped to its head AND tail with the real exit code. Re-run without the pipe to see the true status.]"
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
func ptyNeededNote(command string, usePTY bool) string {
	if usePTY || !ptyGated.MatchString(strings.TrimSpace(command)) {
		return ""
	}
	return "[note: this command needs a controlling terminal — ssh reads its password from /dev/tty (not stdin), and a serial/getty login expects a tty; a plain pipe cannot drive them. Re-launch with background:true AND pty:true, then answer prompts with bash_input and read with bash_output. (Key-based auth or `sshpass` avoids the prompt entirely.)]"
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
