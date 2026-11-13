package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/sayaya1090/magi/internal/port"
)

// Use the same submit wire shape as JetBrains, a real app loop and HTTP adapter.
// The fake endpoint calls Mermaid, then checks its result on the next request.
func TestIDEMermaidRoundTripThroughCustomEndpoint(t *testing.T) {
	for _, injected := range []bool{false, true} {
		t.Run(fmt.Sprintf("injected=%v", injected), func(t *testing.T) { testIDEMermaidEndpoint(t, injected) })
	}
}

func testIDEMermaidEndpoint(t *testing.T, injected bool) {
	t.Helper()
	observed := make(chan bool, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
			Messages []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		found := false
		for _, tool := range body.Tools {
			if tool.Function.Name == "mermaid" {
				found = true
			}
		}
		if !found || body.Model != "custom-model" {
			t.Errorf("request: mermaid=%v model=%q", found, body.Model)
		}
		if injected && r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("session credentials were not used")
		}
		result := false
		for _, m := range body.Messages {
			if m.Role == "tool" && strings.Contains(string(m.Content), "flowchart TD") {
				result = true
			}
		}
		select {
		case observed <- result:
		default:
			t.Error("too many requests")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if result {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"```mermaid\\nflowchart TD\\nA-->B\\n```\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"diagram1","type":"function","function":{"name":"mermaid","arguments":"{\"title\":\"Flow\",\"source\":\"flowchart TD\\nA-->B\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
		}
	}))
	defer srv.Close()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := srv.URL
	if injected {
		base = "http://127.0.0.1:1"
	} // falling back to the default must fail
	a := app.New(store, openai.New(base, ""), builtin.Default(), bus.New(), nil, app.Config{
		ClientRole: "coding", Permission: "allow", MaxSteps: 3,
		Model:       session.ModelRef{Provider: "openai", Model: "custom-model"},
		NewProvider: func(p app.ProfileDef) port.LLMProvider { return openai.New(p.BaseURL, p.APIKey) },
	})
	defer a.Shutdown(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if injected {
		if err := a.ApplySessionProfile(sid, app.ProfileDef{BaseURL: srv.URL, APIKey: "test-key", Model: "custom-model"}); err != nil {
			t.Fatal(err)
		}
	}
	sub, unsubscribe, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unsubscribe()
	client := start(t, a)
	if err := client.call(Request{Method: "submit", Session: string(sid), Text: "Explain A to B with Mermaid."}); err != nil {
		t.Fatal(err)
	}
	sawDiagram := false
	for {
		select {
		case e, ok := <-sub:
			if !ok {
				t.Fatal("event stream closed before turn finished")
			}
			if e.Type == event.TypePartAppended {
				var d event.PartAppendedData
				if json.Unmarshal(e.Data, &d) == nil && d.Role == session.RoleAssistant && strings.Contains(d.Part.Text, "```mermaid") {
					sawDiagram = true
				}
			}
			if e.Type == event.TypeError {
				t.Fatalf("loop error: %s", e.Data)
			}
			if e.Type == event.TypeTurnFinished {
				if !sawDiagram {
					t.Fatal("final diagram was not published to the client")
				}
				select {
				case first := <-observed:
					if first {
						t.Fatal("unexpected initial tool result")
					}
				default:
					t.Fatal("no HTTP request")
				}
				select {
				case second := <-observed:
					if !second {
						t.Fatal("Mermaid result did not reach endpoint")
					}
				default:
					t.Fatal("no follow-up HTTP request")
				}
				return
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
