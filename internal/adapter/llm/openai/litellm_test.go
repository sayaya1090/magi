package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Duplicated/concatenated tool arguments ("{…}{…}") are reduced to the first
// valid JSON value (minimax-via-LiteLLM behavior).
func TestFirstJSONValue(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"path":"a.go"}`, `{"path":"a.go"}`},
		{`{"path":"a.go"}{"path":"a.go"}`, `{"path":"a.go"}`},
		{`{"a":1}` + "\n" + `{"a":1}`, `{"a":1}`},
		{`{"a":1}  garbage`, `{"a":1}`},
	}
	for _, c := range cases {
		got := string(firstJSONValue([]byte(c.in)))
		if got != c.want {
			t.Errorf("firstJSONValue(%q)=%q want %q", c.in, got, c.want)
		}
	}
	// A partial fragment is left as-is (can't decode a full value).
	if got := string(firstJSONValue([]byte(`{"a":`))); got != `{"a":` {
		t.Errorf("partial should pass through, got %q", got)
	}
}

// An assistant message carrying only tool_calls must serialize an explicit empty
// content (not omit it / send null), which some gateways reject.
func TestAssistantToolCallContentNotNull(t *testing.T) {
	req := buildRequest(port.ChatRequest{
		Model: "m",
		Messages: []session.Message{{
			Role:  session.RoleAssistant,
			Parts: []session.Part{{Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c1", Name: "read", Args: json.RawMessage(`{"path":"x"}`)}}},
		}},
	}, false, false)
	b, _ := json.Marshal(req)
	s := string(b)
	if !strings.Contains(s, `"content":""`) {
		t.Errorf("assistant tool-call message must have explicit empty content; got %s", s)
	}
	if strings.Contains(s, `"content":null`) {
		t.Errorf("content must not be null: %s", s)
	}
}

// With caching on, the system prompt + tools carry cache_control breakpoints;
// off, they don't (default, so non-Anthropic backends are unaffected).
func TestPromptCacheBreakpoints(t *testing.T) {
	req := port.ChatRequest{
		Model:  "m",
		System: "you are a long stable system prompt",
		Tools:  []port.ToolSpec{{Name: "read"}, {Name: "edit"}},
	}
	on, _ := json.Marshal(buildRequest(req, true, true))
	if !jsonContains(on, `"cache_control"`) || !jsonContains(on, `"ephemeral"`) {
		t.Errorf("cache on: expected cache_control/ephemeral; got %s", on)
	}
	// system content becomes an array of blocks (not a bare string).
	if !jsonContains(on, `"type":"text"`) {
		t.Errorf("cache on: system should be content blocks; got %s", on)
	}

	off, _ := json.Marshal(buildRequest(req, true, false))
	if jsonContains(off, `"cache_control"`) {
		t.Errorf("cache off: must NOT emit cache_control; got %s", off)
	}
	if !jsonContains(off, `"content":"you are a long stable system prompt"`) {
		t.Errorf("cache off: system should be a plain string; got %s", off)
	}
}

func jsonContains(b []byte, sub string) bool { return strings.Contains(string(b), sub) }

// Tool results are unwrapped from their JSON-string storage (no double quoting).
func TestToolResultContentUnwrapped(t *testing.T) {
	raw, _ := json.Marshal("3 matches in a.go")
	if got := toolResultContent(raw); got != "3 matches in a.go" {
		t.Errorf("toolResultContent=%q want plain text", got)
	}
}

// A backend that rejects the cache shape (400) makes the client fall back to a
// plain (uncached) request transparently, and stick to plain afterward.
func TestPromptCacheAutoFallback(t *testing.T) {
	var cachedSeen, plainSeen int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "cache_control") {
			cachedSeen++
			http.Error(w, `{"error":{"message":"unsupported content"}}`, http.StatusBadRequest)
			return
		}
		plainSeen++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "", WithPromptCache())
	// A request with a system prompt so caching actually attaches cache_control.
	drain := func() string {
		ch, err := c.StreamChat(context.Background(), port.ChatRequest{Model: "m", System: "stable system"})
		if err != nil {
			t.Fatalf("StreamChat: %v", err)
		}
		var s string
		for e := range ch {
			if e.Type == port.ProviderText {
				s += e.Text
			}
		}
		return s
	}
	// First call: tries cache → 400 → falls back to plain → "ok".
	if txt := drain(); txt != "ok" {
		t.Fatalf("first call text=%q want ok (after fallback)", txt)
	}
	if cachedSeen != 1 || plainSeen != 1 {
		t.Fatalf("first call: cached=%d plain=%d, want 1/1", cachedSeen, plainSeen)
	}
	// Second call: cache is now sticky-off → goes straight to plain (no 400).
	if txt := drain(); txt != "ok" {
		t.Fatalf("second call text=%q want ok", txt)
	}
	if cachedSeen != 1 {
		t.Errorf("second call retried cache (cachedSeen=%d); should be sticky-off", cachedSeen)
	}
}

// ListModels parses the gateway's /models catalog.
func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("expected GET /models, got %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet"},{"id":"gpt-4o"},{"id":"minimax-m3"}]}`))
	}))
	defer srv.Close()
	ids, err := New(srv.URL, "k").ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if strings.Join(ids, ",") != "claude-sonnet,gpt-4o,minimax-m3" {
		t.Errorf("ids=%v", ids)
	}
}
