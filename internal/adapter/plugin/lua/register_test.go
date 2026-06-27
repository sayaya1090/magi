package lua

import (
	"context"
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
func (f *fakeMCPMgr) Remove(string)                                                 {}

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
