package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Bash runs a shell command in the working directory and returns its combined
// output and exit code, with a timeout. It is a "danger" tool (permission-gated)
// and the agent's escape hatch for builds/tests/git. (F-TOOL bash)
type Bash struct{}

type bashArgs struct {
	Command    string  `json:"command"`
	Timeout    flexInt `json:"timeout"`    // seconds (default 120, max 600); tolerant parse (flexInt)
	Background bool    `json:"background"` // run detached; returns an id to poll/kill
}

func (Bash) Name() string { return "bash" }
func (Bash) Description() string {
	return "Run a shell command in the working directory. Returns combined stdout/stderr and the exit code. Use for builds, tests, git, and file operations not covered by other tools. A non-zero exit is reported as normal, useful output — never mask a command's failure just to make it look clean (`|| true`, `|| echo $?`; in PowerShell, error-swallowing try/catch or -ErrorAction SilentlyContinue): that turns a failing check into a false pass. Set background=true for a long-running command (dev server, watcher, slow build): it returns an id immediately; read its output with bash_output, send stdin with bash_input (to drive a REPL/interactive program), and stop it with bash_kill."
}
func (Bash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout":{"type":"integer","description":"seconds (default 120, max 600)"},"background":{"type":"boolean","description":"run detached; returns an id for bash_output/bash_kill"}},"required":["command"]}`)
}

func (Bash) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a bashArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Command) == "" {
		return errResult("", "command is required"), nil
	}
	if a.Background {
		p, err := bg.start(env.Workdir, env.Sandbox, a.Command)
		if err != nil {
			return errResult("", "failed to start background command: "+err.Error()), nil
		}
		return okText("", fmt.Sprintf("started background command %s — poll with bash_output{id:%q}, stop with bash_kill{id:%q}", p.id, p.id, p.id)), nil
	}
	timeout := int(a.Timeout)
	if timeout <= 0 {
		timeout = 120
	}
	if timeout > 600 {
		timeout = 600
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	name, args := shell(a.Command)
	// OS-level confinement (sandbox axis): wrap the command so writes
	// stay in the workspace and the network is off, when the platform supports it.
	// Falls back to unconfined transparently — the policy layer's command scan and
	// permission prompt still apply either way.
	if argv, wrapped := sandboxArgv(env.Sandbox, a.Command); wrapped {
		name, args = argv[0], argv[1:]
	}
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Dir = env.Workdir
	// On timeout, CommandContext kills the shell but a child (e.g. `sleep`) can
	// keep the output pipe open, blocking CombinedOutput until the child exits.
	// WaitDelay bounds that post-kill wait so a timed-out command returns promptly.
	cmd.WaitDelay = 2 * time.Second
	// Windows confinement is applied as a process token (no CLI wrapper exists).
	sboxAttr := sandboxProcAttr(env.Sandbox)
	// Run in a new session with no controlling terminal (Unix), so a command that tries to
	// prompt by reading /dev/tty — git credentials, ssh host-key confirmation, sudo, a pager
	// — gets no tty and fails fast instead of hanging until the timeout. stdin is already
	// /dev/null (unset), so stdin reads also get EOF. This covers the common tty-based
	// prompts; it does NOT defeat prompts routed through an askpass/credential helper (those
	// don't need a tty), which remain bounded only by the command timeout. No-op on Windows,
	// where there is no controlling-terminal concept to detach.
	cmd.SysProcAttr = detachTTY(sboxAttr)
	out, err := runCapture(cmd)
	// Safety net: if a token-confined launch (Windows) fails to START (process never
	// ran), retry unconfined so confinement can never break the bash tool outright.
	// Keyed on the sandbox token specifically — tty detachment is kept on the retry
	// and never causes a start failure, so it must not trigger this fallback.
	if err != nil && cmd.ProcessState == nil && sboxAttr != nil {
		cmd = exec.CommandContext(cctx, name, args...)
		cmd.Dir = env.Workdir
		cmd.WaitDelay = 2 * time.Second
		cmd.SysProcAttr = detachTTY(nil)
		out, err = runCapture(cmd)
	}

	exit := 0
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}
	body := string(out)
	if cctx.Err() == context.DeadlineExceeded {
		body += fmt.Sprintf("\n[timed out after %ds]", timeout)
		return errResult("", truncateOut(body)), nil
	}
	if err != nil && exit == 0 {
		// Launch failure (command not found, etc.) rather than non-zero exit.
		return errResult("", err.Error()+"\n"+truncateOut(body)), nil
	}
	disp := truncateOut(body)
	// A command that returns exit 0 while its output carries a real crash/traceback is
	// almost always one whose failing exit code was swallowed by a `|| echo`/`|| true`
	// tail. Both the model (which read the "exit 0" as a pass) and the council (which sees
	// this result marked [ok]) would otherwise rubber-stamp it. Annotate — never reclassify
	// — right after the status line so the note sits at the head, where the council's
	// head-clip and the model both see it. Flag-gated for A/B isolation.
	if bodyscanEnabled() {
		if note := maskedFailureNote(exit, disp); note != "" {
			disp = note + "\n" + disp
		} else if note := backgroundTailNote(exit, a.Command, env.SessionID); note != "" {
			// The command ends in a shell `&`: exit 0 only means the child STARTED.
			// A weak model reads the instant clean exit as progress, abandons the
			// in-flight install/build, and relaunches it — the compile-compcert
			// failure arc (7 competing opam switch creations, none awaited).
			disp = note + "\n" + disp
		} else if note := maskingTailNote(exit, a.Command); note != "" {
			// No crash text, but the COMMAND ends in a pure masking idiom — the
			// exit 0 is structurally uninformative even when the output looks clean
			// (`false || true` fails silently). Static on the command string, so it
			// also catches failures that print nothing.
			disp = note + "\n" + disp
		}
	}
	res := okText("", fmt.Sprintf("exit %d\n%s", exit, disp))
	res.IsError = exit != 0
	return res, nil
}

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

