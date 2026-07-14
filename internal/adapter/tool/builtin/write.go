package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if strings.TrimSpace(a.Path) == "" {
		return errResult("", "path is required"), nil
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	// Serialize against a concurrent edit/write of the same file so an overwrite and
	// another tool's read-modify-write don't interleave. Per-path.
	defer pathLocks.lock(lockKey(abs))()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return errResult("", relPathErr(err, env.Workdir)), nil
	}
	if err := atomicWriteFile(abs, []byte(a.Content), 0o644); err != nil {
		return errResult("", relPathErr(err, env.Workdir)), nil
	}
	msg := fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path)
	msg += commentNoiseAdvisory(a.Content, "")
	return okText("", msg), nil
}

// relPathErr rewrites an OS error that embeds the absolute workdir path back to a
// jail-relative form, so a failure (e.g. "is a directory") reads like the other
// path errors and doesn't leak the workdir's real filesystem location.
func relPathErr(err error, workdir string) string {
	msg := err.Error()
	root := filepath.Clean(workdir)
	msg = strings.ReplaceAll(msg, root+string(filepath.Separator), "")
	msg = strings.ReplaceAll(msg, root, ".")
	return msg
}
