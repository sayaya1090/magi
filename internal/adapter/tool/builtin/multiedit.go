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
	return "Apply multiple exact find/replace edits to a single file atomically (all-or-nothing). Provide {path, edits:[{old,new,replaceAll?}]}. Each 'old' must match uniquely (unless replaceAll). Edits are applied IN ORDER against the already-edited content: a later 'old' must match the file as it will be AFTER the earlier edits, so never overlap hunks — if two changes touch the same or adjacent lines, merge them into one hunk."
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
	// Serialize the read-apply-write against concurrent edits/writes of the same file
	// so all-or-nothing hunks don't interleave with another tool's write. Per-path.
	defer pathLocks.lock(lockKey(abs))()
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return errResult("", "file not found: "+a.Path), nil
		}
		return errResult("", err.Error()), nil
	}

	content := string(data)
	var addedAll, priorAll strings.Builder
	skipped := 0
	// Apply all hunks in memory first; fail fast without writing. Delegate each hunk to
	// applyEdit so multiedit inherits the same tolerant matching as edit (line endings,
	// Unicode normalization — the macOS NFD-Hangul case — and trailing whitespace) instead
	// of a strict byte-exact match that fails on the identical inputs edit would accept.
	for i, h := range a.Edits {
		if h.Old == "" {
			return errResult("", fmt.Sprintf("edit %d: old string must not be empty", i+1)), nil
		}
		if h.Old == h.New {
			// A no-op hunk (models sometimes include an unchanged hunk "for context")
			// is harmless — skip it rather than rejecting the whole batch for it.
			skipped++
			continue
		}
		updated, _, eerr := applyEdit(content, h.Old, h.New, h.ReplaceAll)
		if eerr != nil {
			return errResult("", fmt.Sprintf("edit %d: %s", i+1, eerr.Error())), nil
		}
		content = updated
		addedAll.WriteString(h.New + "\n")
		priorAll.WriteString(h.Old + "\n")
	}
	applied := len(a.Edits) - skipped
	if applied == 0 {
		return errResult("", "every edit was a no-op (old == new) — nothing to change"), nil
	}

	if err := atomicWriteFile(abs, []byte(content), 0o644); err != nil {
		return errResult("", err.Error()), nil
	}
	msg := fmt.Sprintf("applied %d edits to %s", applied, a.Path)
	if skipped > 0 {
		msg += fmt.Sprintf(" (skipped %d no-op hunk(s) whose old == new)", skipped)
	}
	msg += commentNoiseAdvisory(addedAll.String(), priorAll.String())
	return okText("", msg), nil
}
