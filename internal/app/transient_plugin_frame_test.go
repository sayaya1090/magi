package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/port"
)

// The plugins no longer resend when their backend CLI dies before answering: they report it as
// an error frame and the harness owns recovery. These are the frames the claudecode and
// antigravity plugins sent through the real Lua plugin host (2026-11-13, a fake CLI exiting 3),
// read back through the real OpenAI adapter: the harness must take each for a network failure
// it retries, not an unknown one that ends the turn.
func TestAPluginsDeadChildFrameIsARetryableNetworkFailure(t *testing.T) {
	for _, frame := range []string{
		`{"error":{"message":"claude exited before answering: child exited: exit status 3"}}`,
		`{"error":{"message":"agy exited before answering: child exited: exit status 3"}}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", frame)
		}))
		ch, err := openai.New(srv.URL, "").StreamChat(context.Background(), port.ChatRequest{Model: "m"})
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
		if got == nil {
			t.Fatalf("%s: the adapter reported no error", frame)
		}
		if c := classifyBackend(got); c.Kind != port.KindNetwork || !c.Kind.Retryable() {
			t.Errorf("%s: read as %q (%v), want a retryable network failure", frame, c.Kind, got)
		}
	}
}
