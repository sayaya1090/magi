package lua

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// fakeMCPMgr captures plugin MCP registrations for assertion.
type fakeMCPMgr struct {
	httpName, httpURL string
	staticHeaders     map[string]string
	dynName, dynURL   string
	dynFn             func() map[string]string
}

func (f *fakeMCPMgr) AddHTTP(_ context.Context, name, url string, headers map[string]string) error {
	f.httpName, f.httpURL, f.staticHeaders = name, url, headers
	return nil
}
func (f *fakeMCPMgr) AddHTTPDynamic(_ context.Context, name, url string, fn func() map[string]string) error {
	f.dynName, f.dynURL, f.dynFn = name, url, fn
	return nil
}
func (f *fakeMCPMgr) AddStdio(_ context.Context, _, _ string, _, _ []string) error { return nil }
func (f *fakeMCPMgr) Remove(string)                                                {}

// fakeContextReg captures plugin context-provider registrations.
type fakeContextReg struct{ providers []port.ContextProvider }

func (f *fakeContextReg) RegisterContextProvider(p port.ContextProvider) {
	f.providers = append(f.providers, p)
}

// fakeLLMReg captures plugin LLM-header registrations.
type fakeLLMReg struct{ fns []func() map[string]string }

func (f *fakeLLMReg) AddLLMHeaders(fn func() map[string]string) { f.fns = append(f.fns, fn) }

func hostWith(mgr MCPManager, reg ContextProviderRegistry) *Host {
	return NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		MCPMgr:     mgr,
		ContextReg: reg,
		Runtime:    RuntimeInfo{Model: "test-model", Platform: "testos"},
	})
}

func hostWithLLM(llm LLMHeaderRegistry) *Host {
	return NewHostWithConfig(HostConfig{
		ToolSink: builtin.NewRegistry(),
		LLMReg:   llm,
		Runtime:  RuntimeInfo{Model: "test-model", Platform: "testos"},
	})
}

// SPEC F-PLUGIN: a capability the manifest did not declare is denied at the bridge.
// A plugin declaring only ["tool"] cannot register an MCP server — the register_mcp
// call raises, which aborts the entry script and fails the load.
func TestPluginCapabilityGateDeniesUndeclared(t *testing.T) {
	mgr := &fakeMCPMgr{}
	dir := writePlugin(t,
		`name="sneaky"`+"\n"+`capabilities=["tool"]`, // no "mcp"
		`magi.register_mcp{name="svc", url="http://localhost:9/mcp"}`,
	)
	_, err := hostWith(mgr, nil).Load(context.Background(), dir)
	if err == nil {
		t.Fatal("registering MCP without the 'mcp' capability must be denied")
	}
	if !strings.Contains(err.Error(), "capability") || !strings.Contains(err.Error(), "mcp") {
		t.Errorf("error should name the missing capability, got: %v", err)
	}
	if mgr.httpName != "" {
		t.Errorf("no MCP server should have been registered, got %q", mgr.httpName)
	}
}

// A plugin registering an MCP server with a STATIC headers table flows through
// AddHTTP with those headers verbatim.
func TestPluginRegistersMCPStatic(t *testing.T) {
	mgr := &fakeMCPMgr{}
	dir := writePlugin(t,
		`name="mcp-static"`+"\n"+`capabilities=["mcp"]`,
		`magi.register_mcp{name="svc", url="http://localhost:9/mcp", headers={Authorization="Bearer abc"}}`,
	)
	if _, err := hostWith(mgr, nil).Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if mgr.httpName != "svc" || mgr.httpURL != "http://localhost:9/mcp" {
		t.Fatalf("AddHTTP got name=%q url=%q", mgr.httpName, mgr.httpURL)
	}
	if mgr.staticHeaders["Authorization"] != "Bearer abc" {
		t.Errorf("static headers = %v", mgr.staticHeaders)
	}
}

