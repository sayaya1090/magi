package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// TestMain lets the test binary act as a fake MCP server when re-exec'd, so the
// Manager test can spawn a real subprocess.
func TestMain(m *testing.M) {
	if os.Getenv("MAGI_FAKE_MCP") == "1" {
		runFakeServer(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// inMemoryClient wires a Client to an in-process fake server via pipes.
func inMemoryClient(t *testing.T) *Client {
	t.Helper()
	clientReads, serverWrites := io.Pipe()
	serverReads, clientWrites := io.Pipe()
	go runFakeServer(serverReads, serverWrites)
	c := newClient(clientReads, clientWrites, nil)
	t.Cleanup(func() { c.Close() })
	return c
}

// F-MCP: handshake + tools/list discovery over the stdio protocol.
func TestInitializeAndList(t *testing.T) {
	c := inMemoryClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools=%+v want [echo]", tools)
	}
}

// F-MCP: tools/call round-trip.
func TestCallTool(t *testing.T) {
	c := inMemoryClient(t)
	ctx := context.Background()
	if err := c.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := c.CallTool(ctx, "echo", json.RawMessage(`{"text":"hi"}`))
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError || len(res.Content) != 1 || res.Content[0].Text != "echo: hi" {
		t.Fatalf("result=%+v want 'echo: hi'", res)
	}
}

// F-MCP: Manager spawns a real subprocess, registers its tools, and the bridged
// tool is callable through port.Tool.
func TestManagerSpawnAndRegister(t *testing.T) {
	reg := builtin.NewRegistry()
	mgr := NewManager(reg)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.AddStdio(ctx, "fake", os.Args[0], nil, []string{"MAGI_FAKE_MCP=1"}); err != nil {
		t.Fatalf("AddStdio: %v", err)
	}

	tool, ok := reg.Get("mcp__fake__echo")
	if !ok {
		t.Fatal("echo tool not registered from MCP server")
	}
	res, err := tool.Execute(ctx, json.RawMessage(`{"text":"world"}`), port.ToolEnv{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	if s != "echo: world" {
		t.Errorf("tool result=%q want 'echo: world'", s)
	}
}

// F-MCP: when the server process exits, its tools are unregistered.
func TestServerExitUnregisters(t *testing.T) {
	reg := builtin.NewRegistry()
	mgr := NewManager(reg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.AddStdio(ctx, "fake", os.Args[0], nil, []string{"MAGI_FAKE_MCP=1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("mcp__fake__echo"); !ok {
		t.Fatal("echo should be registered")
	}

	mgr.Remove("fake") // simulates teardown / server gone

	if _, ok := reg.Get("mcp__fake__echo"); ok {
		t.Error("echo should be unregistered after server removal")
	}
}

func TestNamespacedToolName(t *testing.T) {
	cases := []struct{ server, tool, want string }{
		{"fake", "echo", "mcp__fake__echo"},
		{"my-server", "read_file", "mcp__my-server__read_file"},
		{"a b", "x.y", "mcp__a_b__x_y"}, // spaces/dots → '_'
		{"", "", "mcp__x__x"},           // empty parts → placeholder, never a bare "mcp____"
	}
	for _, c := range cases {
		if got := namespacedToolName(c.server, c.tool); got != c.want {
			t.Errorf("namespacedToolName(%q,%q)=%q want %q", c.server, c.tool, got, c.want)
		}
	}
}

// A stub builtin tool that records its identity, to prove an MCP tool did not overwrite it.
type stubTool struct{ id string }

func (s stubTool) Name() string            { return "echo" }
func (s stubTool) Description() string     { return s.id }
func (s stubTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s stubTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{}, nil
}

// Namespacing keeps an MCP tool from SHADOWING an identically-named builtin: with a builtin "echo"
// already registered, an MCP server that also advertises "echo" must land under mcp__fake__echo and
// leave the builtin under "echo" untouched (the registry replaces by name).
func TestMCPToolDoesNotShadowBuiltin(t *testing.T) {
	reg := builtin.NewRegistry()
	reg.Register(stubTool{id: "the-builtin"})
	mgr := NewManager(reg)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.AddStdio(ctx, "fake", os.Args[0], nil, []string{"MAGI_FAKE_MCP=1"}); err != nil {
		t.Fatal(err)
	}
	b, ok := reg.Get("echo")
	if !ok || b.Description() != "the-builtin" {
		t.Errorf("builtin 'echo' was shadowed by the MCP tool: %+v (ok=%v)", b, ok)
	}
	if _, ok := reg.Get("mcp__fake__echo"); !ok {
		t.Error("the MCP echo should be reachable under its namespaced name")
	}
}
