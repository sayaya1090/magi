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
	return "Prepare or inspect an isolated coding task. prepare creates files ONLY: it does not start an agent or switch this session. File tools remain confined to the current session workdir; do not pass the returned workspace to this session's read/grep/edit tools. Follow the returned execution guidance to start a separate coding session. Operations: prepare requires requestId (reuse for identical retries); status/diff require taskId; review requires taskId and a file-relative path; list takes neither. path is ONLY for review and never selects a source: the host binds source to this session. Git with an explicit base excludes uncommitted source changes; copy includes supported source files. Recognized Gradle output/cache directories are excluded unless tracked or part of the existing baseline; inspect the returned exclusions. Diff/review inspect changes without applying them; applying requires explicit user review through CLI. Example prepare: {\"operation\":\"prepare\",\"requestId\":\"feature-001\",\"mode\":\"auto\"}. Review uses operation=review, taskId returned by prepare, and path=src/main.go."
}
func (CodingTask) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{"operation":{"type":"string","enum":["prepare","list","status","diff","review"],"description":"prepare creates an isolated task, list inspects this source, status/diff/review inspect an existing task."},"path":{"type":"string","description":"ONLY for review: a changed FILE path relative to the task root, e.g. src/main.go. Never a source directory and not accepted by prepare."},"requestId":{"type":"string","description":"Required ONLY for prepare. Stable logical request ID; reuse with identical options for retries; use a new ID after changing options."},"taskId":{"type":"string","description":"Required for status/diff/review. Use the taskId returned by prepare. Not accepted for prepare/list."},"mode":{"type":"string","enum":["auto","git","copy"],"description":"prepare only. auto selects Git when available. git starts from a commit; copy preserves supported uncommitted source files."},"base":{"type":"string","description":"prepare in Git mode only: explicit commit/ref, e.g. HEAD. Uncommitted changes are excluded, not copied."},"purpose":{"type":"string","description":"prepare only: short task description."}},"required":["operation"]}`)
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
