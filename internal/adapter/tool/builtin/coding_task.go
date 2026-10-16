package builtin

import (
	"context"
	"encoding/json"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

type CodingTask struct{}

func (CodingTask) Name() string { return "coding_task" }
func (CodingTask) Description() string {
	return "Prepare an isolated coding workspace or inspect status/diff or review exact before/after text (up to 256 KiB per file). Git uses a new branch/worktree; without Git use an independent copy. Does not switch this session's working directory or apply changes. Use the returned workspace for development; source integration requires explicit user review through coding-task CLI. Dirty Git sources require an explicit base or copy mode."
}
func (CodingTask) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"operation":{"type":"string","enum":["prepare","list","status","diff","review"]},"path":{"type":"string"},"requestId":{"type":"string","description":"Required for prepare; reuse the same stable ID when retrying a task."},"taskId":{"type":"string"},"mode":{"type":"string","enum":["auto","git","copy"]},"base":{"type":"string"},"purpose":{"type":"string"}},"required":["operation"]}`)
}
func (CodingTask) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	if env.CodingTask == nil {
		return errResult("", "Coding workspaces are unavailable for this client."), nil
	}
	result, err := env.CodingTask(ctx, raw)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", result), nil
}
