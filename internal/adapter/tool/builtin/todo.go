package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// TodoWrite replaces the session plan. The plan is injected into the model's
// context each turn, improving coherence on long multi-step tasks (like Claude
// Code's TodoWrite). (F-TOOL todo)
type TodoWrite struct{}

type todoWriteArgs struct {
	Todos flexTodos `json:"todos"`
	// The same list under the names models reach for when they do not copy the schema. Read only
	// when `todos` came back empty, so the declared name always wins.
	Plan  flexTodos `json:"plan"`
	Items flexTodos `json:"items"`
	Tasks flexTodos `json:"tasks"`
}

// flexTodos is the plan as models actually send it, rather than as the schema describes it.
//
// A strict decode rejected the WHOLE call over the container's shape, and a rejected plan is worse
// than a sloppy one: the model was told its plan does not exist and carried on without one, which
// is exactly the long multi-step coherence this tool is for. Reported from live use (2026-08-17).
// The shapes accepted, all seen from weak models: the array double-encoded as a STRING, a bare
// array where an object was asked for, and a single todo where a list was asked for.
type flexTodos []session.Todo

func (t *flexTodos) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		*t = nil
		return nil
	}
	// A string that CONTAINS the array: the model encoded its arguments twice. Unwrap once and
	// re-read; a string that is not JSON becomes one todo, below.
	if trimmed[0] == '"' {
		var inner string
		if err := json.Unmarshal(b, &inner); err == nil {
			if in := strings.TrimSpace(inner); in != "" && (in[0] == '[' || in[0] == '{') {
				return t.UnmarshalJSON([]byte(in))
			}
			*t = flexTodos{{Content: inner, Status: "pending"}}
			return nil
		}
	}
	// One todo where a list was asked for.
	if trimmed[0] == '{' {
		var one flexTodo
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*t = flexTodos{session.Todo(one)}
		return nil
	}
	var raw []flexTodo
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	out := make(flexTodos, 0, len(raw))
	for _, x := range raw {
		out = append(out, session.Todo(x))
	}
	*t = out
	return nil
}

// flexTodo is one entry, under the field names and status words models actually use. Anything with
// no text at all is dropped by the caller: a checklist line with nothing on it is not a step.
type flexTodo session.Todo

func (e *flexTodo) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	// A plain string is a step with no status: "read the config". Status is what the model updates
	// later, and refusing the line for lacking one loses the step itself.
	if trimmed != "" && trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*e = flexTodo{Content: strings.TrimSpace(s), Status: "pending"}
		return nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			for name, v := range m {
				if !strings.EqualFold(name, k) {
					continue
				}
				var s string
				if json.Unmarshal(v, &s) == nil {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}
	*e = flexTodo{
		Content: pick("content", "task", "title", "text", "description", "step", "name", "todo", "item"),
		Status:  todoStatus(pick("status", "state", "progress", "done")),
	}
	return nil
}

// todoStatus folds what models write onto the three states the plan has. An unrecognised word is
// "pending": a step whose state cannot be read is a step still to do, never a finished one — the
// direction that cannot fake progress.
func todoStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "_", " "), "-", " "))) {
	case "completed", "complete", "done", "finished", "closed", "true", "yes", "ok":
		return "completed"
	case "in progress", "inprogress", "active", "doing", "working", "current", "started", "running":
		return "in_progress"
	default:
		return "pending"
	}
}

func (TodoWrite) Name() string { return "todowrite" }
func (TodoWrite) Description() string {
	return "Record/replace your task plan as a checklist. Each todo has 'content' and 'status' (pending|in_progress|completed). Use it to plan and track multi-step work; update statuses as you go."
}
func (TodoWrite) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"todos":{"type":"array","items":{"type":"object","properties":{"content":{"type":"string"},"status":{"type":"string","enum":["pending","in_progress","completed"]}},"required":["content","status"]}}},"required":["todos"]}`)
}

func (TodoWrite) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.SetTodos == nil {
		return errResult("", "todo plan is not available in this context"), nil
	}
	var a todoWriteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		// The whole payload as a bare ARRAY, the object's wrapper dropped. Read that way before
		// giving up: the plan is right there and the only thing wrong is the envelope.
		var bare flexTodos
		if berr := bare.UnmarshalJSON(raw); berr != nil || len(bare) == 0 {
			return errResult("", "invalid arguments: "+err.Error()), nil
		}
		a = todoWriteArgs{Todos: bare}
	}
	todos := a.Todos
	for _, alt := range []flexTodos{a.Plan, a.Items, a.Tasks} {
		if len(todos) == 0 {
			todos = alt
		}
	}
	// A line with no text is not a step. Kept separate from the parse so every shape above is
	// cleaned the same way.
	kept := make([]session.Todo, 0, len(todos))
	for _, t := range todos {
		if strings.TrimSpace(t.Content) != "" {
			if t.Status == "" {
				t.Status = "pending"
			}
			kept = append(kept, t)
		}
	}
	a.Todos = kept
	env.SetTodos(a.Todos)
	done := 0
	for _, t := range a.Todos {
		if t.Status == "completed" {
			done++
		}
	}
	return okText("", fmt.Sprintf("plan updated: %d todos (%d completed)", len(a.Todos), done)), nil
}
