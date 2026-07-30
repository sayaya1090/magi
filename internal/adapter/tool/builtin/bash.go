package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

// Foreground command deadline: applied when the caller gives no `timeout`, and the ceiling
// its argument is clamped to. Named because timedOutNote reports both back to the caller.
const (
	defaultBashTimeout = 120
	maxBashTimeout     = 600
)

type bashArgs struct {
	Command    string   `json:"command"`
	Timeout    FlexInt  `json:"timeout"`    // seconds (default 120, max 600); tolerant parse (FlexInt)
	Background FlexBool `json:"background"` // run detached; returns an id to poll/kill
	Pty        FlexBool `json:"pty"`        // with background: attach a real terminal (ssh/serial/curses)
}

// There was a `verify` flag here, which the description told the model to set when a command was
// its build/test check. Nothing read it. It had gated magi's own masked-exit accounting once, which
// was the defect that unwired it — a field the model fills in must never switch off what magi
// observed — but the field and the instruction to set it stayed behind, so every request carried
// the schema for it and the model kept spending a token on `"verify": false`. The advice inside it
// was worth keeping and is now plain prose: run the check directly, do not pipe it through
// tail/head, because then the exit you get back is the pager's.

func (Bash) Name() string { return "bash" }
func (Bash) Description() string {
	return "Run a shell command in the working directory; returns combined stdout/stderr and the exit code (a non-zero exit is normal output — never mask it with `|| true`). Each call is a FRESH shell. To keep a process (e.g. a server) alive across steps set background=true — it returns an id: poll with bash_output, stop with bash_kill, send input with bash_input. START a process and VERIFY it in SEPARATE calls: start it with background=true, then check it (netstat/pgrep/curl) in a foreground call so you see the result. Add pty=true (with background) for programs needing a real terminal (ssh password prompt, serial login, curses UI). When a command IS your build/test check, run it directly — piping it through tail/head gives you the pager's exit code, not the check's."
}
func (Bash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"},"timeout":{"type":"integer","description":"seconds (default 120, max 600)"},"background":{"type":"boolean","description":"run detached; returns an id for bash_output/bash_input/bash_kill"},"pty":{"type":"boolean","description":"with background: real terminal for ssh/serial/curses programs"}},"required":["command"]}`)
}

