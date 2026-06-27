package app

import (
	"context"
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
	if base == "disabled" || !reachable(base) {
		t.Skipf("ollama not reachable at %s", base)
	}
	model := os.Getenv("MAGI_E2E_OLLAMA_MODEL")
	if model == "" {
		model = "qwen3-coder:30b"
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

func reachable(base string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(base + "/models")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}
