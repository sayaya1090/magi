package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Background ("detached") commands let the agent start a long-running process —
// a dev server, a file watcher, a slow test/build — and keep working while it runs,
// polling its output with bash_output and stopping it with bash_kill. The synchronous
// bash tool blocks the whole turn until the command exits, so it can't host these.
//
// Two design points keep a background process alive independent of magi:
//   - Its stdout/stderr go to a real file, NOT an in-memory io.Writer. An io.Writer
//     stdout makes os/exec insert an os.Pipe whose read end closes when magi exits,
//     which kills the child with SIGPIPE on its next write. A file has no such pipe,
//     so the process survives magi's own exit — required for "start a server, then
//     grade it" bench tasks where the grader runs after magi returns.
//   - It runs under Setsid (its own session / process group, no controlling tty), so
//     a signal directed at magi's group doesn't cascade into it.

// maxBgBuf bounds one bash_output read. It is kept at/under truncateOut's display
// cap on purpose: the reader advances the consumed offset by exactly the bytes it
// returns, so if this were larger than what truncateOut shows, the excess would be
// skipped past and never surface to the agent. A large burst is paged out across
// successive bash_output calls instead of being silently dropped.
const maxBgBuf = 30 * 1024

// hardLogCap bounds a background command's log file on disk. The child writes to
// the file directly (that decoupling is what keeps it alive past magi's exit —
// see the package doc), so magi can't intercept individual writes; instead, when
// the file grows past this cap it is truncated (rotateIfHuge) and the reader is
// reset to the fresh start. Older un-drained output is dropped — an acceptable
// safety valve for a runaway logger (a dev server logging every request), which
// is what would otherwise fill /tmp over a long session.
const hardLogCap = 8 << 20 // 8 MiB

// rotateIfHuge truncates p's log to zero when it exceeds hardLogCap and rewinds
// the read offset to 0, returning the number of bytes dropped (0 if not rotated).
// Safe because the log fd is O_APPEND: after truncation the child's next write
// lands at offset 0, so no sparse hole forms and absolute offsets restart cleanly.
func rotateIfHuge(p *bgProc) int64 {
	fi, err := os.Stat(p.logPath)
	if err != nil || fi.Size() < hardLogCap {
		return 0
	}
	dropped := fi.Size()
	if err := os.Truncate(p.logPath, 0); err != nil {
		return 0
	}
	p.mu.Lock()
	p.read = 0
	p.mu.Unlock()
	return dropped
}

// readLogSince returns up to maxBgBuf bytes of a background command's log file
// starting at absolute offset `since`, plus the new offset. The file is trimmed
// only by rotateIfHuge (disk cap); within a rotation window nothing is dropped.
func readLogSince(path string, since int) (out string, next int) {
	f, err := os.Open(path)
	if err != nil {
		return "", since
	}
	defer f.Close()
	if _, err := f.Seek(int64(since), io.SeekStart); err != nil {
		return "", since
	}
	b, _ := io.ReadAll(io.LimitReader(f, maxBgBuf))
	return string(b), since + len(b)
}

// bgProc is one running (or finished) background command.
type bgProc struct {
	id      string
	command string
	logPath string // file receiving the process's combined stdout/stderr
	cancel  context.CancelFunc
	started time.Time
	pid     int // process-group leader pid (Setsid => pgid == pid); used by killGroup

	pty bool // process was started on a pseudo-terminal (stdin is the PTY master)

	mu     sync.Mutex
	stdin  io.WriteCloser // stdin sink for bash_input: a pipe, or the PTY master when pty; closed on exit
	read   int            // absolute offset the agent has consumed up to
	done   bool
	killed bool // bash_kill was issued; status reads "killed" until the reaper sets done
	exit   int
	// psPath is where the wrapped shell writes the last pipeline's per-stage statuses, and
	// stages is what it held once the job finished — so the status line can say which stage
	// really failed instead of reporting the pipeline's last exit alone.
	psPath  string
	stages  []int
	claimed bool // this job's finished outcome has already been handed to ClaimBackgroundOutcome
}

// bgManager is the process-global registry of background commands. Tools are
// stateless, so the registry lives here (processes are OS-global anyway).
type bgManager struct {
	mu    sync.Mutex
	procs map[string]*bgProc
	seq   int
}

var bg = &bgManager{procs: map[string]*bgProc{}}

