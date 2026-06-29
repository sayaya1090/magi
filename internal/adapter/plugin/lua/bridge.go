package lua

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/prompt"
)

// installBridge installs the sandboxed `magi.*` API into the plugin's state.
// All host capabilities are reached only through this table; bridge functions
// enforce the manifest's permission grant.
func installBridge(p *plugin) {
	L := p.L
	t := L.NewTable()

	L.SetField(t, "register_tool", L.NewFunction(p.bridgeRegisterTool))
	L.SetField(t, "register_mcp", L.NewFunction(p.bridgeRegisterMCP))
	L.SetField(t, "register_context_provider", L.NewFunction(p.bridgeRegisterContextProvider))
	L.SetField(t, "set_llm_headers", L.NewFunction(p.bridgeSetLLMHeaders))
	L.SetField(t, "on", L.NewFunction(p.bridgeOn))
	L.SetField(t, "ask", L.NewFunction(p.bridgeAsk))
	L.SetField(t, "log", L.NewFunction(p.bridgeLog))
	L.SetField(t, "read_file", L.NewFunction(p.bridgeReadFile))
	L.SetField(t, "write_file", L.NewFunction(p.bridgeWriteFile))
	L.SetField(t, "workdir", L.NewFunction(p.bridgeWorkdir))
	L.SetField(t, "model", L.NewFunction(p.bridgeModel))
	L.SetField(t, "platform", L.NewFunction(p.bridgePlatform))
	L.SetField(t, "time", L.NewFunction(p.bridgeTime))
	// Gated capabilities — enforced against the plugin's exec:/net: permissions.
	L.SetField(t, "exec", L.NewFunction(p.bridgeExec))
	L.SetField(t, "open_url", L.NewFunction(p.bridgeOpenURL))
	L.SetField(t, "http", L.NewFunction(p.bridgeHTTP))
	L.SetField(t, "await_callback", L.NewFunction(p.bridgeAwaitCallback))
	// Plugin custom settings: read [plugins.<name>], persist own values.
	L.SetField(t, "config_get", L.NewFunction(p.bridgeConfigGet))
	L.SetField(t, "config_set", L.NewFunction(p.bridgeConfigSet))

	L.SetGlobal("magi", t)
	// Redirect print to the host log so plugins can't corrupt the TUI stdout.
	L.SetGlobal("print", L.NewFunction(p.bridgeLog))
}

// requireCap denies a bridge call whose capability the manifest did not declare
// (SPEC F-PLUGIN: capabilities are gated at the bridge). Raises a Lua error (which
// aborts the offending call) when the capability is absent.
func (p *plugin) requireCap(L *lua.LState, cap string) {
	if !p.caps[cap] {
		L.RaiseError("capability %q not declared in plugin.toml (add it to capabilities=[...])", cap)
	}
}

// magi.register_tool{name=, description=, schema=, execute=function(args) ... end}
func (p *plugin) bridgeRegisterTool(L *lua.LState) int {
	p.requireCap(L, "tool")
	spec := L.CheckTable(1)

	name := spec.RawGetString("name").String()
	if name == "" || spec.RawGetString("name") == lua.LNil {
		L.RaiseError("register_tool: 'name' is required")
		return 0
	}
	desc := ""
	if d := spec.RawGetString("description"); d != lua.LNil {
		desc = d.String()
	}
	fn, ok := spec.RawGetString("execute").(*lua.LFunction)
	if !ok {
		L.RaiseError("register_tool: 'execute' must be a function")
		return 0
	}

	var schema json.RawMessage
	switch s := spec.RawGetString("schema").(type) {
	case lua.LString:
		schema = json.RawMessage(string(s))
	case *lua.LTable:
		b, _ := json.Marshal(luaToGo(s))
		schema = b
	default:
		schema = json.RawMessage(`{"type":"object"}`)
	}

	p.tools = append(p.tools, &luaTool{
		plugin: p, name: name, description: desc, schema: schema, fn: fn,
	})
	return 0
}

