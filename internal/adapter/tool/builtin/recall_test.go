package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// decodeResult unwraps a ToolResult's JSON-string content for assertions. All builtin
// results go through errResult/okText, which json.Marshal a string — so a non-string payload
// (e.g. a switch to okJSON) is a contract change the test should surface loudly, not mask.
func decodeResult(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("result content is not a JSON string (%v): %s", err, raw)
	}
	return s
}

func TestRecallContext(t *testing.T) {
	ctx := context.Background()

	t.Run("unavailable when env.Recall is nil", func(t *testing.T) {
		res, err := RecallContext{}.Execute(ctx, json.RawMessage(`{"topic":"x"}`), port.ToolEnv{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "not available") {
			t.Fatalf("want unavailable error, got IsError=%v %q", res.IsError, decodeResult(t, res.Content))
		}
	})

	t.Run("invalid json does not call Recall", func(t *testing.T) {
		called := false
		env := port.ToolEnv{Recall: func(string) (string, error) { called = true; return "x", nil }}
		res, _ := RecallContext{}.Execute(ctx, json.RawMessage(`{`), env)
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "invalid arguments") {
			t.Fatalf("want invalid arguments, got %q", decodeResult(t, res.Content))
		}
		if called {
			t.Fatal("Recall must not be called on invalid json")
		}
	})

	t.Run("empty topic rejected before calling Recall", func(t *testing.T) {
		called := false
		env := port.ToolEnv{Recall: func(string) (string, error) { called = true; return "", nil }}
		res, _ := RecallContext{}.Execute(ctx, json.RawMessage(`{"topic":""}`), env)
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "provide a {topic}") {
			t.Fatalf("want empty-topic error, got %q", decodeResult(t, res.Content))
		}
		if called {
			t.Fatal("Recall must not be called for an empty topic")
		}
	})

	t.Run("recall error surfaced", func(t *testing.T) {
		env := port.ToolEnv{Recall: func(string) (string, error) { return "", errors.New("boom") }}
		res, _ := RecallContext{}.Execute(ctx, json.RawMessage(`{"topic":"t"}`), env)
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "recall failed: boom") {
			t.Fatalf("want recall failed, got %q", decodeResult(t, res.Content))
		}
	})

	t.Run("success passes topic through and returns output", func(t *testing.T) {
		var gotTopic string
		env := port.ToolEnv{Recall: func(q string) (string, error) { gotTopic = q; return "ORIGINAL DETAIL", nil }}
		res, _ := RecallContext{}.Execute(ctx, json.RawMessage(`{"topic":"internal/app/loop.go"}`), env)
		if res.IsError {
			t.Fatalf("unexpected error result: %q", decodeResult(t, res.Content))
		}
		if gotTopic != "internal/app/loop.go" {
			t.Fatalf("topic not passed through: %q", gotTopic)
		}
		if decodeResult(t, res.Content) != "ORIGINAL DETAIL" {
			t.Fatalf("output = %q", decodeResult(t, res.Content))
		}
	})
}
