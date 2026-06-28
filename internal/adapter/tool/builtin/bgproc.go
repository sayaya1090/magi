package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Background ("detached") commands let the agent start a long-running process —
// a dev server, a file watcher, a slow test/build — and keep working while it runs,
// polling its output with bash_output and stopping it with bash_kill. The synchronous
// bash tool blocks the whole turn until the command exits, so it can't host these.

const maxBgBuf = 256 * 1024 // per-process captured-output cap (keeps the tail)

// syncBuffer is an io.Writer that accumulates output under a lock, capped to the
// last maxBgBuf bytes, tracking total bytes ever written so reads can resume by
// absolute offset even after the front is trimmed.
type syncBuffer struct {
	mu    sync.Mutex
	buf   []byte
	total int
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	b.total += len(p)
	if len(b.buf) > maxBgBuf {
		b.buf = b.buf[len(b.buf)-maxBgBuf:]
	}
	return len(p), nil
}

// readSince returns output written after absolute offset `since`, the new offset,
// and whether some output before `since` was already dropped by the cap.
func (b *syncBuffer) readSince(since int) (out string, next int, dropped bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	start := b.total - len(b.buf) // absolute offset of buf[0]
	if since < start {
		since, dropped = start, true
	}
	idx := since - start
	if idx < 0 {
		idx = 0
	}
	return string(b.buf[idx:]), b.total, dropped
}

// bgProc is one running (or finished) background command.
type bgProc struct {
	id      string
	command string
	out     *syncBuffer
	cancel  context.CancelFunc
	started time.Time

	mu    sync.Mutex
	stdin io.WriteCloser // open pipe to the process's stdin (for bash_input); closed on exit
	read  int            // absolute offset the agent has consumed up to
	done  bool
	exit  int
}

// bgManager is the process-global registry of background commands. Tools are
// stateless, so the registry lives here (processes are OS-global anyway).
type bgManager struct {
	mu    sync.Mutex
	procs map[string]*bgProc
	seq   int
}

var bg = &bgManager{procs: map[string]*bgProc{}}

func (m *bgManager) start(workdir string, sb port.SandboxSpec, command string) (*bgProc, error) {
	name, args := shell(command)
	if argv, wrapped := sandboxArgv(sb, command); wrapped {
		name, args = argv[0], argv[1:]
	}
	pctx, cancel := context.WithCancel(context.Background()) // outlives the tool call
	cmd := exec.CommandContext(pctx, name, args...)
	cmd.Dir = workdir
	cmd.SysProcAttr = sandboxProcAttr(sb)
	out := &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	// Keep stdin open so bash_input can drive an interactive process (REPL, line
	// debugger). Must be obtained BEFORE Start; closed when the process exits.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return nil, err
	}

	m.mu.Lock()
	m.seq++
	p := &bgProc{id: fmt.Sprintf("bg_%d", m.seq), command: command, out: out, stdin: stdin, cancel: cancel, started: time.Now()}
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
		p.mu.Lock()
		p.done, p.exit = true, exit
		p.stdin = nil // no more input accepted once exited
		p.mu.Unlock()
		_ = stdin.Close() // release the pipe
		cancel()          // release the process context now that it has exited
	}()
	return p, nil
}

// maxBgProcs caps the registry; finished processes beyond it are pruned (oldest
// first) so a long session can't leak unbounded *bgProc/buffer entries. Running
// processes are never pruned.
const maxBgProcs = 32

// pruneLocked drops the oldest finished processes when the registry is over cap.
// Caller holds m.mu.
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
		delete(m.procs, done[i].id)
	}
}

func (m *bgManager) get(id string) *bgProc {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.procs[id]
}

// status returns a one-line status header for a process.
func (p *bgProc) status() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		return fmt.Sprintf("[%s exited %d]", p.id, p.exit)
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
	p := bg.get(a.ID)
	if p == nil {
		return errResult("", "no such background process: "+a.ID), nil
	}
	p.mu.Lock()
	since := p.read
	p.mu.Unlock()
	text, next, dropped := p.out.readSince(since)
	p.mu.Lock()
	p.read = next
	p.mu.Unlock()
	header := p.status()
	if dropped {
		header += " (earlier output dropped)"
	}
	body := header
	if text != "" {
		body += "\n" + truncateOut(text)
	}
	return okText("", body), nil
}

// BashKill terminates a background command.
type BashKill struct{}

func (BashKill) Name() string { return "bash_kill" }
func (BashKill) Description() string {
	return "Stop a background command started with bash (background=true), by its id."
}
func (BashKill) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"the background process id, e.g. bg_1"}},"required":["id"]}`)
}
func (BashKill) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	p := bg.get(a.ID)
	if p == nil {
		return errResult("", "no such background process: "+a.ID), nil
	}
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
	return "Send input to the stdin of a background command (started with bash background=true), then read its reply with bash_output. Drives line-oriented interactive programs — a REPL (python3, psql), a line debugger (gdb, pdb), etc. A trailing newline is appended by default (sends one line); set newline=false for raw/partial input, or eof=true to close stdin (signals end-of-input; some tools only flush/exit on EOF). This is a pipe, not a terminal: programs that require a TTY (full-screen/curses UIs, interactive password prompts) won't work."
}
func (BashInput) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"the background process id, e.g. bg_1"},"input":{"type":"string","description":"text to write to stdin"},"newline":{"type":"boolean","description":"append a newline (default true)"},"eof":{"type":"boolean","description":"close stdin instead of writing (end-of-input)"}},"required":["id"]}`)
}
func (BashInput) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a struct {
		ID      string `json:"id"`
		Input   string `json:"input"`
		Newline *bool  `json:"newline"`
		EOF     bool   `json:"eof"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
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
