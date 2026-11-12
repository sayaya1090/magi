package openai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// The adapter states what it knows in structure, so the harness decides recovery on facts and not
// on wording: kind, status, the server's Retry-After and its request id all ride the error.
func TestRefusalCarriesStructuredFacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.Header().Set("X-Request-Id", "req_abc")
		http.Error(w, `{"error":{"message":"slow down"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	be, ok := port.AsBackendError(err)
	if !ok {
		t.Fatalf("want a port.BackendError, got %T: %v", err, err)
	}
	if be.Kind != port.KindRateLimit || be.Status != 429 || be.RetryAfter != time.Second || be.RequestID != "req_abc" {
		t.Errorf("facts lost: %+v", *be)
	}
}

// The same status means two things: a rate window clears in seconds, an exhausted quota does not.
func TestQuotaRefusalIsNotARateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		http.Error(w, `{"error":{"type":"insufficient_quota","message":"check your billing"}}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	if be, ok := port.AsBackendError(err); !ok || be.Kind != port.KindQuota {
		t.Fatalf("want kind quota, got %+v (%v)", be, err)
	}
}

// A non-retryable status reaches the stream as a structured ProviderError too.
func TestStreamRefusalIsStructured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Request-Id", "req_401")
		http.Error(w, `{"error":{"message":"bad key"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	ch, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var got error
	for ev := range ch {
		if ev.Type == port.ProviderError {
			got = ev.Err
		}
	}
	be, ok := port.AsBackendError(got)
	if !ok || be.Kind != port.KindAuth || be.Status != 401 || be.RequestID != "req_401" {
		t.Fatalf("want structured auth refusal, got %+v (%v)", be, got)
	}
}

// An error frame (the shape every plugin backend uses) may name its type; when it does, that is the
// kind. When it does not, the kind is left for the harness to read from the words.
func TestErrorFrameTypeIsRead(t *testing.T) {
	for _, tc := range []struct {
		frame string
		want  port.BackendErrorKind
		after time.Duration
	}{
		{`{"error":{"type":"overloaded_error","message":"Overloaded"}}`, port.KindOverloaded, 0},
		{`{"error":{"type":"rate_limit_error","message":"x","retry_after":7,"request_id":"req_9"}}`, port.KindRateLimit, 7 * time.Second},
		{`{"error":{"message":"claude exited before answering"}}`, "", 0},
	} {
		frame := tc.frame
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", frame)
		}))
		ch, err := New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{Model: "m"})
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}
		var got error
		for ev := range ch {
			if ev.Type == port.ProviderError {
				got = ev.Err
			}
		}
		srv.Close()
		be, ok := port.AsBackendError(got)
		if !ok || be.Kind != tc.want || be.RetryAfter != tc.after {
			t.Errorf("%s: got %+v (%v)", frame, be, got)
		}
	}
}
