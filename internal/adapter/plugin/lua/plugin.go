package lua

import (
	"fmt"
	"io"
	"path/filepath"
	"sync"

	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/port"
)

// plugin is a single loaded plugin: a sandboxed Lua state plus the tools it
// registered. The LState is not concurrency-safe, so all access is guarded by mu.
type plugin struct {
	name     string
	dir      string
	manifest Manifest
	perms    perms
	caps     map[string]bool // declared capabilities (gate the register_* bridge calls)
	host     *Host           // back-reference to host for MCP registration

	mu       sync.Mutex
	L        *lua.LState
	tools    []*luaTool
	commands []*luaCommand               // slash commands registered via magi.register_command
	probes   []*luaDoctorProbe           // -doctor checks registered via magi.register_doctor_probes
	env      port.ToolEnv                // set per tool Execute so bridge calls see the workdir
	hooks    map[string][]*lua.LFunction // lifecycle handlers registered via magi.on(event, fn)
	servers  []io.Closer                 // loopback HTTP servers opened via magi.serve; closed on unload
	baseSet  bool                        // this plugin overrode the LLM base URL (magi.set_base_url)
	baseTok  uint64                      // ownership token of that override; released (only if still current) on unload
	logf     func(string)
}

// loadPlugin reads the manifest, builds a sandboxed state, installs the bridge,
// and runs the entry script (which registers capabilities).
func loadPlugin(dir string, logf func(string), host *Host) (*plugin, error) {
	m, err := loadManifest(dir)
	if err != nil {
		return nil, err
	}
	caps := make(map[string]bool, len(m.Capabilities))
	for _, c := range m.Capabilities {
		caps[c] = true
	}
	p := &plugin{
		name:     m.Name,
		dir:      dir,
		manifest: m,
		perms:    parsePerms(m.Permissions),
		caps:     caps,
		host:     host,
		logf:     logf,
	}
	// Seed the bridge workdir from the host runtime so read_file/write_file work
	// OUTSIDE tool execution too (lifecycle/observation handlers, context
	// providers). Tool Execute still overwrites env per call, which keeps the
	// stricter per-call workdir when one is provided.
	if host != nil && host.runtime.Workdir != "" {
		p.env.Workdir = host.runtime.Workdir
	}
	p.L = newSandbox()
	installBridge(p)

	// Hold the plugin lock while the entry script runs: it may call magi.serve, which
	// starts an HTTP handler goroutine that calls back into this (non-thread-safe) LState.
	// Locking here makes any such request block until setup completes.
	p.mu.Lock()
	err = p.L.DoFile(filepath.Join(dir, m.Entry))
	p.mu.Unlock()
	if err != nil {
		p.close()
		return nil, fmt.Errorf("plugin %q: run %s: %w", m.Name, m.Entry, err)
	}
	return p, nil
}

func (p *plugin) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Close loopback servers first so no handler goroutine calls back into the
	// Lua state while/after we tear it down.
	for _, s := range p.servers {
		_ = s.Close()
	}
	p.servers = nil
	// Restore the configured LLM backend if this plugin had redirected it: otherwise
	// dynBase keeps pointing at the now-dead loopback proxy and every LLM call fails.
	// Release by token so a reload (whose new instance installed a fresh override — a new
	// token — before this old instance is closed) or another redirecting plugin isn't
	// clobbered; a stale token makes ClearBaseURL a no-op.
	if p.baseSet && p.host != nil && p.host.baseReg != nil {
		p.host.baseReg.ClearBaseURL(p.baseTok)
		p.baseSet = false
	}
	if p.L != nil {
		p.L.Close()
		p.L = nil
	}
}

// fire runs this plugin's handlers for a lifecycle event, synchronously (a
// handler may block — e.g. a startup auth flow). Errors are logged, not fatal.
// Note: close()/reload and the synchronous shutdown fire take the same p.mu, so
// they can wait behind an in-flight observation handler (bounded by the
// analyze bridge's own timeout).
func (p *plugin) fire(event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.L == nil {
		return
	}
	for _, fn := range p.hooks[event] {
		if err := p.L.CallByParam(lua.P{Fn: fn, NRet: 0, Protect: true}); err != nil {
			p.logf(fmt.Sprintf("[%s] on(%s): %v", p.name, event, err))
		}
	}
}

// hasHook reports whether this plugin registered a handler for event. A plugin
// busy in a handler (mutex held — possibly for a minutes-long magi.analyze) is
// assumed to HAVE handlers rather than blocking the asker: the only long holder
// is an observation handler, which by definition registered one.
func (p *plugin) hasHook(event string) bool {
	if !p.mu.TryLock() {
		return true
	}
	defer p.mu.Unlock()
	return len(p.hooks[event]) > 0
}

// fireWith runs this plugin's handlers for a payload-carrying event, passing the
// payload as a Lua table argument. Called from the host's single event worker
// (never the turn path), so a slow handler only delays later observations.
func (p *plugin) fireWith(event string, payload map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.L == nil {
		return
	}
	for _, fn := range p.hooks[event] {
		t := p.L.NewTable()
		for k, v := range payload {
			p.L.SetField(t, k, lua.LString(v))
		}
		if err := p.L.CallByParam(lua.P{Fn: fn, NRet: 0, Protect: true}, t); err != nil {
			p.logf(fmt.Sprintf("[%s] on(%s): %v", p.name, event, err))
		}
	}
}

// newSandbox creates a Lua state with only safe standard libraries and with
// code-loading / process / io escape hatches removed.
func newSandbox() *lua.LState {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	// Open only safe libraries (no package/require, io, os, debug).
	for _, lib := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		L.Push(L.NewFunction(lib.fn))
		L.Push(lua.LString(lib.name))
		L.Call(1, 0)
	}
	// Remove dangerous globals that OpenBase installs.
	for _, g := range []string{"dofile", "loadfile", "load", "loadstring", "collectgarbage"} {
		L.SetGlobal(g, lua.LNil)
	}
	return L
}
