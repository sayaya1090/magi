package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// FileExperiment runs fixed built-in file tools in an ephemeral fixture tree.
func (*Registry) FileExperiment(ctx context.Context, files map[string]string, decide func(string, json.RawMessage) (string, string), run func(port.FileExperimentCall)) (map[string]string, error) {
	dir, err := os.MkdirTemp("", "magi-file-experiment-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Printf("file experiment cleanup: %v", err)
		}
	}()

	budget := 1 << 20
	for name, body := range files {
		budget -= len(name) + len(body)
		if len(files) > 100 || budget < 0 || !filepath.IsLocal(name) || filepath.Clean(name) != name || name == "." {
			return nil, fmt.Errorf("invalid or oversized file fixture")
		}
		args, err := json.Marshal(map[string]string{"path": name})
		if err != nil {
			return nil, err
		}
		if verdict, _ := decide("write", args); verdict != "" {
			return nil, fmt.Errorf("fixture path blocked by tool policy")
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			return nil, err
		}
	}

	tools := map[string]port.Tool{
		"write": Write{}, "edit": Edit{}, "multiedit": MultiEdit{}, "read": Read{},
	}
	calls, argumentBudget := 0, 4<<20
	run(func(callCtx context.Context, name string, args json.RawMessage) (session.ToolResult, error) {
		if err := callCtx.Err(); err != nil {
			return session.ToolResult{}, err
		}
		calls++
		argumentBudget -= len(args)
		if calls > 100 || argumentBudget < 0 {
			return session.ToolResult{}, fmt.Errorf("file experiment call budget exceeded")
		}
		tool, ok := tools[name]
		if !ok {
			return session.ToolResult{}, fmt.Errorf("file experiment does not support %s", name)
		}
		if verdict, reason := decide(name, args); verdict != "" {
			return session.ToolResult{}, fmt.Errorf("file experiment blocked by policy: %s", reason)
		}
		return tool.Execute(callCtx, args, port.ToolEnv{Workdir: dir})
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	state := map[string]string{}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		state[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sha256.Sum256(body))
		return nil
	})
	return state, err
}
