package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// MultiEdit applies several exact find/replace hunks to one file atomically: all
// hunks must apply, or none are written. This makes larger multi-part changes
// reliable instead of brittle single edits. (F-TOOL multiedit / apply_patch)
type MultiEdit struct{}

type multiEditArgs struct {
	Path  string     `json:"path"`
	Edits []editHunk `json:"edits"`
}

type editHunk struct {
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replaceAll"`
}

func (MultiEdit) Name() string { return "multiedit" }
func (MultiEdit) Description() string {
	return "Apply multiple exact find/replace edits to a single file atomically (all-or-nothing). Provide {path, edits:[{old,new,replaceAll?}]}. Each 'old' must match uniquely (unless replaceAll)."
}
func (MultiEdit) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"edits":{"type":"array","items":{"type":"object","properties":{"old":{"type":"string"},"new":{"type":"string"},"replaceAll":{"type":"boolean"}},"required":["old","new"]}}},"required":["path","edits"]}`)
}

func (MultiEdit) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a multiEditArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if len(a.Edits) == 0 {
		return errResult("", "no edits provided"), nil
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult("", "file not found: "+a.Path), nil
		}
		return errResult("", err.Error()), nil
	}

	content := string(data)
	// Apply all hunks in memory first; fail fast without writing.
	for i, h := range a.Edits {
		if h.Old == "" {
			return errResult("", fmt.Sprintf("edit %d: old string must not be empty", i+1)), nil
		}
		if h.Old == h.New {
			return errResult("", fmt.Sprintf("edit %d: no change", i+1)), nil
		}
		count := strings.Count(content, h.Old)
		switch {
		case count == 0:
			return errResult("", fmt.Sprintf("edit %d: not found", i+1)), nil
		case count > 1 && !h.ReplaceAll:
			return errResult("", fmt.Sprintf("edit %d: not unique (%d matches)", i+1, count)), nil
		}
		if h.ReplaceAll {
			content = strings.ReplaceAll(content, h.Old, h.New)
		} else {
			content = strings.Replace(content, h.Old, h.New, 1)
		}
	}

	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return errResult("", err.Error()), nil
	}
	return okText("", fmt.Sprintf("applied %d edits to %s", len(a.Edits), a.Path)), nil
}
