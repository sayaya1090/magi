package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// TodoWrite replaces the session plan. The plan is injected into the model's
// context each turn, improving coherence on long multi-step tasks (like Claude
// Code's TodoWrite). (F-TOOL todo)
type TodoWrite struct{}

type todoWriteArgs struct {
	Todos []session.Todo `json:"todos"`
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
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	env.SetTodos(a.Todos)
	done := 0
	for _, t := range a.Todos {
		if t.Status == "completed" {
			done++
		}
	}
	return okText("", fmt.Sprintf("plan updated: %d todos (%d completed)", len(a.Todos), done)), nil
}
