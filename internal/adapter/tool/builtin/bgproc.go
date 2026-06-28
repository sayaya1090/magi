package builtin

import (
	"context"
	"encoding/json"
	"fmt"
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

	mu   sync.Mutex
	read int // absolute offset the agent has consumed up to
	done bool
	exit int
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
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}

	m.mu.Lock()
	m.seq++
	p := &bgProc{id: fmt.Sprintf("bg_%d", m.seq), command: command, out: out, cancel: cancel, started: time.Now()}
	m.procs[p.id] = p
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
		p.mu.Unlock()
	}()
	return p, nil
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