// runCapture runs cmd with its combined stdout/stderr sent to a temp FILE, then
// returns the captured bytes and the run error. Unlike cmd.CombinedOutput (which
// wires stdout/stderr to an os.Pipe), a file lets a command that backgrounds a child
// with `&` hand that child a plain file fd: the child is not tethered to a pipe whose
// read end our Wait would close, so it survives this call — and magi's own exit —
// instead of dying by SIGPIPE. sh exiting also returns Wait immediately (no pipe to
// drain), so `server &` no longer blocks on WaitDelay. On temp-file failure it falls
// back to the in-memory CombinedOutput path so the tool never breaks outright.
func runCapture(cmd *exec.Cmd) ([]byte, error) {
	f, err := os.CreateTemp("", "magi-bash-*.log")
	if err != nil {
		return cmd.CombinedOutput()
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	werr := cmd.Wait()
	// Read whatever was flushed by the time the shell exited, bounded to captureCap so a
	// command that emits hundreds of MB (`cat huge`, a runaway build) can't buffer it all
	// into memory — we keep the head and the tail (where errors and final status usually
	// are) and elide the middle. A surviving `&` child keeps writing to its own fd on the
	// (now unlinked) inode; that later output is intentionally not captured here. (I1)
	data := readHeadTail(name, captureCap)
	return data, werr
}

// captureCap bounds how much of a command's output runCapture retains in memory. It sits
// above truncateOut's display cap so the head+tail context survives into the display
// truncation, while keeping the buffer to a fixed few hundred KB regardless of output size.
const captureCap = 256 << 10

// readHeadTail returns the file's full content when it fits in cap, else the first and last
// cap/2 bytes joined by an elision marker. Bounds memory for pathologically large output
// while preserving the tail — a truncated build log's error is almost always at the end, so
// head-only capture would drop exactly the useful part.
func readHeadTail(path string, cap int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	if fi.Size() <= cap {
		b, _ := io.ReadAll(f)
		return b
	}
	half := cap / 2
	head := make([]byte, half)
	n1, _ := io.ReadFull(f, head)
	var tail []byte
	if _, err := f.Seek(-half, io.SeekEnd); err == nil {
		tail = make([]byte, half)
		n2, _ := io.ReadFull(f, tail)
		tail = tail[:n2]
	}
	omitted := fi.Size() - int64(n1) - int64(len(tail))
	marker := fmt.Sprintf("\n…[%d bytes omitted]…\n", omitted)
	out := make([]byte, 0, int64(n1)+int64(len(marker))+int64(len(tail)))
	out = append(out, head[:n1]...)
	out = append(out, marker...)
	return append(out, tail...)
}

// shell returns the platform shell invocation for a command string.
func shell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// truncateOut caps very large command output for display, keeping the head AND the tail
// (¾ / ¼) with the middle elided — a build/test failure's actual error and final status
// live at the end, so head-only truncation would hide exactly what the agent needs. Cuts
// on rune boundaries so the result is always valid UTF-8.
func truncateOut(s string) string {
	const max = 30 * 1024
	if len(s) <= max {
		return s
	}
	head := max * 3 / 4
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	tail := len(s) - (max - head)
	for tail < len(s) && !utf8.RuneStart(s[tail]) {
		tail++
	}
	return s[:head] + fmt.Sprintf("\n…(%d bytes omitted)…\n", tail-head) + s[tail:]
}
