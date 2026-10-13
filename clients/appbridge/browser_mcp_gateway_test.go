package appbridge

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrowserMCPIsOptionalAndPinsItsWindow(t *testing.T) {
	calls := 0
	s, err := New(Config{App: "browser", Token: "secret", ConfigDir: t.TempDir(), Tools: []Tool{{Name: "read_page", InputSchema: map[string]any{"type": "object"}}}, Call: func(context.Context, string, string, json.RawMessage) (any, error) {
		calls++
		return map[string]any{"text": "page"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	request := func(session, method string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":{"name":"read_page","arguments":{}}}`))
		r.Header.Set("Authorization", "Bearer secret")
		r.Header.Set("Mcp-Session-Id", session)
		w := httptest.NewRecorder()
		s.Mux.ServeHTTP(w, r)
		return w
	}
	if w := request("", "initialize"); w.Code != 409 {
		t.Fatal("missing browser unexpectedly available", w.Code)
	}
	s.bindings["one"] = &Binding{ID: "one", Session: "s1", Target: "w1"}
	w := request("", "initialize")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	sid := w.Header().Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("missing transport session")
	}
	if w = request(sid, "tools/list"); w.Code != 200 || !strings.Contains(w.Body.String(), "read_page") {
		t.Fatal(w.Body)
	}
	s.bindings["two"] = &Binding{ID: "two", Session: "s2", Target: "w2"}
	if w = request("", "initialize"); w.Code != 409 {
		t.Fatal("ambiguous windows accepted")
	}
	if w = request(sid, "tools/call"); w.Code != 200 || calls != 1 {
		t.Fatal("pinned window unavailable", w.Body)
	}
	s.bindings["one"].Session = "new"
	if w = request(sid, "tools/call"); w.Code != 409 || calls != 1 {
		t.Fatal("old MCP transport followed new session")
	}
}
