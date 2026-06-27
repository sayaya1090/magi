package openai

import (
	"context"
	"encoding/json"
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
		return // retries exhausted → returned error (acceptable)
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

// F-LLM-ERROR llm-err-3: invalid base URL returns error immediately.
func TestErrorBadURL(t *testing.T) {
	c := New("http://127.0.0.1:0", "") // unusable port
	_, err := c.StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	if err == nil {
		t.Fatal("llm-err-3: expected immediate error for bad URL")
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
