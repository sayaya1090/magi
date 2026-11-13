package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Break the HTTP body before its advertised length and disconnect the IDE
// socket during recovery. The next connection resumes from its saved cursor.
func TestHTTPStreamAndIDEDisconnectRecoverWithoutResubmitting(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Content-Length", "10000")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Saved partial answer\"}}]}\n\n")
			w.(http.Flusher).Flush()
			// Returning closes an incomplete HTTP body: io.ErrUnexpectedEOF.
		case 2:
			prefix := false
			for _, m := range req.Messages {
				if m.Role == "assistant" && strings.Contains(string(m.Content), "Saved partial answer") {
					prefix = true
				}
			}
			if !prefix {
				t.Error("continuation request lost saved prefix")
			}
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Recovered final answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		default:
			t.Error("unexpected replay of model request")
			http.Error(w, "unexpected replay", 400)
		}
	}))
	defer srv.Close()
	// Cancel the handler before server cleanup, including on assertion failure.
	defer cancel()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(store, openai.New(srv.URL, ""), builtin.Default(), bus.New(), nil, app.Config{ClientRole: "coding", Permission: "allow", Model: session.ModelRef{Provider: "openai", Model: "test"}})
	defer a.Shutdown(time.Second)
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	client := start(t, a)
	socket := client.nc.RemoteAddr().String()
	if err := client.call(Request{Method: "submit", Session: string(sid), Text: "complete the explanation"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("HTTP recovery did not start")
	}
	history, err := client.History(string(sid))
	if err != nil {
		t.Fatal(err)
	}
	var cursor int64
	for _, e := range history {
		if e.Seq > cursor {
			cursor = e.Seq
		}
	}
	if cursor == 0 {
		t.Fatal("no durable progress before disconnect")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	close(release)
	reconnected, err := Dial(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	reconnected.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	var tail []event.Event
	err = reconnected.Transcript(string(sid), cursor, func(why string) { t.Errorf("unexpected replay reset: %s", why) }, func(e event.Event) bool {
		if e.Seq > 0 && e.Seq <= cursor {
			t.Errorf("duplicated durable event %d <= %d", e.Seq, cursor)
		}
		tail = append(tail, e)
		return e.Type != event.TypeTurnFinished
	})
	if err != nil {
		t.Fatal(err)
	}
	final := false
	for _, e := range tail {
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "Recovered final answer") {
			final = true
		}
	}
	if !final {
		t.Fatal("reconnected IDE did not receive final answer")
	}
	all, err := store.Read(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	prompts := 0
	for _, e := range all {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorUser {
			prompts++
		}
	}
	if prompts != 1 || calls.Load() != 2 {
		t.Fatalf("task replayed: prompts=%d requests=%d", prompts, calls.Load())
	}
}
