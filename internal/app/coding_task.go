package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	task "github.com/sayaya1090/magi/internal/core/codingtask"
	"io"
	"path/filepath"
)

func (a *App) codingTask(ctx context.Context, source string, raw json.RawMessage) (string, error) {
	if a.ClientKind() != ClientCoding {
		return "", fmt.Errorf("coding workspaces require a coding agent")
	}
	q, err := parseCodingTaskRequest(raw)
	if err != nil {
		return "", err
	}
	m := a.cfg.CodingTasks
	if m == nil {
		return "", fmt.Errorf("coding workspace storage is not configured")
	}
	var value any
	switch q.Operation {
	case "prepare":
		value, err = m.Prepare(ctx, source, task.Request{RequestID: q.RequestID, Mode: q.Mode, Base: q.Base, Purpose: q.Purpose})
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
	public := task.Public(value)
	if r, ok := value.(task.Record); ok {
		public = struct {
			task.Summary
			SessionWorkdir        string   `json:"sessionWorkdir"`
			SessionWorkdirChanged bool     `json:"sessionWorkdirChanged"`
			NextAction            string   `json:"nextAction"`
			RunCommand            []string `json:"runCommand"`
		}{task.Summarize(r), source, false,
			"This session has NOT moved. Its read/grep/edit tools still target sessionWorkdir. To develop in the isolated workspace, start a separate coding session using runCommand (replace the prompt placeholder). Do not join the argument array as an unquoted shell string: source/store may contain spaces. A bash cd affects only that shell call; it does not move this session. diff/review remain available here using taskId.",
			[]string{"magi", "coding-task", "run", "--source", r.Source, "--store", filepath.Dir(filepath.Dir(filepath.Dir(r.Workspace))), "--id", r.ID, "--prompt", "<development request>"},
		}
	}
	b, err := json.Marshal(public)
	return string(b), err
}

type codingTaskRequest struct {
	Operation string `json:"operation"`
	RequestID string `json:"requestId"`
	TaskID    string `json:"taskId"`
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Base      string `json:"base"`
	Purpose   string `json:"purpose"`
}

func parseCodingTaskRequest(raw json.RawMessage) (codingTaskRequest, error) {
	var q codingTaskRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&q); err != nil {
		return q, fmt.Errorf("invalid coding_task arguments: %w; source is bound by the host, path is only for review", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return q, fmt.Errorf("coding_task expects exactly one JSON object")
	}
	if q.Path != "" && q.Operation != "review" {
		return q, fmt.Errorf("path is only accepted by review (a file-relative path); %s uses this session's source workdir automatically", q.Operation)
	}
	switch q.Operation {
	case "prepare":
		if q.RequestID == "" {
			return q, fmt.Errorf("prepare requires a stable requestId; reuse it only with identical options")
		}
		if q.TaskID != "" {
			return q, fmt.Errorf("prepare returns taskId; do not supply it")
		}
		if q.Mode == "copy" && q.Base != "" {
			return q, fmt.Errorf("base selects a Git commit and cannot be used with copy mode")
		}
	case "list", "status", "diff", "review":
		if q.RequestID != "" || q.Mode != "" || q.Base != "" || q.Purpose != "" {
			return q, fmt.Errorf("requestId/mode/base/purpose are only accepted by prepare")
		}
		if q.Operation == "list" && q.TaskID != "" {
			return q, fmt.Errorf("list takes no taskId; use status to inspect one task")
		}
		if q.Operation != "list" && q.TaskID == "" {
			return q, fmt.Errorf("%s requires taskId returned by prepare", q.Operation)
		}
		if q.Operation == "review" && q.Path == "" {
			return q, fmt.Errorf("review requires a changed file-relative path, e.g. src/main.go")
		}
	default:
		return q, fmt.Errorf("operation must be prepare, list, status, diff, or review")
	}
	return q, nil
}
