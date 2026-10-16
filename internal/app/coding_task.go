package app

import (
	"context"
	"encoding/json"
	"fmt"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
)

func (a *App) codingTask(ctx context.Context, source string, raw json.RawMessage) (string, error) {
	if a.ClientKind() != ClientCoding {
		return "", fmt.Errorf("coding workspaces require a coding agent")
	}
	var q struct {
		task.Request
		Operation string `json:"operation"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		return "", err
	}
	m := a.cfg.CodingTasks
	if m == nil {
		return "", fmt.Errorf("coding workspace storage is not configured")
	}
	var value any
	var err error
	switch q.Operation {
	case "prepare":
		if q.RequestID == "" {
			return "", fmt.Errorf("prepare requires a stable requestId; reuse it when retrying the same task")
		}
		value, err = m.Prepare(ctx, source, q.Request)
	case "list":
		value, err = m.List(ctx, source)
	case "status":
		value, err = m.Status(ctx, source, q.TaskID)
	case "review":
		value, err = m.Review(ctx, source, q.TaskID, q.Path)
	case "diff":
		value, err = m.Diff(ctx, source, q.TaskID)
	default:
		return "", fmt.Errorf("unsupported coding workspace operation")
	}
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(task.Public(value))
	return string(b), err
}
