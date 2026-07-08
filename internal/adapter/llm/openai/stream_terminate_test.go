package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// heldSSEServer streams body, flushes it, then holds the connection open until the
// client disconnects — reproducing a backend that sends a complete response but
// never emits a trailing `data: [DONE]` nor closes the socket. Without the
// finish-driven termination this strands the reader until the request ctx dies.
func heldSSEServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done() // hold open; unblocks only when the client disconnects
	}))
}

// drain collects events from a StreamChat channel and reports whether it finished
// (channel closed) before the deadline — i.e. consume() did not hang.
func drain(t *testing.T, c *Client, within time.Duration) (evs []port.ProviderEvent, ok bool) {
	t.Helper()
	ch, err := c.StreamChat(context.Background(), port.ChatRequest{Model: "m"})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	done := make(chan []port.ProviderEvent, 1)
	go func() {
		var out []port.ProviderEvent
		for e := range ch {
			out = append(out, e)
		}
		done <- out
	}()
	select {
	case out := <-done:
		return out, true
	case <-time.After(within):
		return nil, false
	}
}

// A complete response (finish_reason + usage) followed by silence must terminate
// immediately, without waiting for `[DONE]` — the regression that caused the
// multi-minute post-answer hangs against Ollama cloud gateways.
func TestFinishStopsWithoutDone(t *testing.T) {
	body := "data: {\"choices\":[{\"delta\":{\"content\":\"done\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":3}}\n\n"
	// deliberately NO "data: [DONE]" and the server holds the connection open.
	srv := heldSSEServer(t, body)
	defer srv.Close()

	evs, ok := drain(t, New(srv.URL, ""), 2*time.Second)
	if !ok {
		t.Fatal("StreamChat hung waiting for [DONE] after a complete response")
	}
	var text string
	var finishes, usage int
	for _, e := range evs {
		switch e.Type {
		case port.ProviderText:
			text += e.Text
		case port.ProviderFinish:
			finishes++
		case port.ProviderUsage:
			usage++
			if e.Usage == nil || e.Usage.In != 11 {
				t.Errorf("usage=%+v want In=11", e.Usage)
			}
		case port.ProviderError:
			t.Fatalf("unexpected error event: %v", e.Err)
		}
	}
	if text != "done" || finishes != 1 || usage != 1 {
		t.Errorf("text=%q finishes=%d usage=%d; want done/1/1", text, finishes, usage)
	}
}

// finish_reason with neither a usage chunk nor `[DONE]` must still terminate,
// bounded by the epilogue grace, instead of blocking until the request ctx.
func TestFinishStopsWithoutUsageOrDone(t *testing.T) {
	restore := streamEpilogueGrace
	streamEpilogueGrace = 150 * time.Millisecond
	defer func() { streamEpilogueGrace = restore }()

	body := "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"
	srv := heldSSEServer(t, body)
	defer srv.Close()

	evs, ok := drain(t, New(srv.URL, ""), 2*time.Second)
	if !ok {
		t.Fatal("StreamChat hung: epilogue backstop did not unwind the read")
	}
	var finishes int
	for _, e := range evs {
		switch e.Type {
		case port.ProviderFinish:
			finishes++
		case port.ProviderError:
			// A post-finish read error must NOT be surfaced (the answer is complete).
			t.Fatalf("post-finish error surfaced to loop: %v", e.Err)
		}
	}
	if finishes != 1 {
		t.Errorf("finishes=%d want 1", finishes)
	}
}
