package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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

// Opt-in live-provider verification: interrupt an actual remote answer through
// a local proxy, then reconnect a transcript without resubmitting the task.
func TestRemoteProviderDisconnectRecovery(t *testing.T) {
	base, model := os.Getenv("MAGI_VERIFY_REMOTE_URL"), os.Getenv("MAGI_VERIFY_REMOTE_MODEL")
	if base == "" || model == "" {
		t.Skip("set MAGI_VERIFY_REMOTE_URL and MAGI_VERIFY_REMOTE_MODEL for a paid remote request")
	}
	var prefix string

	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		n := calls.Add(1)
		if n > 2 {
			t.Error("unexpected extra model request")
			http.Error(w, "request budget exceeded", 400)
			return
		}
		if n == 2 {
			if prefix == "" || !bytes.Contains(body, []byte(prefix)) {
				t.Error("continuation omitted saved prefix")
			}
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			t.Error(err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if key := os.Getenv("MAGI_VERIFY_REMOTE_KEY"); key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("remote request: %v", err)
			http.Error(w, "upstream unavailable", 502)
			return
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Errorf("remote HTTP status %d", response.StatusCode)
			http.Error(w, "upstream refused", response.StatusCode)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 2 {
			if _, err := io.Copy(w, response.Body); err != nil {
				t.Error(err)
			}
			return
		}
		// Only forward the first content fragment, without its finish marker.
		// The actual provider is contacted; no answer text is manufactured.
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var frame struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame) != nil {
				continue
			}
			if len(frame.Choices) == 0 || frame.Choices[0].Delta.Content == "" {
				continue
			}
			prefix = frame.Choices[0].Delta.Content
			if len(prefix) > 80 {
				prefix = string([]rune(prefix)[:min(40, len([]rune(prefix)))])
			}
			payload, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": prefix}}}})
			w.Header().Set("Content-Length", "10000000")
			fmt.Fprintf(w, "data: %s\n\n", payload)
			w.(http.Flusher).Flush()
			return
		}
		if err := scanner.Err(); err != nil {
			t.Error(err)
		}
		t.Error("remote provider returned no content")

	}))
	defer srv.Close()
	// Cancel the handler before server cleanup, including on assertion failure.
	defer cancel()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(store, openai.New(srv.URL, ""), builtin.NewRegistry(), bus.New(), nil, app.Config{ClientRole: "coding", Permission: "allow", Model: session.ModelRef{Provider: "openai", Model: model}})
	defer a.Shutdown(time.Second)
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	client := start(t, a)
	socket := client.nc.RemoteAddr().String()
	if err := client.call(Request{Method: "submit", Session: string(sid), Text: "Briefly explain why retrying a temporary network failure can preserve progress. Use plain text, no tools."}); err != nil {
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
	attached, err := Dial(socket)
	if err != nil {
		t.Fatal(err)
	}
	attached.nc.SetReadDeadline(time.Now().Add(10 * time.Second))
	seen := make(chan struct{}, 1)
	detached := make(chan error, 1)
	go func() {
		detached <- attached.Transcript(string(sid), 0, nil, func(e event.Event) bool {
			if e.Seq == cursor {
				select {
				case seen <- struct{}{}:
				default:
				}
			}
			return true
		})
	}()
	select {
	case <-seen:
	case <-ctx.Done():
		attached.Close()
		t.Fatal("transcript did not attach")
	}
	if err := attached.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-detached:
	case <-ctx.Done():
		t.Fatal("transcript did not detach")
	}
	close(release)
	reconnected, err := Dial(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	reconnected.nc.SetReadDeadline(time.Now().Add(90 * time.Second))
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
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), `"kind":"text"`) {
			final = true
		}
	}
	if !final {
		t.Fatal("reconnected transcript did not receive final answer")
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
	t.Logf("remote model=%s session=%s requests=%d prompts=%d prefix_chars=%d tail_events=%d", model, sid, calls.Load(), prompts, len([]rune(prefix)), len(tail))
	if prompts != 1 || calls.Load() != 2 {
		t.Fatalf("task replayed: prompts=%d requests=%d", prompts, calls.Load())
	}
}
