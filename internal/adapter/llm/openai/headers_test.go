package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// captureHeaderServer records the headers of each incoming request.
func captureHeaderServer(t *testing.T, sink *[]http.Header) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*sink = append(*sink, r.Header.Clone())
		// Minimal valid SSE so StreamChat completes cleanly.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
}

func basicReq() port.ChatRequest {
	return port.ChatRequest{Model: "m", Messages: []session.Message{
		{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "hi"}}},
	}}
}

// WithHeaders (config path A): static custom headers are sent on every request.
func TestWithHeadersStatic(t *testing.T) {
	var got []http.Header
	srv := captureHeaderServer(t, &got)
	defer srv.Close()

	c := New(srv.URL, "", WithHeaders(map[string]string{"X-Client-Api-Key": "abc123"}))
	ch, err := c.StreamChat(context.Background(), basicReq())
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if len(got) == 0 {
		t.Fatal("no request captured")
	}
	if got[0].Get("X-Client-Api-Key") != "abc123" {
		t.Errorf("X-Client-Api-Key = %q, want abc123", got[0].Get("X-Client-Api-Key"))
	}
}

// AddLLMHeaders (plugin path B): the function is re-evaluated per request, so a
// rotating value (here a counter, mimicking an SSO token) differs each call.
func TestAddLLMHeadersDynamic(t *testing.T) {
	var got []http.Header
	srv := captureHeaderServer(t, &got)
	defer srv.Close()

	n := 0
	c := New(srv.URL, "")
	c.AddLLMHeaders(func() map[string]string {
		n++
		return map[string]string{"Authorization": "Bearer token-" + strconv.Itoa(n)}
	})

	for i := 0; i < 2; i++ {
		ch, err := c.StreamChat(context.Background(), basicReq())
		if err != nil {
			t.Fatal(err)
		}
		for range ch {
		}
	}
	if len(got) < 2 {
		t.Fatalf("expected 2 requests, got %d", len(got))
	}
	if got[0].Get("Authorization") == got[1].Get("Authorization") {
		t.Errorf("dynamic header not re-evaluated: both %q", got[0].Get("Authorization"))
	}
	if got[1].Get("Authorization") != "Bearer token-2" {
		t.Errorf("second request auth = %q, want Bearer token-2", got[1].Get("Authorization"))
	}
}