// A plugin registering an MCP server with a headers FUNCTION flows through
// AddHTTPDynamic, and that function is re-evaluated on every request — so
// runtime values (here a counter, and magi.model()) are fresh each call, not
// frozen at registration time.
func TestPluginRegistersMCPDynamicHeaders(t *testing.T) {
	mgr := &fakeMCPMgr{}
	dir := writePlugin(t,
		`name="mcp-dyn"`+"\n"+`capabilities=["mcp"]`,
		`local n = 0
magi.register_mcp{
  name = "svc",
  url = "http://localhost:9/mcp",
  headers = function()
    n = n + 1
    return { ["X-Seq"] = tostring(n), ["X-Model"] = magi.model() }
  end,
}`,
	)
	if _, err := hostWith(mgr, nil).Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if mgr.dynName != "svc" || mgr.dynFn == nil {
		t.Fatalf("expected dynamic registration, got name=%q fn=%v", mgr.dynName, mgr.dynFn != nil)
	}
	h1 := mgr.dynFn()
	h2 := mgr.dynFn()
	if h1["X-Seq"] != "1" || h2["X-Seq"] != "2" {
		t.Errorf("dynamic headers not re-evaluated per call: h1=%v h2=%v", h1, h2)
	}
	if h1["X-Model"] != "test-model" {
		t.Errorf("runtime model not exposed to headers fn: %v", h1)
	}
}

// A plugin registering a context provider (RAG) flows through to a real
// port.ContextProvider whose Provide() runs the Lua function, receiving the
// query and returning chunks.
func TestPluginRegistersContextProvider(t *testing.T) {
	reg := &fakeContextReg{}
	dir := writePlugin(t,
		`name="rag"`+"\n"+`capabilities=["context-provider"]`,
		`magi.register_context_provider{
  name = "rag",
  provide = function(q)
    return { { source = "doc:" .. q.workdir, text = "answer for " .. q.prompt } }
  end,
}`,
	)
	if _, err := hostWith(nil, reg).Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(reg.providers) != 1 {
		t.Fatalf("expected 1 registered provider, got %d", len(reg.providers))
	}
	chunks, err := reg.providers[0].Provide(context.Background(), port.ContextQuery{
		SessionID: "s1", Workdir: "/proj", Prompt: "how to auth",
	})
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Source != "doc:/proj" || chunks[0].Text != "answer for how to auth" {
		t.Errorf("chunk = %+v (query not threaded into Lua?)", chunks[0])
	}
}

// A plugin registering a slash command surfaces it via PluginCommands and can be
// invoked through DispatchCommand, which runs the Lua function with the arg
// tokens; a nil return is success. The command name is stored without a slash.
func TestPluginRegistersCommand(t *testing.T) {
	dir := writePlugin(t,
		`name="cmd"`+"\n"+`capabilities=["command"]`,
		`captured = nil
magi.register_command{
  name = "login",
  description = "re-auth",
  execute = function(args) captured = args[1] end,
}`,
	)
	h := hostWith(nil, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	cmds := h.PluginCommands()
	if len(cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(cmds))
	}
	if cmds[0].Name() != "login" || cmds[0].Description() != "re-auth" {
		t.Errorf("command = name=%q desc=%q", cmds[0].Name(), cmds[0].Description())
	}
	// Leading slash is tolerated on dispatch.
	handled, err := h.DispatchCommand("/login", []string{"tok123"})
	if !handled || err != nil {
		t.Fatalf("DispatchCommand handled=%v err=%v", handled, err)
	}
}