// magi.register_mcp{name=, url=, headers={} | function}
func (p *plugin) bridgeRegisterMCP(L *lua.LState) int {
	p.requireCap(L, "mcp")
	if p.host == nil || p.host.mcpMgr == nil {
		L.RaiseError("register_mcp: MCP manager not available")
		return 0
	}

	spec := L.CheckTable(1)

	name := spec.RawGetString("name").String()
	if name == "" {
		L.RaiseError("register_mcp: 'name' is required")
		return 0
	}

	url := spec.RawGetString("url").String()
	if url == "" {
		L.RaiseError("register_mcp: 'url' is required")
		return 0
	}

	ctx := context.Background()
	switch hv := spec.RawGetString("headers").(type) {
	case *lua.LFunction:
		// Dynamic headers: the function is re-invoked on EVERY request so values
		// like magi.time()/magi.model() reflect request time, not setup time.
		fn := hv
		headersFn := func() map[string]string { return p.callHeaderFn(fn) }
		if err := p.host.mcpMgr.AddHTTPDynamic(ctx, name, url, headersFn); err != nil {
			L.RaiseError("register_mcp: %v", err)
			return 0
		}
	default:
		// Static headers table (or none).
		headers := map[string]string{}
		if tbl, ok := hv.(*lua.LTable); ok {
			tbl.ForEach(func(k, v lua.LValue) {
				if ks, ok := k.(lua.LString); ok {
					if vs, ok := v.(lua.LString); ok {
						headers[string(ks)] = string(vs)
					}
				}
			})
		}
		if err := p.host.mcpMgr.AddHTTP(ctx, name, url, headers); err != nil {
			L.RaiseError("register_mcp: %v", err)
			return 0
		}
	}

	p.logf(fmt.Sprintf("Registered MCP server: %s at %s", name, url))
	return 0
}

// knownEvents are the lifecycle points the host fires (magi.on validates
// against these so a typo'd event name is caught at registration).
var knownEvents = map[string]bool{
	"startup":       true, // after plugins load, before the first turn (UI ready)
	"shutdown":      true, // on exit
	"session_start": true, // a new top-level session was created
}

// magi.on(event, fn) — register a lifecycle handler the host calls at `event`.
func (p *plugin) bridgeOn(L *lua.LState) int {
	event := L.CheckString(1)
	if !knownEvents[event] {
		L.RaiseError("on: unknown event %q (known: startup, shutdown, session_start)", event)
		return 0
	}
	fn, ok := L.Get(2).(*lua.LFunction)
	if !ok {
		L.RaiseError("on: second argument must be a function")
		return 0
	}
	if p.hooks == nil {
		p.hooks = map[string][]*lua.LFunction{}
	}
	p.hooks[event] = append(p.hooks[event], fn)
	return 0
}

// magi.ask{title=, fields={ {name=,type=,label=,options={},default=}, … }} →
// answers table. Interactive prompt rendered by the host; errors when no terminal
// is available (headless) so the plugin can fall back.
func (p *plugin) bridgeAsk(L *lua.LState) int {
	if p.host == nil || p.host.prompter == nil {
		return fail(L, "ask: no interactive prompt available")
	}
	spec := L.CheckTable(1)
	s := prompt.Spec{Title: spec.RawGetString("title").String()}
	if ft, ok := spec.RawGetString("fields").(*lua.LTable); ok {
		ft.ForEach(func(_, fv lua.LValue) {
			ftbl, ok := fv.(*lua.LTable)
			if !ok {
				return
			}
			f := prompt.Field{
				Name:    ftbl.RawGetString("name").String(),
				Type:    ftbl.RawGetString("type").String(),
				Label:   ftbl.RawGetString("label").String(),
				Default: ftbl.RawGetString("default").String(),
			}
			if opts, ok := ftbl.RawGetString("options").(*lua.LTable); ok {
				opts.ForEach(func(_, ov lua.LValue) {
					if os, ok := ov.(lua.LString); ok {
						f.Options = append(f.Options, string(os))
					}
				})
			}
			s.Fields = append(s.Fields, f)
		})
	}
	ans, err := p.host.prompter.Ask(s)
	if err != nil {
		return fail(L, "ask: "+err.Error())
	}
	out := L.NewTable()
	for k, v := range ans {
		L.SetField(out, k, goToLua(L, v))
	}
	L.Push(out)
	return 1
}

// magi.set_llm_headers(table | function) — inject custom headers into LLM
// backend requests. A table is static; a function is re-invoked per request so
// values like a rotating SSO token reflect request time.
func (p *plugin) bridgeSetLLMHeaders(L *lua.LState) int {
	if p.host == nil || p.host.llmReg == nil {
		L.RaiseError("set_llm_headers: LLM header registry not available")
		return 0
	}
	switch v := L.Get(1).(type) {
	case *lua.LFunction:
		fn := v
		p.host.llmReg.AddLLMHeaders(func() map[string]string { return p.callHeaderFn(fn) })
	case *lua.LTable:
		static := map[string]string{}
		v.ForEach(func(k, val lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				if vs, ok := val.(lua.LString); ok {
					static[string(ks)] = string(vs)
				}
			}
		})
		p.host.llmReg.AddLLMHeaders(func() map[string]string { return static })
	default:
		L.RaiseError("set_llm_headers: argument must be a table or function")
		return 0
	}
	p.logf("[" + p.name + "] set custom LLM headers")
	return 0
}

