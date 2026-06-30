package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func TestTodoWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("unavailable when SetTodos is nil", func(t *testing.T) {
		res, err := TodoWrite{}.Execute(ctx, json.RawMessage(`{"todos":[]}`), port.ToolEnv{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "not available") {
			t.Fatalf("want unavailable error, got IsError=%v %q", res.IsError, decodeResult(t, res.Content))
		}
	})

	t.Run("invalid json does not call SetTodos", func(t *testing.T) {
		called := false
		env := port.ToolEnv{SetTodos: func([]session.Todo) { called = true }}
		res, _ := TodoWrite{}.Execute(ctx, json.RawMessage(`{"todos":`), env)
		if !res.IsError || !strings.Contains(decodeResult(t, res.Content), "invalid arguments") {
			t.Fatalf("want invalid arguments, got %q", decodeResult(t, res.Content))
		}
		if called {
			t.Fatal("SetTodos must not be called on invalid json")
		}
	})

	t.Run("forwards todos verbatim and counts completed", func(t *testing.T) {
		var got []session.Todo
		env := port.ToolEnv{SetTodos: func(td []session.Todo) { got = td }}
		args := `{"todos":[{"content":"a","status":"completed"},{"content":"b","status":"in_progress"},{"content":"c","status":"completed"}]}`
		res, _ := TodoWrite{}.Execute(ctx, json.RawMessage(args), env)
		if res.IsError {
			t.Fatalf("unexpected error: %q", decodeResult(t, res.Content))
		}
		want := []session.Todo{
			{Content: "a", Status: "completed"},
			{Content: "b", Status: "in_progress"},
			{Content: "c", Status: "completed"},
		}
		if len(got) != len(want) {
			t.Fatalf("got %d todos, want %d: %+v", len(got), len(want), got)
		}
		for i := range want {
			if got[i].Content != want[i].Content || got[i].Status != want[i].Status {
				t.Fatalf("todo[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
		if msg := decodeResult(t, res.Content); !strings.Contains(msg, "3 todos") || !strings.Contains(msg, "2 completed") {
			t.Fatalf("summary wrong: %q", msg)
		}
	})

	t.Run("empty plan is valid and clears via SetTodos", func(t *testing.T) {
		called := false
		var got []session.Todo
		env := port.ToolEnv{SetTodos: func(td []session.Todo) { called = true; got = td }}
		res, _ := TodoWrite{}.Execute(ctx, json.RawMessage(`{"todos":[]}`), env)
		if res.IsError {
			t.Fatalf("empty plan should be ok, got %q", decodeResult(t, res.Content))
		}
		if !called || len(got) != 0 {
			t.Fatalf("SetTodos should be called with an empty slice, called=%v got=%+v", called, got)
		}
		if msg := decodeResult(t, res.Content); !strings.Contains(msg, "0 todos") {
			t.Fatalf("summary = %q", msg)
		}
	})
}
