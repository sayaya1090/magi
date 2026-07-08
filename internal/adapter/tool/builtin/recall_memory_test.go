package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestRecallMemory(t *testing.T) {
	ctx := context.Background()

	t.Run("unavailable when env.RecallMemory is nil", func(t *testing.T) {
		res, err := RecallMemory{}.Execute(ctx, json.RawMessage(`{"query":"x"}`), port.ToolEnv{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "not available") {
			t.Fatalf("want unavailable error, got IsError=%v %q", res.IsError, decodeResult(t, res.Content))
		}
	})

	t.Run("empty query rejected before calling RecallMemory", func(t *testing.T) {
		called := false
		env := port.ToolEnv{RecallMemory: func(string) (string, error) { called = true; return "", nil }}
		res, _ := RecallMemory{}.Execute(ctx, json.RawMessage(`{"query":""}`), env)
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "provide a {query}") {
			t.Fatalf("want empty-query error, got %q", decodeResult(t, res.Content))
		}
		if called {
			t.Fatal("RecallMemory must not be called for an empty query")
		}
	})

	t.Run("error surfaced", func(t *testing.T) {
		env := port.ToolEnv{RecallMemory: func(string) (string, error) { return "", errors.New("boom") }}
		res, _ := RecallMemory{}.Execute(ctx, json.RawMessage(`{"query":"t"}`), env)
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "recall failed: boom") {
			t.Fatalf("want recall failed, got %q", decodeResult(t, res.Content))
		}
	})

	t.Run("no match returns a friendly note", func(t *testing.T) {
		env := port.ToolEnv{RecallMemory: func(string) (string, error) { return "", nil }}
		res, _ := RecallMemory{}.Execute(ctx, json.RawMessage(`{"query":"nothing"}`), env)
		if res.IsError || !strings.Contains(decodeResult(t, res.Content), "No stored memories") {
			t.Fatalf("want no-match note, got IsError=%v %q", res.IsError, decodeResult(t, res.Content))
		}
	})

	t.Run("success passes query through and returns the full detail", func(t *testing.T) {
		var gotQ string
		env := port.ToolEnv{RecallMemory: func(q string) (string, error) { gotQ = q; return "- [project] use tabs", nil }}
		res, _ := RecallMemory{}.Execute(ctx, json.RawMessage(`{"query":"indentation"}`), env)
		if res.IsError {
			t.Fatalf("unexpected error result: %q", decodeResult(t, res.Content))
		}
		if gotQ != "indentation" {
			t.Fatalf("query not passed through: %q", gotQ)
		}
		if decodeResult(t, res.Content) != "- [project] use tabs" {
			t.Fatalf("output = %q", decodeResult(t, res.Content))
		}
	})
}