// callHeaderFn invokes a plugin's dynamic-headers Lua function under the plugin
// lock (gopher-lua states are not concurrency-safe) and returns the resulting
// string→string map. Errors and non-table results yield no headers.
func (p *plugin) callHeaderFn(fn *lua.LFunction) map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	L := p.L
	if L == nil {
		return nil // plugin unloaded
	}
	if err := L.CallByParam(lua.P{Fn: fn, NRet: 1, Protect: true}); err != nil {
		p.logf(fmt.Sprintf("register_mcp: headers function error: %v", err))
		return nil
	}
	result := L.Get(-1)
	L.Pop(1)
	out := map[string]string{}
	if tbl, ok := result.(*lua.LTable); ok {
		tbl.ForEach(func(k, v lua.LValue) {
			if ks, ok := k.(lua.LString); ok {
				if vs, ok := v.(lua.LString); ok {
					out[string(ks)] = string(vs)
				}
			}
		})
	}
	return out
}

// magi.register_context_provider{name=, provide=function(query)}
func (p *plugin) bridgeRegisterContextProvider(L *lua.LState) int {
	p.requireCap(L, "context-provider")
	if p.host == nil || p.host.contextReg == nil {
		L.RaiseError("register_context_provider: context provider registry not available")
		return 0
	}

	spec := L.CheckTable(1)

	name := spec.RawGetString("name").String()
	if name == "" {
		L.RaiseError("register_context_provider: 'name' is required")
		return 0
	}

	fn, ok := spec.RawGetString("provide").(*lua.LFunction)
	if !ok {
		L.RaiseError("register_context_provider: 'provide' must be a function")
		return 0
	}

	provider := &luaContextProvider{
		plugin: p,
		name:   name,
		fn:     fn,
	}

	p.host.contextReg.RegisterContextProvider(provider)
	p.logf(fmt.Sprintf("Registered context provider: %s", name))
	return 0
}

func (p *plugin) bridgeLog(L *lua.LState) int {
	if p.logf != nil {
		p.logf("[" + p.name + "] " + L.ToString(1))
	}
	return 0
}

func (p *plugin) bridgeWorkdir(L *lua.LState) int {
	L.Push(lua.LString(p.env.Workdir))
	return 1
}

// magi.model() -> string
func (p *plugin) bridgeModel(L *lua.LState) int {
	if p.host != nil {
		L.Push(lua.LString(p.host.runtime.Model))
	} else {
		L.Push(lua.LString(""))
	}
	return 1
}

// magi.platform() -> string
func (p *plugin) bridgePlatform(L *lua.LState) int {
	if p.host != nil && p.host.runtime.Platform != "" {
		L.Push(lua.LString(p.host.runtime.Platform))
	} else {
		L.Push(lua.LString(runtime.GOOS))
	}
	return 1
}

// magi.time() -> string (ISO8601)
func (p *plugin) bridgeTime(L *lua.LState) int {
	L.Push(lua.LString(time.Now().UTC().Format(time.RFC3339)))
	return 1
}

// magi.read_file(path) -> content | (nil, error)
func (p *plugin) bridgeReadFile(L *lua.LState) int {
	rel := L.CheckString(1)
	abs, ok := p.resolve(rel)
	if !ok {
		return failPath(L, rel)
	}
	if !p.perms.allowFSRead(rel) {
		return fail(L, "permission denied: fs:read "+rel)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return fail(L, err.Error())
	}
	L.Push(lua.LString(string(b)))
	return 1
}

// magi.write_file(path, content) -> true | (nil, error)
func (p *plugin) bridgeWriteFile(L *lua.LState) int {
	rel := L.CheckString(1)
	content := L.CheckString(2)
	abs, ok := p.resolve(rel)
	if !ok {
		return failPath(L, rel)
	}
	if !p.perms.allowFSWrite(rel) {
		return fail(L, "permission denied: fs:write "+rel)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fail(L, err.Error())
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return fail(L, err.Error())
	}
	L.Push(lua.LTrue)
	return 1
}

// resolve maps a plugin-supplied path to an absolute path inside the session
// workdir, rejecting escapes.
func (p *plugin) resolve(rel string) (string, bool) {
	base := filepath.Clean(p.env.Workdir)
	if base == "" || base == "." {
		return "", false
	}
	var abs string
	if filepath.IsAbs(rel) {
		abs = filepath.Clean(rel)
	} else {
		abs = filepath.Clean(filepath.Join(base, rel))
	}
	r, err := filepath.Rel(base, abs)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

func fail(L *lua.LState, msg string) int {
	L.Push(lua.LNil)
	L.Push(lua.LString(msg))
	return 2
}

func failPath(L *lua.LState, rel string) int {
	return fail(L, "invalid path (outside workdir): "+rel)
}
