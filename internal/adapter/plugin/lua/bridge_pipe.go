package lua

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// magi.pipe — magi.exec's living twin: a child that stays up between calls, with its stdin and
// stdout held open, so a plugin can hold a CONVERSATION with a subprocess instead of a
// transaction.
//
// It exists for one measured reason. The backend shims drive a coding CLI as a language model,
// and every one of them re-sends the whole conversation on every turn because a one-shot process
// cannot remember the last one. What that costs, measured on codex 0.147.0 (2026-08-20), same
// conversation, three turns:
//
//	one-shot `codex exec --ephemeral` ..... ~9,800 tokens a turn at full rate (55% cache hit)
//	one-shot `codex exec resume` .......... ~21,200 a turn — resume REPLAYS the transcript,
//	                                        so input grew 25,305 -> 50,640 -> 75,993
//	one live process, delta prompts ....... 527, then 544 (98% hit), input FLAT at ~28,550
//
// The last row is only reachable while the process stays up: the provider keys its prompt cache
// on a thread the CLI mints per run, so a fresh process is a cold key no matter how identical the
// bytes are. Nineteen times less billed input, for holding a pipe open.
//
// # Why this is not a new permission class
//
// pipe is gated on exec:<cmd> — the SAME permission magi.exec checks, deliberately not a new
// one. A plugin that may run `codex` with arguments it chooses, for minutes, already has the
// reach; what pipe adds is duration and interactivity, not a new thing to touch. Inventing
// pipe:<cmd> beside exec:<cmd> would put two names on one capability and make every manifest
// answer the same question twice.
//
// What the host owes in exchange is a child that cannot outlive its usefulness or eat the daemon:
//
//   - killed on close(), on plugin unload, and after an idle period with no traffic
//   - at most pipeMaxPerPlugin alive at once, so a loop that forgets to close cannot fork-bomb
//   - stdout drained continuously into a bounded queue — an unread pipe is a child that blocks,
//     and an unbounded one is a child that grows the daemon
//   - read() returns at its deadline instead of blocking forever: these calls run on a
//     magi.serve handler goroutine, and one that never returns is one request wedged for good
//   - the child never inherits the daemon's stdin, so it cannot reach the terminal a user is
//     looking at
//
// # Why it is not MCP
//
// The wire format on that pipe is the plugin's business. A shim speaking JSON-RPC to
// `codex mcp-server` over a pipe is NOT registering an MCP server: mcpMgr never learns of it, the
// tool list does not change, and the model is never told anything exists. Registering it properly
// would be the opposite of what a backend needs — a registered server's tools are offered TO the
// model, while a shim needs to call INTO the child from behind it.
const (
	pipeLineMax      = 1 << 20 // one line; a longer one is truncated rather than buffered forever
	pipeQueueMax     = 64      // lines held before the drain applies backpressure to the child
	pipeMaxPerPlugin = 4
	pipeIdleDefault  = 10 * time.Minute
	pipeReadDefault  = 60 * time.Second
	pipeStderrMax    = 64 << 10 // kept only to put a reason in the error when a child dies
)

// child is one live subprocess plus the goroutine draining its stdout.
type child struct {
	name  string
	cmd   *exec.Cmd
	stdin io.WriteCloser
	lines chan string
	done  chan struct{} // closed when stdout reaches EOF (the child is finishing or gone)

	mu     sync.Mutex
	closed bool
	stderr *lockedBuffer
	idle   time.Duration
	timer  *time.Timer
}

// lockedBuffer is the child's stderr, which two goroutines touch: os/exec writes to it as the
// child produces output, and deadReason reads it when a call finds the child gone. The plugin
// mutex cannot serve — os/exec knows nothing about it — so the buffer carries its own, which the
// race detector found the moment a test asked a dying child why it died.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Capped like every other captured stream here: a child that fails in a loop must not grow the
	// daemon. The count returned is the whole write, because a short count is an error to os/exec
	// and a truncated diagnostic is not a failure of the child.
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Close kills the child and releases the pipe. Safe to call twice — unload calls it on children a
// plugin already closed, and a double kill must not be an error anybody sees.
//
// No error, deliberately. There is no failure here a caller could act on: the pipe is going away,
// the process is being killed, and a second call is a no-op. Returning one would only produce
// discarded returns at every call site, which is the shape this tree treats as how a failure stops
// being one.
func (c *child) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	go func() { _ = c.cmd.Wait() }() // reap without blocking the caller on a slow exit
}

