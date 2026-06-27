package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Write creates or overwrites a file, creating parent dirs as needed. (F-TOOL-WRITE)
type Write struct{}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (Write) Name() string        { return "write" }
func (Write) Description() string { return "Create or overwrite a file with the given content." }
func (Write) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}

func (Write) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a writeArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return errResult("", err.Error()), nil
	}
	if err := os.WriteFile(abs, []byte(a.Content), 0o644); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)), nil
}