func (Bash) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a bashArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if strings.TrimSpace(a.Command) == "" {
		return errResult("", "command is required"), nil
	}
	// Self-kill protection (see bash_selfkill.go): a kill-by-match whose target
	// matches our own process is blocked outright — self-termination is
	// unrecoverable, so this is the one deliberate exception to annotate-only.
	if selfKillGuardEnabled() {
		cmdline, name := selfIdentity()
		if reason := selfKillReason(a.Command, cmdline, name); reason != "" {
			return errResult("", reason), nil
		}
	}
	if a.Background {
		// background=true ALREADY persists the process as its own job, so a trailing `&` (or a
		// `nohup` wrapper) is worse than redundant: it backgrounds the process INSIDE the job's
		// shell, the shell then has nothing left to do and exits immediately, and bash_output
		// reports `[job exited 0]` — the model reads that as "the server died" and restarts it in
		// a loop, even though (with nohup) a detached copy is actually running. The tool description
		// says not to do this, but weak models do it anyway, so strip it and run the bare command as
		// the job's foreground — then the job stays alive exactly as long as the process does.
		cmd, stripped := stripBackgroundArtifacts(a.Command)
		p, err := bg.start(env.Workdir, env.ScratchTmp, env.Sandbox, cmd, bool(a.Pty))
		if err != nil {
			return errResult("", "failed to start background command: "+err.Error()), nil
		}
		msg := fmt.Sprintf("started background command %s — poll with bash_output{id:%q}, stop with bash_kill{id:%q}", p.id, p.id, p.id)
		if stripped {
			msg += " (note: dropped a redundant `&`/`nohup` — background=true already keeps it running as this job; poll bash_output to see it stay up)"
		}
		// `timeout` is a FOREGROUND deadline, and this branch returns before it is ever read — a
		// background job's whole point is to outlive the call. Ignoring it is right; doing so in
		// silence is not. Observed live (large-scale-text-editing): `timeout:5` with
		// background:true, and the job was still running when the agent killed it at 2m18s. The
		// same message already discloses the `&` it dropped, so the branch's own rule is that a
		// part of the call magi did not honor gets said out loud.
		if int(a.Timeout) > 0 {
			msg += fmt.Sprintf(" (note: your `timeout` of %ds was NOT applied — it bounds a foreground"+
				" call, and this one returned as soon as the job started. Nothing will stop this job on"+
				" a clock: kill it with bash_kill, or build the limit into the command itself with"+
				" `timeout %d …`)", int(a.Timeout), int(a.Timeout))
		}
		if bool(a.Pty) {
			msg += " (on a pseudo-terminal — send keystrokes/answers with bash_input)"
		} else if note := ptyNeededNote(cmd, false); note != "" {
			// Backgrounded a tty-gated command (ssh/serial) on a plain pipe — it can't be
			// driven. Steer to pty:true rather than let the model poll a dead prompt.
			msg = note + "\n" + msg
		}
		return okText("", msg), nil
	}
	// `pty` is a background-only capability — this path never reads it, so a caller that asked for
	// a terminal without background got a plain pipe and no word about it. Same rule as the
	// background branch's `&` and `timeout` notes: what magi did not honor, magi says. It matters
	// more here than most: a program that wanted a tty (ssh, a serial login) will sit on a prompt
	// nothing can answer until the deadline, and the caller had asked for exactly the thing that
	// would have avoided that.
	ptyIgnored := bool(a.Pty)

	timeout := int(a.Timeout)
	if timeout <= 0 {
		timeout = defaultBashTimeout
	}
	if timeout > maxBashTimeout {
		timeout = maxBashTimeout
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
	// The exit a pipeline reports is its LAST stage's, so `make … | tail` says 0 for a build that
	// died. bash knows better — PIPESTATUS holds every stage — so ask for it out of band: written to
	// a side file, never into the output, and the command's own status is passed through untouched.
	name, args, psPath := withPipeStatus(name, args, env.ScratchTmp)
	if psPath != "" {
		defer os.Remove(psPath)
	}
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.Dir = env.Workdir
	cmd.Env = scratchEnv(env.ScratchTmp)
	// On timeout the default context-cancel kills only the launched shell; a child it spawned
	// (a build, ssh, ping) can survive holding the inherited output handle, which on Windows
	// wedges cmd.Wait forever so the tool hangs past its timeout for ANY command. armCancel
	// wires a whole-tree kill into cancellation and keeps a WaitDelay backstop so Wait returns.
	armCancel(cmd)
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
	started := time.Now()
	bashDebugf("exec start: timeout=%ds shell=%s cmd=%q", timeout, name, dbgClip(a.Command))
	out, logPath, whole, err := runCapture(cmd, env.ScratchLogs)
	stages := readPipeStatus(psPath)
	// Safety net: if a token-confined launch (Windows) fails to START (process never
	// ran), retry unconfined so confinement can never break the bash tool outright.
	// Keyed on the sandbox token specifically — tty detachment is kept on the retry
	// and never causes a start failure, so it must not trigger this fallback.
	if err != nil && cmd.ProcessState == nil && sboxAttr != nil {
		cmd = exec.CommandContext(cctx, name, args...)
		cmd.Dir = env.Workdir
		armCancel(cmd)
		cmd.SysProcAttr = detachTTY(nil)
		out, logPath, whole, err = runCapture(cmd, env.ScratchLogs)
	}

	exit := 0
	if cmd.ProcessState != nil {
		exit = cmd.ProcessState.ExitCode()
	}
	bashDebugf("exec done: elapsed=%s exit=%d ctxErr=%v runErr=%v", time.Since(started).Round(time.Millisecond), exit, cctx.Err(), err)
	body := string(out)
	// Whether the message will carry the output entire or a clipped view of it — measured, not
	// assumed. Two stages can clip: readHeadTail at capture (the file keeps everything) and
	// truncateOut for the message, each inserting its own omission marker. The capture reports its
	// own answer rather than being re-derived from a second stat, because a detached child can grow
	// the log after it was read and a bigger file is not evidence anything was elided.
	captureClipped := logPath != "" && !whole
	if ptyIgnored {
		body += "\n[note: `pty` only applies with background=true, so this command ran on a plain pipe. " +
			"If it needs a real terminal (an ssh password prompt, a serial login, a curses UI), re-launch " +
			"it with background:true AND pty:true and drive it with bash_input/bash_output.]"
	}
	if cctx.Err() == context.DeadlineExceeded {
		// The partial log is the most useful thing a killed build leaves, so name it here too.
		body += "\n" + timedOutNote(timeout, int(a.Timeout))
		shown := truncateOut(body)
		return errResult("", outputLine(logPath, captureClipped || len(shown) != len(body), true)+shown), nil
	}
	if err != nil && exit == 0 {
		// Launch failure (command not found, etc.) rather than non-zero exit.
		return errResult("", err.Error()+"\n"+truncateOut(body)), nil
	}
	disp := truncateOut(body)
	dispClipped := captureClipped || len(disp) != len(body)
	// A command that returns exit 0 while its output carries a real crash/traceback is
	// almost always one whose failing exit code was swallowed by a `|| echo`/`|| true`
	// tail. Both the model (which read the "exit 0" as a pass) and the council (which sees
	// this result marked [ok]) would otherwise rubber-stamp it. Annotate — never reclassify
	// — right after the status line so the note sits at the head, where the council's
	// head-clip and the model both see it. Flag-gated for A/B isolation.
	// magi captured the per-stage statuses: the notes that exist to say "this exit may not be the
	// head's" have nothing left to add, because the statuses themselves are about to be stated.
	stagesKnown := len(stages) > 1
	if note := pipeStageNote(exit, stages); note != "" {
		disp = note + "\n" + disp
	}
	// Binary output is not text, and saying so is a fact magi already knows how to measure — the
	// read tool refuses a binary file outright on the same predicate. bash cannot refuse (a command
	// may legitimately emit bytes, and the capture file must keep them either way), but it can stop
	// handing the model a wall of them unlabelled. Observed live (sqlite-with-gcov, 2026-07-30):
	// `cat /app/sqlite/bin/sqlite3 | head -5` put 9225 bytes of ELF header into the context with
	// nothing to say what it was. Named, the model can reach for `file`, `xxd`, or `strings`
	// instead of trying to read it.
	if isBinary(out) {
		disp = binaryOutputNote(len(out), logPath) + "\n" + disp
	}
	// The annotators, in precedence order, FIRST match wins: each says one thing about how this
	// command's status should be read, and stacking several on one result would bury the sharpest
	// under the vaguest. It is a list rather than an if/else ladder because the order IS the policy
	// — reading it should not mean reading eight nested branches — and because each entry can then
	// be exercised on its own.
	if bodyscanEnabled() {
		for _, annotate := range statusAnnotators {
			if note := annotate(exit, a.Command, disp, env.SessionID, stagesKnown); note != "" {
				disp = note + "\n" + disp
				break
			}
		}
	}
	// A tty-gated command (ssh, serial console) run in the foreground gets no controlling
	// terminal (detachTTY), so its /dev/tty prompt fails — steer to the interactive pty
	// path. Only on failure: a key-auth `ssh host cmd` that succeeded needs no tty.
	if exit != 0 {
		if note := ptyNeededNote(a.Command, false); note != "" {
			disp = note + "\n" + disp
		}
	}
	appendRunIndex(env.ScratchLogs, exit, a.Command, logPath)
	res := okText("", fmt.Sprintf("exit %d\n%s%s", exit, outputLine(logPath, dispClipped, false), disp))
	res.IsError = exit != 0
	return res, nil
}