// touch restarts the idle countdown. Every read and write calls it, so "idle" means no traffic,
// not merely no writes: a plugin sitting in a long read is using the child, not forgetting it.
func (c *child) touch() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.timer == nil {
		return
	}
	c.timer.Reset(c.idle)
}

func (c *child) alive() bool {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// deadReason explains an exit for the error string a plugin sees. The stderr tail is what makes
// "the child is gone" actionable rather than a shrug.
func (c *child) deadReason() string {
	tail := strings.TrimSpace(c.stderr.String())
	if tail == "" {
		return "child exited"
	}
	if len(tail) > 400 {
		tail = tail[len(tail)-400:]
	}
	return "child exited: " + tail
}

// bridgePipe implements magi.pipe(cmd, args?, opts?) -> handle | (nil, err).
func (p *plugin) bridgePipe(L *lua.LState) int {
	cmdName := L.CheckString(1)
	if !p.perms.allowExec(cmdName) {
		return fail(L, "permission denied: exec:"+cmdName)
	}
	var args []string
	if tbl, ok := L.Get(2).(*lua.LTable); ok {
		tbl.ForEach(func(_, v lua.LValue) {
			if s, ok := v.(lua.LString); ok {
				args = append(args, string(s))
			}
		})
	}
	neutral, idle := false, pipeIdleDefault
	if opts, ok := L.Get(3).(*lua.LTable); ok {
		neutral = lua.LVAsBool(opts.RawGetString("neutral_dir"))
		if raw, ok := opts.RawGetString("idle").(lua.LString); ok {
			if d, err := time.ParseDuration(string(raw)); err == nil && d > 0 {
				idle = d
			}
		}
	}

	// No p.mu here, and none below: bridge calls reach this while the caller ALREADY holds it —
	// the entry script runs under the plugin lock (so a magi.serve handler cannot re-enter the
	// LState mid-setup), and every later path into Lua takes it too. Locking again is a deadlock,
	// which is why magi.serve appends to p.servers bare as well. The slice is only ever touched
	// from inside the Lua state, single-threaded by that same lock.
	live := 0
	for _, ch := range p.children {
		if ch.alive() {
			live++
		}
	}
	if live >= pipeMaxPerPlugin {
		return fail(L, fmt.Sprintf("pipe: %d children already alive (cap %d); close one first",
			live, pipeMaxPerPlugin))
	}

	// No CommandContext: a child bound to the ctx of whichever call happened to create it would
	// die when that call returned, which is the entire thing this is here to avoid. Its lifetime
	// is close(), unload, or the idle timer — all of them explicit.
	c := exec.Command(cmdName, args...)
	c.Dir = p.dir
	if p.host != nil && p.host.runtime.Workdir != "" {
		c.Dir = p.host.runtime.Workdir
	}
	if neutral {
		// Same bargain as magi.exec's: a CLI that walks up from its working directory bills for
		// what it finds. For a cached backend it is also stability — codex puts <cwd> in an
		// <environment_context> block INSIDE the cached prefix, so a directory that moves between
		// turns invalidates every token after it.
		if dir, err := p.neutralDir(); err == nil {
			c.Dir = dir
		}
	}
	stdin, err := c.StdinPipe()
	if err != nil {
		return fail(L, "pipe: stdin: "+err.Error())
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return fail(L, "pipe: stdout: "+err.Error())
	}
	errBuf := &lockedBuffer{max: pipeStderrMax}
	ch := &child{
		name: cmdName, cmd: c, stdin: stdin,
		lines: make(chan string, pipeQueueMax), done: make(chan struct{}),
		stderr: errBuf, idle: idle,
	}
	c.Stderr = errBuf
	// c.Stdin is the pipe above; stdin is NOT inherited, so the child cannot read the terminal.
	if err := c.Start(); err != nil {
		return fail(L, "pipe: "+err.Error())
	}
	ch.timer = time.AfterFunc(idle, func() {
		p.logf(fmt.Sprintf("pipe: %s idle for %s; closing", cmdName, idle))
		ch.Close()
	})

	// Drain stdout continuously. A pipe nobody reads fills, and a child whose pipe is full stops
	// — which looks exactly like a hung model. The queue is bounded, so a child that talks faster
	// than the plugin reads gets backpressure instead of the daemon getting a memory leak.
	go func() {
		defer close(ch.done)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64<<10), pipeLineMax)
		for sc.Scan() {
			select {
			case ch.lines <- sc.Text():
			case <-ch.done:
				return
			}
		}
	}()

	p.children = append(p.children, ch)

	L.Push(ch.handle(L))
	return 1
}

