package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A response the provider stopped at the output-token cap ends with finish_reason "length" — a
// clean stream, no error, a proper terminator. The reason was read for presence and dropped, so
// "length" and "stop" reached the caller as the same event and a truncated reply was
// indistinguishable from a finished one. Carry the value.
func TestTheFinishReasonReachesTheCaller(t *testing.T) {
	for _, reason := range []string{"length", "stop", "tool_calls"} {
		t.Run(reason, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				chunk := map[string]any{"choices": []map[string]any{{
					"delta": map[string]any{"content": "half a sen"},
				}}}
				b, _ := json.Marshal(chunk)
				_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
				fin := map[string]any{"choices": []map[string]any{{
					"delta": map[string]any{}, "finish_reason": reason,
				}}}
				b, _ = json.Marshal(fin)
				_, _ = w.Write([]byte("data: " + string(b) + "\n\n"))
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
			}))
			defer srv.Close()

			p := New(srv.URL, "k")
			ch, err := p.StreamChat(context.Background(), port.ChatRequest{
				Messages: []session.Message{{Role: session.RoleUser,
					Parts: []session.Part{{Kind: session.PartText, Text: "hi"}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			var got string
			var sawFinish bool
			for ev := range ch {
				if ev.Type == port.ProviderFinish {
					sawFinish, got = true, ev.FinishReason
				}
			}
			if !sawFinish {
				t.Fatal("no finish event")
			}
			if got != reason {
				t.Errorf("finish_reason %q arrived as %q — the caller cannot tell a cut reply from a finished one", reason, got)
			}
		})
	}
}