// appendRunIndex records one finished command in the turn's index: when, what came back, where the
// full output is, and what ran. It is what makes the scratch legible — `ls` shows a pile of logs,
// and this says which is which without opening any of them.
//
// It is also the turn's execution history in a form that survives context compaction, which the
// conversation does not: the transcript is folded as the turn grows, and after that neither the
// agent nor a post-mortem can answer "was this already run, and what happened" from anything but
// the index.
//
// One short line, opened O_APPEND per call, because children of the same turn write here
// concurrently. Best-effort throughout: a failure here must never be the reason a command's result
// is lost.
func appendRunIndex(logsDir string, exit int, command, logPath string) {
	if logsDir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(logsDir, "index.tsv"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	// Tabs separate the fields, so the command — the only field that can contain anything — goes
	// last and is flattened: a newline inside it would otherwise start a row that is not one.
	flat := strings.Join(strings.Fields(command), " ")
	fmt.Fprintf(f, "%s\t%d\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), exit,
		filepath.Base(logPath), clipRunes(flat, 400))
}

// clipRunes bounds a field to n runes, cutting on a rune boundary so the row stays valid UTF-8.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// stripBackgroundArtifacts removes a trailing `&` and a leading `nohup` from a background-launched
// command: background=true already persists it as its own job, so those only make the job's shell
// exit immediately (see the background branch). Conservative — a SINGLE trailing `&` (never `&&`)
// and a leading `nohup ` word. Returns the cleaned command and whether anything was stripped.
func stripBackgroundArtifacts(command string) (string, bool) {
	c := strings.TrimSpace(command)
	if rest, ok := strings.CutPrefix(c, "nohup "); ok {
		c = strings.TrimSpace(rest)
	}
	if strings.HasSuffix(c, "&") && !strings.HasSuffix(c, "&&") {
		c = strings.TrimSpace(strings.TrimSuffix(c, "&"))
	}
	if c == "" || c == strings.TrimSpace(command) {
		return command, false
	}
	return c, true
}

// armCancel makes a timed-out or cancelled command terminate its WHOLE process tree and
// guarantees cmd.Wait returns. os/exec's default cancel kills only the launched process, so a
// surviving grandchild that inherited the output handle can wedge Wait — observed on Windows,
// where every command (not just ssh) hung well past its timeout. killCmdTree is the platform
// tree kill; WaitDelay is the backstop that force-returns Wait if that kill is slow or partial.
// Set per-cmd (captures this exact *exec.Cmd), so the retry path arms independently.
func armCancel(cmd *exec.Cmd) {
	cmd.WaitDelay = 3 * time.Second
	cmd.Cancel = func() error {
		pid := -1
		if cmd.Process != nil {
			pid = cmd.Process.Pid
		}
		err := killCmdTree(cmd)
		bashDebugf("cancel fired: kill tree pid=%d err=%v", pid, err)
		return err
	}
}

// bashDebugf appends a diagnostic line to a FILE when MAGI_BASH_DEBUG is set. It writes to a file
// — not stderr — because in TUI mode the alternate screen swallows stderr, so the trace never
// appeared. The env value is the log path; a bare "1"/"true"/"on"/"yes" defaults to
// magi-bash-debug.log in the working directory. The trace (exec start → started pid → cancel
// fired/taskkill result → wait returned → exec done) localizes the Windows timeout hang: a
// "started" with no "wait returned" means Wait itself is stuck, a "cancel fired" whose taskkill
// errored points at the kill, and no "cancel fired" at all means the deadline never triggered.
// Off (empty env) by default, so normal and bench runs stay silent.
func bashDebugf(format string, args ...any) {
	v := strings.TrimSpace(os.Getenv("MAGI_BASH_DEBUG"))
	if v == "" {
		return
	}
	path := v
	switch strings.ToLower(v) {
	case "1", "true", "on", "yes":
		path = "magi-bash-debug.log"
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[bash-debug %s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// dbgClip collapses a command to a single bounded line for a debug log.
func dbgClip(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\n", " "), "\r", " ")
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// runCapture runs cmd with its combined stdout/stderr sent to a temp FILE, then
// returns the captured bytes and the run error. Unlike cmd.CombinedOutput (which
// wires stdout/stderr to an os.Pipe), a file lets a command that backgrounds a child
// with `&` hand that child a plain file fd: the child is not tethered to a pipe whose
// read end our Wait would close, so it survives this call — and magi's own exit —
// instead of dying by SIGPIPE. sh exiting also returns Wait immediately (no pipe to
// drain), so `server &` no longer blocks on WaitDelay. On temp-file failure it falls
// back to the in-memory CombinedOutput path so the tool never breaks outright.
func runCapture(cmd *exec.Cmd, logsDir string) (out []byte, logPath string, whole bool, err error) {
	// The child inherits our console where the platform gives us no way to detach it (Windows), so
	// an interactive program can reconfigure the terminal the UI is drawn on and — killed on
	// timeout — never put it back. Snapshot around the whole run, including the fallback path.
	defer guardConsole()()
	// With a turn scratch, the capture file is KEPT and named back to the caller: the output then
	// outlives the call, so the elided middle is still on disk and a later step can grep the part
	// it needs. Without one, it is a temp file deleted on return, exactly as before. CreateTemp is
	// what makes the name unique with no shared counter — children of the same turn write here
	// concurrently.
	dir, keep := logsDir, logsDir != ""
	f, err := os.CreateTemp(dir, "magi-bash-*.log")
	if err != nil {
		if keep { // the scratch went away under us — fall back to a temp file rather than lose the run
			if f, err = os.CreateTemp("", "magi-bash-*.log"); err == nil {
				keep = false
			}
		}
		if err != nil {
			out, werr := cmd.CombinedOutput()
			// Unbounded, so it is everything the command wrote — and with no capture file there
			// is no line to say so on.
			return out, "", true, werr
		}
	}
	name := f.Name()
	if !keep {
		defer os.Remove(name)
	}
	defer f.Close()
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		bashDebugf("start failed: %v", err)
		return nil, "", false, err
	}
	bashDebugf("started pid=%d — waiting", cmd.Process.Pid)
	werr := cmd.Wait()
	bashDebugf("wait returned pid=%d err=%v", cmd.Process.Pid, werr)
	// Read whatever was flushed by the time the shell exited, bounded to captureCap so a
	// command that emits hundreds of MB (`cat huge`, a runaway build) can't buffer it all
	// into memory — we keep the head and the tail (where errors and final status usually
	// are) and elide the middle. A surviving `&` child keeps writing to its own fd on the
	// (now unlinked) inode; that later output is intentionally not captured here. (I1)
	data, whole := readHeadTail(name, captureCap)
	if !keep {
		name = ""
	}
	return data, name, whole, werr
}

// captureCap bounds how much of a command's output runCapture retains in memory. It sits
// above truncateOut's display cap so the head+tail context survives into the display
// truncation, while keeping the buffer to a fixed few hundred KB regardless of output size.
const captureCap = 256 << 10

// readHeadTail returns the file's full content when it fits in cap, else the first and last
// cap/2 bytes joined by an elision marker. Bounds memory for pathologically large output
// while preserving the tail — a truncated build log's error is almost always at the end, so
// head-only capture would drop exactly the useful part.
// whole reports that the returned bytes ARE the file, not a view of it — measured against the same
// stat the read was decided by. The caller cannot re-derive this afterwards: a `&` child that
// outlives the shell keeps writing to the log (see I1), so a later stat can be larger than what was
// read here with nothing having been elided.
func readHeadTail(path string, cap int64) (data []byte, whole bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, false
	}
	if fi.Size() <= cap {
		b, _ := io.ReadAll(f)
		return b, true
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
	return append(out, tail...), false
}

// shell returns the platform shell invocation for a command string.
//
// bash when the machine has it, /bin/sh otherwise. On Debian and Ubuntu — which is what the
// containers are — /bin/sh is dash, and a model writing the bash it writes everywhere else gets a
// syntax error for `[[ ]]`, `source`, arrays, or `${var,,}`: a failure that belongs to the shell
// choice, not to the work. dash also has no PIPESTATUS, which is the only way to learn what the
// left side of `make … | tail` actually exited with.
//
// Nothing else changes: no `pipefail`, no `errexit`. A command's exit status is what it always was,
// because the agent reads that number and a shell that quietly redefines it would be lying to it.
func shell(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-NoProfile", "-Command", command}
	}
	return unixShell(), []string{"-c", command}
}

// unixShellPath is resolved once: the lookup is a stat, and the answer cannot change under a
// running process in any way that matters.
var unixShellPath = sync.OnceValue(func() string {
	for _, p := range []string{"/bin/bash", "/usr/bin/bash"} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return "/bin/sh"
})

func unixShell() string { return unixShellPath() }

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

// scratchEnv points the command's TMPDIR at the turn's scratch, so everything that ASKS for a temp
// path — mktemp, python's tempfile, a compiler's intermediates — writes outside the deliverable
// tree with no model awareness at all. Observed without it: an agent copying its own working files
// into the workspace it was being graded on (`cp /app/input.csv /app/test_input.csv`), left behind
// after the run. Returns nil (inherit magi's environment, the previous behavior) when there is no
// scratch, so this can never be the reason a command fails to start.
func scratchEnv(tmp string) []string {
	if tmp == "" {
		return nil
	}
	out := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "TMPDIR="), strings.HasPrefix(kv, "TMP="), strings.HasPrefix(kv, "TEMP="):
		default:
			out = append(out, kv)
		}
	}
	return append(out, "TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)
}

// outputLine names the file the command's full output was captured to, when the turn kept one, and
// says whether the message above it is that output entire or a clipped view of it. The file is
// always everything, so a later step greps the part it needs instead of re-running the command.
//
// It used to say "this message shows the head and tail" on every result, including the ones it had
// shown whole. Observed live (fix-ocaml-gc, 2026-07-30): two `grep -n "^#define Make_header" …`
// runs, both exit 1, both answered "0 bytes — the full output; this message shows the head and
// tail" — a head-and-tail presentation of nothing, announcing an elision that had not happened. An
// agent told the middle was withheld has reason to go looking for what is not there; the second of
// those two calls was the same command again. Clipping is measured by the caller and marked in the
// body where it occurs, so this states what that measurement found.
func outputLine(path string, omitted, killed bool) string {
	if path == "" {
		return ""
	}
	size := int64(-1)
	if fi, err := os.Stat(path); err == nil {
		size = fi.Size()
	}
	switch {
	case size < 0:
		return "output: " + path + "\n"
	case size == 0 && killed:
		// An empty capture from a command that was KILLED says nothing about what the command
		// produced — only that nothing had reached the file by the time it died. The difference is
		// the whole story when the output went through a pager: observed live,
		// `make world.opt -j4 2>&1 | tail -50` hit the 120s limit, and `tail` holds everything
		// until its input ends, so the log was empty for a build that had been talking for two
		// minutes. The timeout note already refuses to read the missing exit as failure; the
		// missing output gets the same treatment.
		return fmt.Sprintf("output: %s (the capture is empty — the command was killed at the "+
			"timeout, so this does not say it wrote nothing)\n", path)
	case size == 0:
		return fmt.Sprintf("output: %s (the command wrote nothing — the file is empty)\n", path)
	case omitted:
		return fmt.Sprintf("output: %s (%d bytes — the head and tail are above, the file has all of it)\n", path, size)
	}
	return fmt.Sprintf("output: %s (%d bytes — all of it is above)\n", path, size)
}
