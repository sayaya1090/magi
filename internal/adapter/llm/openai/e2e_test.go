package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// backend describes a real OpenAI-compatible endpoint to exercise.
type backend struct {
	name  string
	base  string
	model string
}

// e2eBackends returns the backends configured via env. Unconfigured backends
// are simply omitted (the suite auto-skips when none are set), so plain
// `go test ./...` on a machine without models stays green.
//
//	Ollama : MAGI_E2E_OLLAMA_BASE  (default http://localhost:11434/v1) + _MODEL (default qwen3-coder:30b)
//	LiteLLM: MAGI_E2E_LITELLM_BASE + _MODEL
//	vLLM   : MAGI_E2E_VLLM_BASE    + _MODEL
func e2eBackends() []backend {
	var out []backend
	add := func(name, baseEnv, modelEnv, defBase, defModel string) {
		base := os.Getenv(baseEnv)
		// Ollama has a sensible local default; others require explicit opt-in.
		if base == "" && defBase != "" {
			base = defBase
		}
		if base == "" {
			return
		}
		model := os.Getenv(modelEnv)
		if model == "" {
			model = defModel
		}
		out = append(out, backend{name: name, base: base, model: model})
	}
	add("ollama", "MAGI_E2E_OLLAMA_BASE", "MAGI_E2E_OLLAMA_MODEL", "http://localhost:11434/v1", "qwen3-coder:30b")
	add("litellm", "MAGI_E2E_LITELLM_BASE", "MAGI_E2E_LITELLM_MODEL", "", "llama31")
	add("vllm", "MAGI_E2E_VLLM_BASE", "MAGI_E2E_VLLM_MODEL", "", "")
	return out
}

// reachable reports whether base answers /models within a short timeout.
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

func readToolSpec() port.ToolSpec {
	return port.ToolSpec{
		Name:        "read",
		Description: "Read the contents of a file.",
		Schema:      json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`),
	}
}

func TestE2E(t *testing.T) {
	backends := e2eBackends()
	if len(backends) == 0 {
		t.Skip("no E2E backends configured (set MAGI_E2E_*_BASE)")
	}
	for _, b := range backends {
		b := b
		t.Run(b.name, func(t *testing.T) {
			if !serves(b.base, b.model) {
				t.Skipf("backend %s does not serve %s at %s", b.name, b.model, b.base)
			}
			t.Logf("E2E backend=%s base=%s model=%s", b.name, b.base, b.model)
			c := New(b.base, os.Getenv("MAGI_E2E_API_KEY"))

			t.Run("text-stream", func(t *testing.T) { e2eTextStream(t, c, b) })
			t.Run("native-tool-call", func(t *testing.T) { e2eToolCall(t, c, b) })
		})
	}
}

// Plain text streaming reaches a finish with non-empty output.
func e2eTextStream(t *testing.T, c *Client, b backend) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ch, err := c.StreamChat(ctx, port.ChatRequest{
		Model:    b.model,
		Messages: []session.Message{userMsg("Say the single word: pong")},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var text string
	var finished bool
	for e := range ch {
		switch e.Type {
		case port.ProviderText:
			text += e.Text
		case port.ProviderFinish:
			finished = true
		case port.ProviderError:
			t.Fatalf("provider error: %v", e.Err)
		}
	}
	if !finished {
		t.Error("stream did not finish")
	}
	if text == "" {
		t.Error("no text produced")
	}
}

// The model emits a real native tool call for the `read` tool. This is the case
// mock fixtures previously hid (memory: gocode local-model tool-calling bug).
func e2eToolCall(t *testing.T, c *Client, b backend) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ch, err := c.StreamChat(ctx, port.ChatRequest{
		Model: b.model,
		System: "You are a coding agent. When asked to read a file, you MUST call the read tool. " +
			"Do not answer in prose.",
		Messages: []session.Message{userMsg("Read the file config.txt and show me its contents.")},
		Tools:    []port.ToolSpec{readToolSpec()},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	var toolCalls []*session.ToolCall
	for e := range ch {
		if e.Type == port.ProviderToolCall {
			toolCalls = append(toolCalls, e.ToolCall)
		}
		if e.Type == port.ProviderError {
			t.Fatalf("provider error: %v", e.Err)
		}
	}
	if len(toolCalls) == 0 {
		t.Fatalf("model %s produced no tool call (native tool-calling broken?)", b.model)
	}
	tc := toolCalls[0]
	if tc.Name != "read" {
		t.Errorf("tool name=%q want read", tc.Name)
	}
	// Arguments must be valid JSON containing a path.
	var args map[string]any
	if err := json.Unmarshal(tc.Args, &args); err != nil {
		t.Fatalf("tool args not valid JSON: %s", tc.Args)
	}
	if _, ok := args["path"]; !ok {
		t.Errorf("tool args missing 'path': %s", tc.Args)
	}
}

func userMsg(text string) session.Message {
	return session.Message{
		Role:  session.RoleUser,
		Parts: []session.Part{{Kind: session.PartText, Text: text}},
	}
}
