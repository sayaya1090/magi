package lua

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/sayaya1090/magi/internal/port"
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
	L.SetField(t, "register_command", L.NewFunction(p.bridgeRegisterCommand))
	L.SetField(t, "register_doctor_probes", L.NewFunction(p.bridgeRegisterDoctorProbes))
	L.SetField(t, "set_llm_headers", L.NewFunction(p.bridgeSetLLMHeaders))
	L.SetField(t, "on", L.NewFunction(p.bridgeOn))
	L.SetField(t, "analyze", L.NewFunction(p.bridgeAnalyze))
	L.SetField(t, "json_decode", L.NewFunction(p.bridgeJSONDecode))
	L.SetField(t, "propose_experience", L.NewFunction(p.bridgeProposeExperience))
	L.SetField(t, "notify", L.NewFunction(p.bridgeNotify))
	L.SetField(t, "remove_file", L.NewFunction(p.bridgeRemoveFile))
	L.SetField(t, "ask", L.NewFunction(p.bridgeAsk))
	L.SetField(t, "log", L.NewFunction(p.bridgeLog))
	L.SetField(t, "read_file", L.NewFunction(p.bridgeReadFile))
	L.SetField(t, "write_file", L.NewFunction(p.bridgeWriteFile))
	L.SetField(t, "workdir", L.NewFunction(p.bridgeWorkdir))
	L.SetField(t, "model", L.NewFunction(p.bridgeModel))
	L.SetField(t, "set_model", L.NewFunction(p.bridgeSetModel))
	L.SetField(t, "set_user_label", L.NewFunction(p.bridgeSetUserLabel))
	L.SetField(t, "clear_transcript", L.NewFunction(p.bridgeClearTranscript))
	L.SetField(t, "reload_config", L.NewFunction(p.bridgeReloadConfig))
	L.SetField(t, "platform", L.NewFunction(p.bridgePlatform))
	L.SetField(t, "time", L.NewFunction(p.bridgeTime))
	L.SetField(t, "nonce", L.NewFunction(p.bridgeNonce))
	// Gated capabilities — enforced against the plugin's exec:/net: permissions.
	L.SetField(t, "exec", L.NewFunction(p.bridgeExec))
	L.SetField(t, "open_url", L.NewFunction(p.bridgeOpenURL))
	L.SetField(t, "http", L.NewFunction(p.bridgeHTTP))
	L.SetField(t, "serve", L.NewFunction(p.bridgeServe))
	L.SetField(t, "set_base_url", L.NewFunction(p.bridgeSetBaseURL))
	// Plugin custom settings: read [plugins.<name>], persist own values.
	L.SetField(t, "store_get", L.NewFunction(p.bridgeStoreGet))
	L.SetField(t, "store_set", L.NewFunction(p.bridgeStoreSet))
	// User config.toml access (dotted keys), gated by config:read/write permissions.
	L.SetField(t, "get_config_key", L.NewFunction(p.bridgeGetConfigKey))
	L.SetField(t, "set_config_key", L.NewFunction(p.bridgeSetConfigKey))

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
	// Observation events (payload table arg; fired asynchronously off the turn
	// path, best-effort): conversation milestones for observer-style plugins.
	"user_message":  true, // {session=, text=} — a genuine user prompt was submitted
	"turn_finished": true, // {session=, text=} — a top-level turn ended (text = final assistant answer)
}

