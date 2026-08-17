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

// The plan arrives in whatever shape the model managed. A strict decode refused the whole call
// over the envelope, and the model then worked without a plan at all — reported from live use
// (2026-08-17, an unmarshal error on a todowrite the model had just written).
func TestTodoWriteTakesThePlanAsModelsActuallySendIt(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []session.Todo
	}{
		{"the declared shape", `{"todos":[{"content":"read the config","status":"pending"}]}`,
			[]session.Todo{{Content: "read the config", Status: "pending"}}},
		{"double-encoded array", `{"todos":"[{\"content\":\"read the config\",\"status\":\"in_progress\"}]"}`,
			[]session.Todo{{Content: "read the config", Status: "in_progress"}}},
		{"bare array, no envelope", `[{"content":"one","status":"completed"}]`,
			[]session.Todo{{Content: "one", Status: "completed"}}},
		{"plain strings", `{"todos":["one","two"]}`,
			[]session.Todo{{Content: "one", Status: "pending"}, {Content: "two", Status: "pending"}}},
		{"one todo, not a list", `{"todos":{"content":"only","status":"doing"}}`,
			[]session.Todo{{Content: "only", Status: "in_progress"}}},
		{"other field names", `{"tasks":[{"task":"build it","state":"done"}]}`,
			[]session.Todo{{Content: "build it", Status: "completed"}}},
		{"status words", `{"todos":[{"content":"a","status":"In Progress"},{"content":"b","status":"finished"},{"content":"c","status":"???"}]}`,
			[]session.Todo{{Content: "a", Status: "in_progress"}, {Content: "b", Status: "completed"}, {Content: "c", Status: "pending"}}},
		{"empty lines dropped", `{"todos":[{"content":"  ","status":"pending"},{"content":"real","status":"pending"}]}`,
			[]session.Todo{{Content: "real", Status: "pending"}}},
	}
	for _, c := range cases {
		var got []session.Todo
		res, err := TodoWrite{}.Execute(context.Background(), json.RawMessage(c.args),
			port.ToolEnv{SetTodos: func(td []session.Todo) { got = td }})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if res.IsError {
			t.Errorf("%s: refused the call: %s", c.name, res.Content)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: got %d todos, want %d (%+v)", c.name, len(got), len(c.want), got)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: todo %d = %+v, want %+v", c.name, i, got[i], c.want[i])
			}
		}
	}
}

// An unreadable status must never read as finished: a plan that fakes progress is worse than one
// that admits it does not know.
func TestTodoWriteNeverInventsCompletion(t *testing.T) {
	var got []session.Todo
	_, _ = TodoWrite{}.Execute(context.Background(),
		json.RawMessage(`{"todos":[{"content":"a"},{"content":"b","status":""},{"content":"c","status":"maybe"}]}`),
		port.ToolEnv{SetTodos: func(td []session.Todo) { got = td }})
	for _, t2 := range got {
		if t2.Status != "pending" {
			t.Errorf("unknown status became %q, want pending: %+v", t2.Status, t2)
		}
	}
}