// handle builds the Lua object. A table of closures rather than a userdata metatable: the methods
// are few and fixed, and this keeps the bridge readable next to the rest of the file.
func (c *child) handle(L *lua.LState) *lua.LTable {
	t := L.NewTable()
	L.SetField(t, "pid", lua.LNumber(c.cmd.Process.Pid))
	L.SetField(t, "write", L.NewFunction(c.luaWrite))
	L.SetField(t, "read", L.NewFunction(c.luaRead))
	L.SetField(t, "alive", L.NewFunction(c.luaAlive))
	L.SetField(t, "close", L.NewFunction(c.luaClose))
	return t
}

// selfArg tolerates both call forms. `ch:write(s)` puts the handle at 1 and the string at 2;
// `ch.write(s)` puts the string at 1. Rejecting one of them would be a type error thrown from
// deep inside a shim on a syntax that reads correct, and the codebase has already paid for that
// lesson once with tool arguments.
func selfArg(L *lua.LState, n int) lua.LValue {
	if _, ok := L.Get(1).(*lua.LTable); ok {
		return L.Get(n + 1)
	}
	return L.Get(n)
}

func (c *child) luaWrite(L *lua.LState) int {
	s, ok := selfArg(L, 1).(lua.LString)
	if !ok {
		return fail(L, "write: expected a string")
	}
	if !c.alive() {
		return fail(L, c.deadReason())
	}
	line := string(s)
	// Exactly one terminating newline, added when it is missing. These are line protocols, and a
	// forgotten newline is a write that arrives nowhere and a read that waits out its deadline —
	// a silent hang with nothing in any log to say why.
	if !strings.HasSuffix(line, "\n") {
		line += "\n"
	}
	if _, err := io.WriteString(c.stdin, line); err != nil {
		return fail(L, "write: "+err.Error())
	}
	c.touch()
	L.Push(lua.LTrue)
	return 1
}

func (c *child) luaRead(L *lua.LState) int {
	to := pipeReadDefault
	if opts, ok := selfArg(L, 1).(*lua.LTable); ok {
		if raw, ok := opts.RawGetString("timeout").(lua.LString); ok {
			if d, err := time.ParseDuration(string(raw)); err == nil && d > 0 {
				to = d
			}
		}
	}
	c.touch()
	timer := time.NewTimer(to)
	defer timer.Stop()
	select {
	case line := <-c.lines:
		c.touch()
		L.Push(lua.LString(line))
		return 1
	case <-c.done:
		// Drain first: EOF and a last line can land together, and losing the reply because the
		// child exited right after writing it would be a race that reads as a flaky backend.
		select {
		case line := <-c.lines:
			L.Push(lua.LString(line))
			return 1
		default:
		}
		return fail(L, c.deadReason())
	case <-timer.C:
		// nil with no error: a deadline is not a failure, it is "nothing yet". The caller decides
		// whether to wait again or give up, and can tell that apart from a dead child.
		L.Push(lua.LNil)
		return 1
	}
}

func (c *child) luaAlive(L *lua.LState) int {
	L.Push(lua.LBool(c.alive()))
	return 1
}

func (c *child) luaClose(L *lua.LState) int {
	c.Close()
	L.Push(lua.LTrue)
	return 1
}