func (m *bgManager) start(workdir, tmpDir string, sb port.SandboxSpec, command string, usePTY bool) (*bgProc, error) {
	name, args := shell(command)
	if argv, wrapped := sandboxArgv(sb, command); wrapped {
		name, args = argv[0], argv[1:]
	}
	// The exit a pipeline reports is its LAST stage's, and a background job reports that exit
	// with nothing beside it — `[bg_1 exited 0]` for a `make … | tail` whose build died. Observed
	// live: a three-hour run polled a backgrounded build, was told it exited 0, and the failure
	// (`make: *** Error 2`) sat in the output the model had to read past the header to find. The
	// foreground path has said which stage really failed since PIPESTATUS went in; ask for the
	// same thing here.
	name, args, psPath := withPipeStatus(name, args, tmpDir)
	// Combined stdout+stderr go to a real file so the process is not tethered to an
	// os.Pipe that would close (and SIGPIPE the child) when magi exits. The file is
	// opened O_APPEND so that when rotateIfHuge truncates it to bound disk, the
	// child's next write lands cleanly at offset 0 (append seeks to EOF) instead of
	// leaving a sparse hole full of NUL bytes at its old, now-past-EOF fd offset.
	tmp, err := os.CreateTemp("", "magi-bg-*.log")
	if err != nil {
		return nil, err
	}
	logName := tmp.Name()
	_ = tmp.Close()
	f, err := os.OpenFile(logName, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		_ = os.Remove(logName)
		return nil, err
	}
	pctx, cancel := context.WithCancel(context.Background()) // outlives the tool call
	cmd := exec.CommandContext(pctx, name, args...)
	cmd.Dir = workdir
	logPath := f.Name()

	var stdin io.WriteCloser
	if usePTY {
		if !ptySupported {
			_ = f.Close()
			_ = os.Remove(logPath)
			cancel()
			return nil, errors.New("pty is not supported on this platform")
		}
		// Opt-in pseudo-terminal: the child gets a real controlling tty (ptyStart sets
		// Setsid+Setctty), so ssh's /dev/tty password prompt and a serial getty login
		// work and can be driven with bash_input. A terminal merges stdout+stderr into
		// one stream, which a goroutine copies to the same log file the pipe path writes,
		// so bash_output reads it identically. In PTY mode the copy goroutine OWNS f and
		// the master and closes both when the slave EOFs (process exit) — unlike the pipe
		// path, which closes f right after Start because the child inherited its own fd.
		ptmx, err := ptyStart(cmd)
		if err != nil {
			_ = f.Close()
			_ = os.Remove(logPath)
			cancel()
			return nil, err
		}
		stdin = ptmx
		go func() {
			_, _ = io.Copy(f, ptmx)
			_ = ptmx.Close()
			_ = f.Close()
		}()
	} else {
		// New session: own process group, no controlling terminal. Detaches the child
		// from magi's group and makes killGroup(pid) reach any workers it forks.
		cmd.SysProcAttr = detachTTY(sandboxProcAttr(sb))
		cmd.Stdout, cmd.Stderr = f, f
		// Keep stdin open so bash_input can drive an interactive process (REPL, line
		// debugger). Must be obtained BEFORE Start; closed when the process exits.
		sp, err := cmd.StdinPipe()
		if err != nil {
			_ = f.Close()
			_ = os.Remove(logPath)
			cancel()
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			_ = sp.Close()
			_ = f.Close()
			_ = os.Remove(logPath)
			cancel()
			return nil, err
		}
		stdin = sp
		// The child inherited its own fd for the log at fork; the parent's handle is no
		// longer needed (reads reopen the path read-only).
		_ = f.Close()
	}

	m.mu.Lock()
	m.seq++
	p := &bgProc{id: fmt.Sprintf("bg_%d", m.seq), command: command, logPath: logPath, stdin: stdin, cancel: cancel, started: time.Now(), pid: cmd.Process.Pid, pty: usePTY, psPath: psPath}
	m.procs[p.id] = p
	m.pruneLocked()
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		exit := 0
		if cmd.ProcessState != nil {
			exit = cmd.ProcessState.ExitCode()
		} else if err != nil {
			exit = -1
		}
		// Read the per-stage statuses BEFORE marking done, so a poll that sees `done` always sees
		// them too — otherwise the first poll after exit could report the pipeline's exit alone.
		var stages []int
		if psPath != "" {
			stages = readPipeStatus(psPath)
			_ = os.Remove(psPath)
		}
		p.mu.Lock()
		p.done, p.exit, p.stages = true, exit, stages
		p.stdin = nil // no more input accepted once exited
		p.mu.Unlock()
		// Pipe mode: release the stdin pipe here. PTY mode: the copy goroutine closes the
		// master (== stdin) and the log file when the slave EOFs, so don't double-close.
		if !usePTY {
			_ = stdin.Close()
		}
		cancel() // release the process context now that it has exited
	}()
	return p, nil
}