// magi.on(event, fn) — register a lifecycle handler the host calls at `event`.
func (p *plugin) bridgeOn(L *lua.LState) int {
	event := L.CheckString(1)
	if !knownEvents[event] {
		L.RaiseError("on: unknown event %q (known: startup, shutdown, session_start, user_message, turn_finished)", event)
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
		// A missing Lua key returns LNil, whose .String() is the literal "nil" — which
		// would slip past prompt.go's empty-label fallback and render "› nil". Normalize
		// absent values to "" so the fallback (label == "" → Name) works.
		str := func(v lua.LValue) string {
			if v == lua.LNil {
				return ""
			}
			return v.String()
		}
		ft.ForEach(func(_, fv lua.LValue) {
			ftbl, ok := fv.(*lua.LTable)
			if !ok {
				return
			}
			f := prompt.Field{
				Name:    str(ftbl.RawGetString("name")),
				Type:    str(ftbl.RawGetString("type")),
				Label:   str(ftbl.RawGetString("label")),
				Default: str(ftbl.RawGetString("default")),
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

// magi.register_command{name=, description=, execute=function(args) ... end}
// Registers a TUI slash command (name "login" → /login). execute receives the
// whitespace-split tokens after the command as an array; return a non-empty
// string to signal an error, or nil for success.
func (p *plugin) bridgeRegisterCommand(L *lua.LState) int {
	p.requireCap(L, "command")
	spec := L.CheckTable(1)

	name := spec.RawGetString("name").String()
	if name == "" || spec.RawGetString("name") == lua.LNil {
		L.RaiseError("register_command: 'name' is required")
		return 0
	}
	desc := ""
	if d := spec.RawGetString("description"); d != lua.LNil {
		desc = d.String()
	}
	fn, ok := spec.RawGetString("execute").(*lua.LFunction)
	if !ok {
		L.RaiseError("register_command: 'execute' must be a function")
		return 0
	}

	p.commands = append(p.commands, &luaCommand{
		plugin: p, name: strings.TrimPrefix(name, "/"), description: desc, fn: fn,
	})
	p.logf(fmt.Sprintf("Registered command: /%s", strings.TrimPrefix(name, "/")))
	return 0
}

// magi.register_doctor_probes{name = function() return status, detail end, ...}
// Registers one or more environment checks that `magi -doctor` runs and folds into
// its report — e.g. an SSO plugin verifying its cached token. Each value is a
// function returning (status, detail) where status is ok|warn|fail|info. Gated on
// the "doctor" capability. Probes are collected at load time, so -doctor can gather
// them without firing plugin startup handlers (no interactive auth during a check).
func (p *plugin) bridgeRegisterDoctorProbes(L *lua.LState) int {
	p.requireCap(L, "doctor")
	spec := L.CheckTable(1)
	n := 0
	spec.ForEach(func(k, v lua.LValue) {
		name, ok := k.(lua.LString)
		if !ok || string(name) == "" {
			return
		}
		fn, ok := v.(*lua.LFunction)
		if !ok {
			return
		}
		p.probes = append(p.probes, &luaDoctorProbe{plugin: p, name: string(name), fn: fn})
		n++
	})
	if n == 0 {
		L.RaiseError("register_doctor_probes: expected a table of {name = function}")
		return 0
	}
	p.logf(fmt.Sprintf("Registered %d doctor probe(s)", n))
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
		L.Push(lua.LString(p.host.runtimeModel()))
	} else {
		L.Push(lua.LString(""))
	}
	return 1
}

// magi.set_model(model) -> true | (nil, err)
// Changes the current session's active model at runtime (and persists it, like the
// /route editor). Gated on config:write:model since it writes the top-level `model`
// key; a plugin always holds write over its own [plugins.<name>] section but must be
// granted config:write:model to steer the session model.
func (p *plugin) bridgeSetModel(L *lua.LState) int {
	if p.host == nil || p.host.modelReg == nil {
		return fail(L, "set_model: model registry not available")
	}
	model := strings.TrimSpace(L.CheckString(1))
	if model == "" {
		return fail(L, "set_model: model must be a non-empty string")
	}
	if !p.perms.allowConfigWrite("model") {
		return fail(L, "permission denied: config:write:model")
	}
	if err := p.host.modelReg.SetModel(model); err != nil {
		return fail(L, "set_model: "+err.Error())
	}
	p.host.setRuntimeModel(model) // keep magi.model() in sync
	p.logf("[" + p.name + "] set session model: " + model)
	L.Push(lua.LTrue)
	return 1
}

// magi.set_user_label(name) -> true | (nil, err)
// Sets the display name shown for the user in the transcript (replacing the default
// "you") — e.g. an SSO plugin injecting the authenticated username. Gated on the
// "ui" capability (a UI-touching primitive class), like register_command's
// "command" cap. An empty name is rejected so a failed lookup can't blank the label.
func (p *plugin) bridgeSetUserLabel(L *lua.LState) int {
	p.requireCap(L, "ui")
	if p.host == nil || p.host.userReg == nil {
		return fail(L, "set_user_label: user label registry not available")
	}
	label := strings.TrimSpace(L.CheckString(1))
	if label == "" {
		return fail(L, "set_user_label: name must be a non-empty string")
	}
	p.host.userReg.SetUserLabel(label)
	p.logf("[" + p.name + "] set user label: " + label)
	L.Push(lua.LTrue)
	return 1
}

// magi.reload_config() -> true | (nil, err)
// Re-reads the user's config.toml and applies what can take effect live — currently
// the session model. A parse error is returned as (nil, err) and the running session
// keeps its settings (so a corrupt edit can't silently blank the model). Other
// settings (routing, base URLs, plugin reloads) still require a restart. Gated on
// config:write:model since it can change the session model.
func (p *plugin) bridgeReloadConfig(L *lua.LState) int {
	if p.host == nil || p.host.configPath == "" {
		return fail(L, "reload_config: config path not available")
	}
	if !p.perms.allowConfigWrite("model") {
		return fail(L, "permission denied: config:write:model")
	}
	v, ok, err := readConfigKey(p.host.configPath, "model")
	if err != nil {
		return fail(L, "reload_config: cannot parse config: "+err.Error())
	}
	if ok {
		if s, isStr := v.(string); isStr {
			if s = strings.TrimSpace(s); s != "" {
				if p.host.modelReg != nil {
					if err := p.host.modelReg.SetModel(s); err != nil {
						return fail(L, "reload_config: "+err.Error())
					}
				}
				p.host.setRuntimeModel(s)
			}
		}
	}
	p.host.pushUIEffect("reload_config")
	p.logf("[" + p.name + "] reload config")
	L.Push(lua.LTrue)
	return 1
}

// magi.clear_transcript() -> true
// Queues a UI effect that clears the visible transcript back to the splash screen
// (the on-disk session is untouched). A plugin's /logout command uses this to return
// the user to a clean start. UI-only, so it needs no capability grant.
func (p *plugin) bridgeClearTranscript(L *lua.LState) int {
	if p.host == nil {
		return fail(L, "clear_transcript: unavailable")
	}
	p.host.pushUIEffect("clear_transcript")
	p.logf("[" + p.name + "] clear transcript")
	L.Push(lua.LTrue)
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

// magi.nonce(nbytes?) -> hex string of nbytes random bytes (default 16 → 32 hex chars).
// Cryptographically random (crypto/rand). Lua's math.random is deterministically seeded
// in the sandbox (os is removed, so it can't seed from the clock), so plugins MUST use
// this — not math.random — for OAuth/PKCE state, CSRF tokens, request IDs, etc.
func (p *plugin) bridgeNonce(L *lua.LState) int {
	n := 16
	if v := L.Get(1); v != lua.LNil {
		n = int(lua.LVAsNumber(v))
	}
	if n < 1 {
		n = 16
	}
	if n > 256 {
		n = 256
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fail(L, "nonce: "+err.Error())
	}
	L.Push(lua.LString(hex.EncodeToString(b)))
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

// analyzeTimeout bounds one magi.analyze sidecar call. Generous: observer
// plugins run analysis off the turn path (the host's event worker), so a slow
// local model hurts only later observations, never the conversation.
const analyzeTimeout = 2 * time.Minute

// magi.analyze{system=, text=, model=?} → raw response text. A one-shot,
// tool-free LLM call for observation-style plugins (lesson/skill extraction).
// It cannot see the session, register tools, or mutate anything — it is a pure
// text→text sidecar on the configured backend.
func (p *plugin) bridgeAnalyze(L *lua.LState) int {
	p.requireCap(L, "analyze") // it spends LLM tokens — must be declared in the manifest
	if p.host == nil || p.host.analyzer == nil {
		return fail(L, "analyze: no analyzer available")
	}
	spec := L.CheckTable(1)
	// An absent field is LNil, whose String() is the literal "nil" — read each
	// through a nil guard so a missing system/text can't silently become "nil".
	str := func(key string) string {
		if v := spec.RawGetString(key); v != lua.LNil {
			return v.String()
		}
		return ""
	}
	system, text, model := str("system"), str("text"), str("model")
	if strings.TrimSpace(text) == "" {
		return fail(L, "analyze: text is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), analyzeTimeout)
	defer cancel()
	out, err := p.host.analyzer.Analyze(ctx, system, text, model)
	if err != nil {
		return fail(L, "analyze: "+err.Error())
	}
	L.Push(lua.LString(out))
	return 1
}

// magi.propose_experience{scope=?, memories={{text=,tags={..}},…}, skills={{name=,description=,body=},…}}
// → true (or nil, err). Routes a plugin's learned knowledge into the D13 shared
// experience store's review queue (team tier via git), so plugin-captured
// lessons/skills join the same curation pipeline as everything else. Capability
// "experience" — it writes to a team-shared store.
func (p *plugin) bridgeProposeExperience(L *lua.LState) int {
	p.requireCap(L, "experience")
	if p.host == nil || p.host.experience == nil {
		return fail(L, "propose_experience: no experience store available")
	}
	spec := L.CheckTable(1)
	var c port.Contribution
	c.Source = "plugin:" + p.name
	if v := spec.RawGetString("scope"); v != lua.LNil {
		c.Scope = v.String()
	}
	if mt, ok := spec.RawGetString("memories").(*lua.LTable); ok {
		mt.ForEach(func(_, v lua.LValue) {
			t, ok := v.(*lua.LTable)
			if !ok {
				return
			}
			m := port.Memory{Text: t.RawGetString("text").String()}
			if tags, ok := t.RawGetString("tags").(*lua.LTable); ok {
				tags.ForEach(func(_, tv lua.LValue) { m.Tags = append(m.Tags, tv.String()) })
			}
			if strings.TrimSpace(m.Text) != "" && m.Text != "nil" {
				c.Memories = append(c.Memories, m)
			}
		})
	}
	if st, ok := spec.RawGetString("skills").(*lua.LTable); ok {
		st.ForEach(func(_, v lua.LValue) {
			t, ok := v.(*lua.LTable)
			if !ok {
				return
			}
			s := port.Skill{
				Name:        t.RawGetString("name").String(),
				Description: t.RawGetString("description").String(),
				Body:        t.RawGetString("body").String(),
			}
			if strings.TrimSpace(s.Name) != "" && s.Name != "nil" {
				c.Skills = append(c.Skills, s)
			}
		})
	}
	if len(c.Memories) == 0 && len(c.Skills) == 0 {
		return fail(L, "propose_experience: nothing to propose")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.host.experience.Propose(ctx, c); err != nil {
		return fail(L, "propose_experience: "+err.Error())
	}
	L.Push(lua.LTrue)
	return 1
}

// magi.notify(session, text) → true (or nil, err). Appends a system note to the
// session transcript (rendered as a ⟳ note; the model sees it next turn) — the
// active-notification channel for observer plugins ("skill saved — reply N to
// undo"). Capability "notify": it injects text into the conversation.
func (p *plugin) bridgeNotify(L *lua.LState) int {
	p.requireCap(L, "notify")
	if p.host == nil || p.host.notify == nil {
		return fail(L, "notify: no notification channel available")
	}
	sid, text := L.CheckString(1), L.CheckString(2)
	if strings.TrimSpace(sid) == "" || strings.TrimSpace(text) == "" {
		return fail(L, "notify: session and text are required")
	}
	p.host.notify(sid, text)
	L.Push(lua.LTrue)
	return 1
}

// magi.remove_file(rel) → true (or nil, err). Deletes a file or directory
// (recursively) inside the workdir — the undo half of a plugin that writes
// artifacts (e.g. engram retracting a just-saved skill). Same fs:write grant
// and workdir confinement as write_file.
func (p *plugin) bridgeRemoveFile(L *lua.LState) int {
	rel := L.CheckString(1)
	abs, ok := p.resolve(rel)
	if !ok {
		return failPath(L, rel)
	}
	if !p.perms.allowFSWrite(rel) {
		return fail(L, "permission denied: fs:write "+rel)
	}
	if err := os.RemoveAll(abs); err != nil {
		return fail(L, err.Error())
	}
	L.Push(lua.LTrue)
	return 1
}

// magi.json_decode(s) → table (or nil, err). JSON decoding for plugins that
// parse structured sidecar output; the sandbox has no require/package, so a
// pure-Lua JSON library isn't an option.
func (p *plugin) bridgeJSONDecode(L *lua.LState) int {
	var v any
	if err := json.Unmarshal([]byte(L.CheckString(1)), &v); err != nil {
		return fail(L, "json_decode: "+err.Error())
	}
	L.Push(goToLua(L, v))
	return 1
}

func failPath(L *lua.LState, rel string) int {
	return fail(L, "invalid path (outside workdir): "+rel)
}
