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

	mu      sync.Mutex
	L       *lua.LState
	tools   []*luaTool
	env     port.ToolEnv                // set per tool Execute so bridge calls see the workdir
	hooks   map[string][]*lua.LFunction // lifecycle handlers registered via magi.on(event, fn)
	servers []io.Closer                 // loopback HTTP servers opened via magi.serve; closed on unload
	baseSet bool                        // this plugin overrode the LLM base URL (magi.set_base_url)
	baseURL string                      // the exact override this plugin set; compare-and-cleared on unload
	logf    func(string)
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
	// Compare-and-clear so a reload (whose new instance already installed its own override
	// before this old instance is closed) or another redirecting plugin isn't clobbered.
	if p.baseSet && p.host != nil && p.host.baseReg != nil {
		p.host.baseReg.ClearBaseURLIfEquals(p.baseURL)
		p.baseSet = false
	}
	if p.L != nil {
		p.L.Close()
		p.L = nil
	}
}

// fire runs this plugin's handlers for a lifecycle event, synchronously (a
// handler may block — e.g. a startup auth flow). Errors are logged, not fatal.
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