// maxBgProcs caps the registry; finished processes beyond it are pruned (oldest
// first) so a long session can't leak unbounded *bgProc/log-file entries. Running
// processes are never pruned.
const maxBgProcs = 32

// pruneLocked drops the oldest finished processes when the registry is over cap,
// removing each dropped process's log file. Caller holds m.mu.
func (m *bgManager) pruneLocked() {
	if len(m.procs) <= maxBgProcs {
		return
	}
	type ent struct {
		id  string
		seq int
	}
	var done []ent
	for id, p := range m.procs {
		p.mu.Lock()
		fin := p.done
		p.mu.Unlock()
		if fin {
			n := 0
			fmt.Sscanf(id, "bg_%d", &n)
			done = append(done, ent{id, n})
		}
	}
	// Oldest first.
	for i := 0; i < len(done); i++ {
		for j := i + 1; j < len(done); j++ {
			if done[j].seq < done[i].seq {
				done[i], done[j] = done[j], done[i]
			}
		}
	}
	for i := 0; i < len(done) && len(m.procs) > maxBgProcs; i++ {
		if p := m.procs[done[i].id]; p != nil {
			_ = os.Remove(p.logPath)
		}
		delete(m.procs, done[i].id)
	}
}

func (m *bgManager) get(id string) *bgProc {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procs[id]
}

// ClaimBackgroundOutcome hands back a finished background job's own command text and exit code,
// exactly once per job, so the orchestrator can record what that command actually did.
//
// It exists because a background job's START and its RESULT arrive as two different tool calls. The
// start returns "started background command bg_N" — a success that says only that a process now
// exists — and everything that judges whether a build/test converged was reading THAT as the
// command's outcome, so a build was booked as a pass before it had run a single rule, while its
// real exit arrived later through bash_output and was recorded nowhere at all. The one-shot claim is
// what keeps a repeated poll of the same finished job from counting its failure again and again.
//
// A KILLED job reports nothing: the agent stopped it, so its exit says nothing about the work. A job
// the agent never polls after it exits is likewise never claimed — silence is the safe direction.
func ClaimBackgroundOutcome(id string) (command string, exit int, ok bool) {
	p := bg.get(id)
	if p == nil {
		return "", 0, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.done || p.killed || p.claimed {
		return "", 0, false
	}
	p.claimed = true
	return p.command, p.exit, true
}

// KillAll terminates every still-running background command and its process group.
func (m *bgManager) KillAll() {
	m.mu.Lock()
	var running []*bgProc
	for _, p := range m.procs {
		p.mu.Lock()
		fin := p.done
		p.mu.Unlock()
		if !fin {
			running = append(running, p)
		}
	}
	m.mu.Unlock()
	for _, p := range running {
		_ = killGroup(p.pid)
		p.cancel()
	}
}

// KillBackgroundProcesses terminates all still-running background commands and their
// process groups. Call it on INTERACTIVE shutdown so dev servers don't leak; headless
// (-p) runs deliberately skip it, so a launched server survives for post-run grading.
func KillBackgroundProcesses() { bg.KillAll() }

// staleTempLogAge is how old a leftover magi temp log must be before startup sweep
// reclaims it — well past any plausible live use, so a background server still
// writing to its log (a survived headless run) is never touched.
const staleTempLogAge = 24 * time.Hour

// SweepStaleTempLogs removes magi's own leftover temp log files (magi-bg-*.log,
// magi-bash-*.log) older than staleTempLogAge from the temp dir. It reclaims files
// that escaped cleanup: a runCapture temp that a surviving `&` child kept open past
// process exit (notably on Windows, where an open handle can block os.Remove), and
// background logs orphaned when a headless run left a server alive. Best-effort and
// age-gated, so it never races a live process. Call once at startup.
func SweepStaleTempLogs() {
	dir := os.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleTempLogAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "magi-bg-") && !strings.HasPrefix(name, "magi-bash-") {
			continue
		}
		if !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// status returns a one-line status header for a process.
func (p *bgProc) status() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		h := fmt.Sprintf("[%s exited %d]", p.id, p.exit)
		// A pipeline reports its LAST stage's exit, so `make … | tail` says 0 for a build that
		// died. Say which stage really failed, in the header the model reads first.
		if note := pipeStageNote(p.command, p.exit, p.stages); note != "" {
			h += " " + note
		}
		return h
	}
	if p.killed {
		// Reflect the kill immediately; the reaper goroutine flips done shortly after.
		return fmt.Sprintf("[%s killed]", p.id)
	}
	return fmt.Sprintf("[%s running %s]", p.id, time.Since(p.started).Round(time.Second))
}

