package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/adapter/platform"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// TestE2EFullLoop drives the entire pipeline against a real model: the agent
// must call the write tool to create a file. This is the integration that mock
// fixtures cannot cover (memory: local-model tool-calling bug).
//
// Configure via MAGI_E2E_OLLAMA_BASE (default localhost) + _MODEL. A model
// strong at tool-calling is recommended, e.g. MAGI_E2E_OLLAMA_MODEL=qwen2.5-coder:32b.
func TestE2EFullLoop(t *testing.T) {
	base := os.Getenv("MAGI_E2E_OLLAMA_BASE")
	if base == "" {
		base = "http://localhost:11434/v1"
	}
	model := os.Getenv("MAGI_E2E_OLLAMA_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
	}
	// The model is resolved BEFORE the gate, because the gate now asks about it — see serves.
	if base == "disabled" || !serves(base, model) {
		t.Skipf("ollama does not serve %s at %s", model, base)
	}

	wd := t.TempDir()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	llm := openai.New(base, os.Getenv("MAGI_E2E_API_KEY"))
	a := New(store, llm, builtin.Default(), bus.New(), platform.New(), Config{
		Model:      session.ModelRef{Provider: "openai", Model: model},
		System:     "You are a coding agent operating in a working directory. To create files you MUST call the write tool with {path, content}. Do not print file contents in prose.",
		Permission: "allow",
		MaxSteps:   6,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd, Model: session.ModelRef{Provider: "openai", Model: model}})
	if err != nil {
		t.Fatal(err)
	}

	sub, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()

	if err := a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "Create a file named hello.txt containing the text: magi works"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "test"},
	}); err != nil {
		t.Fatal(err)
	}

	var sawToolResult bool
	for {
		select {
		case e, ok := <-sub:
			if !ok {
				t.Fatal("stream closed before completion")
			}
			if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), `"kind":"tool-result"`) {
				sawToolResult = true
			}
			if e.Type == event.TypeTurnFinished {
				goto done
			}
			if e.Type == event.TypeError {
				t.Fatalf("loop error: %s", string(e.Data))
			}
		case <-ctx.Done():
			t.Fatal("timeout waiting for turn to finish")
		}
	}
done:
	if !sawToolResult {
		t.Errorf("expected the agent to call a tool (write), but no tool-result was produced")
	}
	// The file must actually exist on disk.
	if b, err := os.ReadFile(wd + "/hello.txt"); err != nil {
		t.Errorf("hello.txt not created: %v", err)
	} else if len(b) == 0 {
		t.Errorf("hello.txt is empty")
	} else {
		t.Logf("hello.txt created: %q", string(b))
	}
}

// serves reports whether base is up AND willing to serve model.
//
// ⚠ **Reachable is not enough, and the difference is a red suite.** The comment above promises that
// plain `go test ./...` stays green on a machine without models; it was only true when nothing
// answered at all. With ollama RUNNING and this model not pulled, the endpoint answers `/models`
// happily and every request then comes back `404 model '…' not found` — measured 2026-09-13 on this
// machine: tests in this tree red for a model nobody asked for. A suite red for its environment is
// one people learn to ignore, which is how a real regression gets through.
//
// A listing this cannot READ is not a refusal: some OpenAI-compatible servers do not enumerate, and
// skipping there would silently stop running the tests where they used to work — so anything that is
// not a well-formed listing goes ahead and lets the request answer. An EMPTY one is different: a
// server that says `{"object":"list","data":null}` has told us it serves nothing, and that is the
// exact answer ollama gives with no models pulled (measured here). Reading that as "cannot tell" is
// what made the first version of this gate let the same three tests through to their 404s.
func serves(base, model string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + "/models")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return false
	}
	var listing struct {
		Object string                `json:"object"`
		Data   []struct{ ID string } `json:"data"`
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || json.Unmarshal(body, &listing) != nil || listing.Object != "list" {
		return true // cannot tell from here — let the request answer
	}
	for _, m := range listing.Data {
		if m.ID == model {
			return true
		}
	}
	return false
}
