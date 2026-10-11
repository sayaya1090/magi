package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/port"
)

func instructionServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "test", "version": "1"}, "instructions": body}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "read", "description": "read", "inputSchema": map[string]any{"type": "object"}, "annotations": map[string]any{"readOnlyHint": true}}}}
		default:
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
}
func requestText(req port.ChatRequest) string {
	var out strings.Builder
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			out.WriteString(p.Text)
		}
	}
	return out.String()
}
func TestMCPInitializeInstructionsReachActualModelRequestAndDetach(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "allow", ClientRole: "slack"})
	ctx := context.Background()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	manager := mcp.NewManager(a.tools.(mcp.ToolSink))
	defer manager.Close()
	a.UseToolServers(manager)
	first := instructionServer(t, "SLACK_SESSION_GUIDANCE")
	defer first.Close()
	other := instructionServer(t, "OTHER_SESSION_SECRET")
	defer other.Close()
	global := instructionServer(t, "GLOBAL_CONFIG_GUIDANCE")
	defer global.Close()
	for _, x := range []struct{ owner, name, url string }{{string(sid), "slack", first.URL}, {"other", "slack", other.URL}, {"", "foreign", global.URL}} {
		if _, err := manager.Attach(ctx, x.owner, x.name, x.url, nil); err != nil {
			t.Fatal(err)
		}
	}
	tc := turnCtx{s: a.sessionInfo(ctx, sid), agent: AgentSpec{Name: "main"}, runStart: time.Now(), guard: newRunGuard(nil)}
	req, evs := a.buildStepRequest(ctx, tc, nil, 1, 0)
	body := requestText(req)
	if !strings.Contains(body, "SLACK_SESSION_GUIDANCE") || strings.Contains(body, "OTHER_SESSION_SECRET") || strings.Contains(body, "GLOBAL_CONFIG_GUIDANCE") {
		t.Fatalf("guidance isolation/role failed: %s", body)
	}
	tc.agent.Tools = []string{"ask_user"}
	req, _ = a.buildStepRequest(ctx, tc, evs, 2, 0)
	if strings.Contains(requestText(req), "SLACK_SESSION_GUIDANCE") {
		t.Fatal("unavailable tools still expose instructions")
	}
	tc.agent = AgentSpec{Name: "main"}
	if removed, err := manager.Detach(string(sid), "slack"); err != nil || !removed {
		t.Fatalf("detach %v %v", removed, err)
	}
	req, _ = a.buildStepRequest(ctx, tc, evs, 3, 0)
	if strings.Contains(requestText(req), "SLACK_SESSION_GUIDANCE") {
		t.Fatal("detached instructions retained")
	}
}