// BashOutput returns output produced by a background command since the last read.
type BashOutput struct{}

func (BashOutput) Name() string { return "bash_output" }
func (BashOutput) Description() string {
	return "Read new output from a background command started with bash (background=true), since the last read. Returns a status header ([id running …] or [id exited N]) and any new output."
}
func (BashOutput) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"the background process id, e.g. bg_1"}},"required":["id"]}`)
}
func (BashOutput) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if a.ID == "" {
		return errResult("", "id is required"), nil
	}
	p := bg.get(a.ID)
	if p == nil {
		return errResult("", "no such background process: "+a.ID), nil
	}
	p.mu.Lock()
	since := p.read
	p.mu.Unlock()
	text, next := readLogSince(p.logPath, since)
	p.mu.Lock()
	p.read = next
	p.mu.Unlock()
	body := p.status()
	if text != "" {
		body += "\n" + truncateOut(text)
	}
	// A full-cap read almost certainly left more buffered; tell the agent to page on.
	if len(text) >= maxBgBuf {
		body += "\n…(more output buffered — call bash_output again)"
	}
	// Bound the log on disk: if it has grown past the cap, drop it and restart so a
	// chatty long-lived process can't fill /tmp over a long session.
	if mb := rotateIfHuge(p) >> 20; mb > 0 {
		body += fmt.Sprintf("\n…(log exceeded %d MiB — rotated, older output dropped)", hardLogCap>>20)
	}
	return okText("", body), nil
}

// BashKill terminates a background command.
type BashKill struct{}

func (BashKill) Name() string { return "bash_kill" }
func (BashKill) Description() string {
	return "Stop a background command (bash background=true) by its id. Default is a hard stop. Set signal=\"int\" (Ctrl-C) or \"term\" for a GRACEFUL interrupt: the process runs cleanup handlers and prints what it knows — read it with bash_output after. Prefer signal=\"int\" first on a hung test you still want answers from; hard-kill only when it ignores the interrupt. (Unix only; Windows uses the default hard stop.)"
}
func (BashKill) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"the background process id, e.g. bg_1"},"signal":{"type":"string","enum":["int","term"],"description":"optional graceful signal (Ctrl-C equivalent); omit for the default hard stop"}},"required":["id"]}`)
}
func (BashKill) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a struct {
		ID     string `json:"id"`
		Signal string `json:"signal"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if a.ID == "" {
		return errResult("", "id is required"), nil
	}
	p := bg.get(a.ID)
	if p == nil {
		return errResult("", "no such background process: "+a.ID), nil
	}
	if sig := strings.ToLower(strings.TrimSpace(a.Signal)); sig == "int" || sig == "term" {
		// Graceful interrupt (the Ctrl-C affordance): deliver the signal and DON'T
		// tear anything down — the process may handle it, print its cleanup, and
		// exit on its own; the agent reads the outcome with bash_output. If it
		// ignores the signal, a plain bash_kill still hard-stops it.
		if err := signalGroup(p.pid, sig); err != nil {
			return errResult("", "signal failed: "+err.Error()), nil
		}
		return okText("", "sent SIG"+strings.ToUpper(sig)+" to "+a.ID+" — check bash_output for its cleanup output; run bash_kill without signal if it doesn't exit"), nil
	}
	// Mark killed synchronously so an immediately following bash_output reports
	// "[id killed]" instead of a stale "[id running]" until the reaper sets done.
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	// Kill the whole process group (workers the command forked), then cancel the
	// context so exec releases the leader.
	_ = killGroup(p.pid)
	p.cancel()
	return okText("", "killed "+a.ID), nil
}

// BashInput sends input to a background command's stdin, so the agent can drive a
// line-oriented interactive program (a REPL, a line debugger) together with
// bash_output. It's a plain pipe, not a TTY — programs that require a terminal
// (full-screen/curses UIs, password prompts) won't work.
type BashInput struct{}

func (BashInput) Name() string { return "bash_input" }
func (BashInput) Description() string {
	return "Send input to the stdin of a background command (bash background=true), then read its reply with bash_output. Drives line-oriented interactive programs — a REPL (python3, psql), a line debugger (gdb, pdb). A trailing newline is appended by default; set newline=false for raw/partial input, or eof=true to close stdin (some tools only flush/exit on EOF). This is a pipe, not a terminal — programs needing a TTY (curses UIs, password prompts) won't work."
}
func (BashInput) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"the background process id, e.g. bg_1"},"input":{"type":"string","description":"text to write to stdin"},"newline":{"type":"boolean","description":"append a newline (default true)"},"eof":{"type":"boolean","description":"close stdin instead of writing (end-of-input)"}},"required":["id"]}`)
}
func (BashInput) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a struct {
		ID      string   `json:"id"`
		Input   string   `json:"input"`
		Newline *bool    `json:"newline"`
		EOF     FlexBool `json:"eof"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if a.ID == "" {
		return errResult("", "id is required"), nil
	}
	p := bg.get(a.ID)
	if p == nil {
		return errResult("", "no such background process: "+a.ID), nil
	}
	p.mu.Lock()
	done := p.done
	w := p.stdin
	p.mu.Unlock()
	if done || w == nil {
		return errResult("", "background process "+a.ID+" has exited; cannot send input"), nil
	}
	if a.EOF {
		if err := w.Close(); err != nil {
			return errResult("", "close stdin of "+a.ID+": "+err.Error()), nil
		}
		return okText("", "closed stdin of "+a.ID), nil
	}
	data := a.Input
	if a.Newline == nil || *a.Newline {
		data += "\n"
	}
	if _, err := io.WriteString(w, data); err != nil {
		return errResult("", "write to "+a.ID+": "+err.Error()), nil
	}
	return okText("", "sent input to "+a.ID), nil
}

// A background job is the one thing magi runs that nobody could watch. The agent polls it with
// bash_output; the person at the terminal saw a line saying a process had started and nothing after
// that — not its output, not when it ended, not whether it ended badly. The pane machinery that used
// to show subagents is exactly the shape this needs, so these expose the registry to a viewer.
//
// A viewer must not consume: bash_output advances p.read, and anything that shares that offset would
// take output the agent has not seen yet. TailBackgroundJob reads the file's own tail and touches no
// offset at all, so watching changes nothing about what the agent reads.

// BackgroundJob is one background command as an observer sees it.
type BackgroundJob struct {
	ID      string
	Command string
	Running bool
	Killed  bool
	Exit    int
	Started time.Time
}

// ListBackgroundJobs returns every background job this process still holds, oldest first. The
// registry is process-global (processes are), so this is not scoped to a session.
func ListBackgroundJobs() []BackgroundJob {
	bg.mu.Lock()
	procs := make([]*bgProc, 0, len(bg.procs))
	for _, p := range bg.procs {
		procs = append(procs, p)
	}
	bg.mu.Unlock()

	out := make([]BackgroundJob, 0, len(procs))
	for _, p := range procs {
		p.mu.Lock()
		out = append(out, BackgroundJob{
			ID: p.id, Command: p.command, Running: !p.done, Killed: p.killed,
			Exit: p.exit, Started: p.started,
		})
		p.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool {
		var a, b int
		fmt.Sscanf(out[i].ID, "bg_%d", &a)
		fmt.Sscanf(out[j].ID, "bg_%d", &b)
		return a < b
	})
	return out
}

// TailBackgroundJob returns the last max bytes of a job's log, for display. It never advances the
// agent's read offset — a viewer that consumed output would take it from the agent.
func TailBackgroundJob(id string, max int) string {
	p := bg.get(id)
	if p == nil || max <= 0 {
		return ""
	}
	f, err := os.Open(p.logPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	at := int64(0)
	if fi.Size() > int64(max) {
		at = fi.Size() - int64(max)
	}
	if _, err := f.Seek(at, io.SeekStart); err != nil {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(f, int64(max)))
	s := string(b)
	if at > 0 { // started mid-line: drop the partial first line rather than show a fragment
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	return s
}

// KillBackgroundJob stops a job by id, the same way bash_kill does: mark it killed synchronously so
// the next status read says so, kill the whole process group (workers the command forked), then
// cancel the context so exec releases the leader. Reports whether a job with that id existed.
func KillBackgroundJob(id string) bool {
	p := bg.get(id)
	if p == nil {
		return false
	}
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	_ = killGroup(p.pid)
	p.cancel()
	return true
}
