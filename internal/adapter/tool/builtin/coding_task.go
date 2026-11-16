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
	return "Prepare, inspect, or clean up an isolated coding task. prepare creates files ONLY: it does not start an agent or switch this session, and this session's file tools stay confined to its own workdir — never pass the returned workspace to read/grep/edit; follow the returned execution guidance instead. prepare needs a meaningful slug requestId (reuse it only for identical retries); status/diff/review/remove need the taskId it returned; review also needs a file path relative to the task root (path is for review only and never selects a source). remove deletes the task worktree, branch and tags once its changes are integrated. Git with an explicit base excludes uncommitted source changes; copy includes supported source files. Recognized Gradle output/cache directories are excluded unless tracked or in the baseline; inspect the returned exclusions. diff/review never apply changes; applying requires explicit user review through the CLI."
}
func (CodingTask) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"operation":{"type":"string","enum":["prepare","list","status","diff","review","remove"],"description":"prepare creates an isolated task, list inspects this source, status/diff/review inspect an existing task, remove cleans up a completed task worktree/branch/tags after integration."},"path":{"type":"string","description":"ONLY for review: a changed FILE path relative to the task root, e.g. src/main.go. Never a source directory and not accepted by prepare."},"requestId":{"type":"string","description":"Required ONLY for prepare. Meaningful ASCII slug request ID (e.g. fix-login-timeout); reuse with identical options for retries; use a new ID after changing options."},"taskId":{"type":"string","description":"Required for status/diff/review/remove. Use the taskId returned by prepare. Not accepted for prepare/list."},"mode":{"type":"string","enum":["auto","git","copy"],"description":"prepare only. auto selects Git when available. git starts from a commit; copy preserves supported uncommitted source files."},"base":{"type":"string","description":"prepare in Git mode only: explicit commit/ref, e.g. HEAD. Uncommitted changes are excluded, not copied."},"purpose":{"type":"string","description":"prepare only: short task description."}},"required":["operation"]}`)
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
