package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// sseServer returns a test server that streams the given raw SSE body.
func sseServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
}

func collect(t *testing.T, c *Client) []port.ProviderEvent {
	t.Helper()
	ch, err := c.StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var evs []port.ProviderEvent
	for e := range ch {
		evs = append(evs, e)
	}
	return evs
}

// F-LLM-SSE sse-1..4: text deltas, [DONE], malformed line skipped.
func TestSSEParsing(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n" +
		"data: {bad json\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	srv := sseServer(t, body)
	defer srv.Close()

	evs := collect(t, New(srv.URL, ""))

	var text string
	var finishes int
	for _, e := range evs {
		switch e.Type {
		case port.ProviderText:
			text += e.Text
		case port.ProviderFinish:
			finishes++
		case port.ProviderError:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}
	if text != "Hello" {
		t.Errorf("text=%q want Hello", text)
	}
	if finishes != 1 {
		t.Errorf("finish count=%d want 1", finishes)
	}
}

// F-LLM-TOOLS-NATIVE native-1: a single tool_call is parsed.
func TestNativeToolCall(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\\\"x\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	srv := sseServer(t, body)
	defer srv.Close()

	evs := collect(t, New(srv.URL, ""))

	var found *port.ProviderEvent
	for i := range evs {
		if evs[i].Type == port.ProviderToolCall {
			found = &evs[i]
		}
	}
	if found == nil {
		t.Fatal("native-1: no tool-call event")
	}
	if found.ToolCall.Name != "read" {
		t.Errorf("native-1: name=%q want read", found.ToolCall.Name)
	}
	var args map[string]string
	_ = json.Unmarshal(found.ToolCall.Args, &args)
	if args["path"] != "x" {
		t.Errorf("native-1: args=%s want {path:x}", found.ToolCall.Args)
	}
}

// Two PARALLEL tool calls (distinct indices) in one stream must both be assembled and
// emitted in index order — exercises the multi-index accumulator, not just index 0.
func TestNativeParallelToolCalls(t *testing.T) {
	body := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read","arguments":"{\"path\":\"a\"}"}}]}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"grep","arguments":"{\"q\":\"b\"}"}}]}}]}` + "\n\n" +
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\n" +
		"data: [DONE]\n\n"
	srv := sseServer(t, body)
	defer srv.Close()

	var names []string
	argsByName := map[string]string{}
	for _, e := range collect(t, New(srv.URL, "")) {
		if e.Type == port.ProviderToolCall {
			names = append(names, e.ToolCall.Name)
			argsByName[e.ToolCall.Name] = string(e.ToolCall.Args)
		}
	}
	if len(names) != 2 || names[0] != "read" || names[1] != "grep" {
		t.Fatalf("parallel tool calls = %v, want [read grep] in order", names)
	}
	if !strings.Contains(argsByName["read"], `"path":"a"`) || !strings.Contains(argsByName["grep"], `"q":"b"`) {
		t.Errorf("args not assembled per index: %v", argsByName)
	}
}

// F-LLM-TOOLS-NATIVE native-2: arguments split across multiple chunks.
func TestNativeToolCallSplitArgs(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"type\":\"function\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"pa\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"th\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"x\\\"}\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	srv := sseServer(t, body)
	defer srv.Close()

	evs := collect(t, New(srv.URL, ""))

	var calls int
	var args map[string]string
	for _, e := range evs {
		if e.Type == port.ProviderToolCall {
			calls++
			_ = json.Unmarshal(e.ToolCall.Args, &args)
		}
	}
	if calls != 1 {
		t.Fatalf("native-2: tool-call count=%d want 1", calls)
	}
	if args["path"] != "x" {
		t.Errorf("native-2: reassembled args=%v want {path:x}", args)
	}
}

// F-LLM-ERROR llm-err-1: a persistent server error surfaces as an error — either
// returned (after retries are exhausted) or as a stream error event.
func TestErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	if err != nil {
		// retries exhausted → returned error: it must carry the 500 status, not a bare
		// "request failed" that hides what went wrong.
		if !strings.Contains(err.Error(), "500") {
			t.Fatalf("exhausted-retry error should carry the 500 status, got %v", err)
		}
		return
	}
	var sawErr bool
	for e := range ch {
		if e.Type == port.ProviderError {
			sawErr = true
		}
	}
	if !sawErr {
		t.Fatal("llm-err-1: expected an error (returned or event)")
	}
}

// Harmony tool-call misparse recovery: Ollama's gpt-oss parser 500s
// ("error parsing tool call: raw=<prose>") when the model answers in prose but the
// server tries to read it as a tool call. Because the request is identical across
// HTTP retries the 500 is deterministic, so StreamChat must retry ONCE with the
// tools array stripped — with no tools the server skips tool-call parsing and the
// same prose comes back as normal content, instead of hard-aborting the turn.
func TestHarmonyToolParseRetryWithoutTools(t *testing.T) {
	var withTools, withoutTools int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if strings.Contains(string(b), "\"tools\"") {
			withTools++
			http.Error(w, `{"error":{"message":"error parsing tool call: raw='**Summary:** the answer'"}}`, http.StatusInternalServerError)
			return
		}
		withoutTools++
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"the recovered answer\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	ch, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{
		Model: "m",
		Tools: []port.ToolSpec{{Name: "read"}},
	})
	if err != nil {
		t.Fatalf("StreamChat should recover by dropping tools, not error: %v", err)
	}
	var text string
	for e := range ch {
		switch e.Type {
		case port.ProviderText:
			text += e.Text
		case port.ProviderError:
			t.Fatalf("unexpected error event after tools-stripped retry: %v", e.Err)
		}
	}
	if text != "the recovered answer" {
		t.Fatalf("expected the prose answer to be recovered, got %q", text)
	}
	if withTools == 0 || withoutTools != 1 {
		t.Fatalf("expected a with-tools 500 then exactly one tools-stripped retry, got withTools=%d withoutTools=%d", withTools, withoutTools)
	}
}

// A non-tool-parse 5xx (a genuine outage) must NOT trigger the tools-stripped retry —
// it should surface as an error so the real failure is diagnosable, not masked.
func TestGenericServerErrorNotToolStripped(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "\"tools\"") {
			t.Errorf("a generic 5xx must not be retried without tools; got a tools-stripped request")
		}
		reqs++
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ch, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{
		Model: "m",
		Tools: []port.ToolSpec{{Name: "read"}},
	})
	sawErr := err != nil
	if ch != nil {
		for e := range ch {
			if e.Type == port.ProviderError {
				sawErr = true
			}
		}
	}
	if !sawErr {
		t.Fatal("a persistent generic 500 must surface as an error")
	}
}

// F-LLM-ERROR llm-err-3: invalid base URL returns error immediately.
func TestErrorBadURL(t *testing.T) {
	c := New("http://127.0.0.1:0", "") // unusable port
	_, err := c.StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("llm-err-3: expected immediate error for bad URL")
	}
}

// SetBaseURL redirects requests at runtime (plugin magi.set_base_url); an empty
// string clears the override and restores the configured backend. This is what makes
// a loopback proxy a plugin serves with magi.serve actually receive the agent's traffic.
func TestSetBaseURLOverride(t *testing.T) {
	const sse = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	mk := func(hit *bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*hit = true
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(sse))
		}))
	}
	var hitA, hitB bool
	a := mk(&hitA)
	defer a.Close()
	b := mk(&hitB)
	defer b.Close()

	c := New(a.URL, "")
	c.SetBaseURL(b.URL)
	collect(t, c)
	if hitA || !hitB {
		t.Fatalf("override should route to B only (a=%v b=%v)", hitA, hitB)
	}

	hitA, hitB = false, false
	c.SetBaseURL("") // clear → back to the configured backend
	collect(t, c)
	if !hitA || hitB {
		t.Fatalf("cleared override should route back to A (a=%v b=%v)", hitA, hitB)
	}
}

// A trailing slash on the override is trimmed so base()+"/chat/completions" stays well-formed.
func TestSetBaseURLTrimsSlash(t *testing.T) {
	c := New("http://configured.example/v1", "")
	c.SetBaseURL("http://override.example/v1/")
	if got := c.base(); got != "http://override.example/v1" {
		t.Errorf("base() = %q, want trailing slash trimmed", got)
	}
	c.SetBaseURL("")
	if got := c.base(); got != "http://configured.example/v1" {
		t.Errorf("empty override should restore configured base, got %q", got)
	}
}

// ClearBaseURL releases the override only when the token still identifies the current one:
// a non-owning token is a no-op, and a newer SetBaseURL with the SAME url (a hot-reload's new
// instance) supersedes the old token so the outgoing instance's release can't revert the
// redirect — the multi-instance localhost-revert bug.
func TestClearBaseURL(t *testing.T) {
	c := New("http://configured.example/v1", "")
	tok := c.SetBaseURL("http://127.0.0.1:1/v1")

	c.ClearBaseURL(tok + 999) // not the current owner → no-op
	if c.base() != "http://127.0.0.1:1/v1" {
		t.Fatalf("non-owning clear changed base: %q", c.base())
	}
	c.ClearBaseURL(tok) // owner → restore configured
	if c.base() != "http://configured.example/v1" {
		t.Errorf("owning clear should restore configured base, got %q", c.base())
	}

	// Reload self-clobber guard: a newer Set with the same url takes ownership; the old
	// token must NOT clear it.
	old := c.SetBaseURL("http://gw.example/v1")
	c.SetBaseURL("http://gw.example/v1") // reload re-installs, new token
	c.ClearBaseURL(old)                  // old instance closes → must be a no-op
	if c.base() != "http://gw.example/v1" {
		t.Errorf("stale token cleared a re-installed override, got %q", c.base())
	}
}

// Transient 503s are retried, then the request succeeds.
func TestRetryOnTransient(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		if n < 3 {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	evs := collect(t, New(srv.URL, ""))
	var text string
	for _, e := range evs {
		if e.Type == port.ProviderText {
			text += e.Text
		}
	}
	if text != "ok" {
		t.Errorf("text=%q want ok (after retries)", text)
	}
	if n != 3 {
		t.Errorf("attempts=%d want 3", n)
	}
}

// When retries are exhausted on a retryable status (e.g. a 429 rate limit), the
// returned error must carry the status + body so the failure is diagnosable —
// not a generic "request failed".
func TestRetryExhaustedSurfacesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		http.Error(w, `{"error":{"message":"rate limit exceeded","code":429}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error should surface the 429 status and body, got: %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("2"); d != 2*time.Second {
		t.Errorf("parseRetryAfter(2)=%v want 2s", d)
	}
	if d := parseRetryAfter("999"); d != 15*time.Second {
		t.Errorf("parseRetryAfter caps at 15s, got %v", d)
	}
	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("parseRetryAfter('')=%v want 0", d)
	}
	if d := parseRetryAfter("garbage"); d != 0 {
		t.Errorf("parseRetryAfter(garbage)=%v want 0", d)
	}
}