// A command that calls magi.clear_transcript() queues a UI effect the TUI drains
// after dispatch (returning to the splash). The effect is one-shot: a second drain
// yields nothing.
func TestClearTranscriptQueuesUIEffect(t *testing.T) {
	dir := writePlugin(t,
		`name="out"`+"\n"+`capabilities=["command"]`,
		`magi.register_command{ name="logout", execute = function() magi.clear_transcript() end }`,
	)
	h := hostWith(nil, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := h.TakeUIEffects(); len(got) != 0 {
		t.Fatalf("no command run yet, want no effects, got %v", got)
	}
	if handled, err := h.DispatchCommand("logout", nil); !handled || err != nil {
		t.Fatalf("DispatchCommand handled=%v err=%v", handled, err)
	}
	got := h.TakeUIEffects()
	if len(got) != 1 || got[0] != "clear_transcript" {
		t.Fatalf("effects = %v, want [clear_transcript]", got)
	}
	if again := h.TakeUIEffects(); len(again) != 0 {
		t.Errorf("effects should drain once, second take got %v", again)
	}
}

// An unknown command is not handled, so the TUI can fall back to "unknown command".
func TestDispatchCommandUnknown(t *testing.T) {
	h := hostWith(nil, nil)
	if handled, err := h.DispatchCommand("nope", nil); handled || err != nil {
		t.Fatalf("unknown command should be unhandled: handled=%v err=%v", handled, err)
	}
}

// A command whose Lua function returns a non-empty string signals an error, which
// DispatchCommand surfaces as a Go error (so the TUI can show it in a snackbar).
func TestDispatchCommandError(t *testing.T) {
	dir := writePlugin(t,
		`name="cmderr"`+"\n"+`capabilities=["command"]`,
		`magi.register_command{ name="logout", execute = function() return "not logged in" end }`,
	)
	h := hostWith(nil, nil)
	if _, err := h.Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	handled, err := h.DispatchCommand("logout", nil)
	if !handled {
		t.Fatal("logout should be handled")
	}
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("expected the Lua error message, got %v", err)
	}
}

// Registering a command without the "command" capability is denied at the bridge.
func TestPluginCommandCapabilityGate(t *testing.T) {
	dir := writePlugin(t,
		`name="nocap"`+"\n"+`capabilities=["tool"]`, // no "command"
		`magi.register_command{ name="login", execute = function() end }`,
	)
	_, err := hostWith(nil, nil).Load(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "capability") {
		t.Fatalf("registering a command without the capability must be denied, got %v", err)
	}
}

// A plugin can inject LLM backend headers; a function form is re-evaluated per
// request so a rotating SSO token (here a counter) changes between calls.
func TestPluginSetsLLMHeadersDynamic(t *testing.T) {
	llm := &fakeLLMReg{}
	dir := writePlugin(t,
		`name="sso"`+"\n"+`capabilities=["llm-headers"]`,
		`local n = 0
magi.set_llm_headers(function()
  n = n + 1
  return { ["X-Client-Api-Key"] = "tok-" .. tostring(n), ["X-Model"] = magi.model() }
end)`,
	)
	if _, err := hostWithLLM(llm).Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(llm.fns) != 1 {
		t.Fatalf("expected 1 header fn, got %d", len(llm.fns))
	}
	h1 := llm.fns[0]()
	h2 := llm.fns[0]()
	if h1["X-Client-Api-Key"] != "tok-1" || h2["X-Client-Api-Key"] != "tok-2" {
		t.Errorf("LLM header fn not re-evaluated per call: h1=%v h2=%v", h1, h2)
	}
	if h1["X-Model"] != "test-model" {
		t.Errorf("runtime model not exposed: %v", h1)
	}
}

// The static-table form yields a constant header set.
func TestPluginSetsLLMHeadersStatic(t *testing.T) {
	llm := &fakeLLMReg{}
	dir := writePlugin(t,
		`name="sso-static"`+"\n"+`capabilities=["llm-headers"]`,
		`magi.set_llm_headers({ ["X-Client-Api-Key"] = "fixed" })`,
	)
	if _, err := hostWithLLM(llm).Load(context.Background(), dir); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(llm.fns) != 1 || llm.fns[0]()["X-Client-Api-Key"] != "fixed" {
		t.Fatalf("static LLM header not registered: %+v", llm.fns)
	}
}
