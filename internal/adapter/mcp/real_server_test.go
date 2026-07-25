package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// TestRealServerEndToEnd exercises the whole client against the reference "everything" MCP server
// over stdio: spawn → handshake → tools/list → namespaced registration → tools/call round-trip.
// Opt-in (needs npx + network to fetch the server), so it is SKIPPED unless MAGI_MCP_REAL=1 —
// it must never run in CI or a bench.
func TestRealServerEndToEnd(t *testing.T) {
	if os.Getenv("MAGI_MCP_REAL") != "1" {
		t.Skip("set MAGI_MCP_REAL=1 to run the real-server integration test (needs npx + network)")
	}
	reg := builtin.NewRegistry()
	mgr := NewManager(reg)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := mgr.AddStdio(ctx, "everything", "npx",
		[]string{"-y", "@modelcontextprotocol/server-everything"}, nil); err != nil {
		t.Fatalf("AddStdio: %v", err)
	}

	tool, ok := reg.Get("mcp__everything__echo")
	if !ok {
		var names []string
		for _, x := range reg.List() {
			names = append(names, x.Name())
		}
		t.Fatalf("mcp__everything__echo not registered; registered: %v", names)
	}
	res, err := tool.Execute(ctx, json.RawMessage(`{"message":"hello from magi"}`), port.ToolEnv{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var s string
	_ = json.Unmarshal(res.Content, &s)
	if res.IsError || !strings.Contains(s, "hello from magi") {
		t.Fatalf("echo round-trip lost the message: %q (isError=%v)", s, res.IsError)
	}
	t.Logf("real MCP echo round-trip OK: %q", s)
}
