package builtin

import (
	"context"
	"encoding/json"
	"os"
	"sort"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// List returns the entries of a directory. (F-TOOL-LIST)
type List struct{}

type listArgs struct {
	Path string `json:"path"`
}

type listEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
}

func (List) Name() string        { return "list" }
func (List) Description() string { return "List the entries of a directory." }
func (List) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}

func (List) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a listArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult("", "not found: "+a.Path), nil
		}
		return errResult("", err.Error()), nil
	}
	if !info.IsDir() {
		return errResult("", "not a directory: "+a.Path), nil
	}
	des, err := os.ReadDir(abs)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	entries := make([]listEntry, 0, len(des))
	for _, d := range des {
		entries = append(entries, listEntry{Name: d.Name(), IsDir: d.IsDir()})
	}
	// Directories first, then by name (deterministic).
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	return okJSON("", entries), nil
}
