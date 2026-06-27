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

// Read returns the contents of a file, optionally a line range. (F-TOOL-READ)
type Read struct{}

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"` // 1-based start line (optional)
	Limit  int    `json:"limit"`  // max lines (optional)
}

func (Read) Name() string        { return "read" }
func (Read) Description() string { return "Read the contents of a file, optionally a line range." }
func (Read) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"]}`)
}

func (Read) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a readArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	locatedNote := ""
	info, err := os.Stat(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			return errResult("", err.Error()), nil
		}
		// Recover from an imprecise path: if a same-named file exists elsewhere in
		// the tree, read it (when unique) or suggest the candidates, instead of
		// dead-ending — weak models otherwise give up or bounce the task back.
		located, rel, suggest := resolveOrSuggest(env.Workdir, a.Path)
		if located == "" {
			if suggest != "" {
				return errResult("", "file not found: "+a.Path+" — "+suggest), nil
			}
			return errResult("", "file not found: "+a.Path), nil
		}
		abs = located
		locatedNote = "(note: \"" + a.Path + "\" not found; reading \"" + rel + "\" instead)\n"
		if info, err = os.Stat(abs); err != nil {
			return errResult("", "file not found: "+a.Path), nil
		}
	}
	if info.IsDir() {
		return errResult("", "is a directory: "+a.Path), nil
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	if isBinary(data) {
		return errResult("", "binary file: "+a.Path), nil
	}

	start := a.Offset
	if start < 1 {
		start = 1
	}
	content := string(data)
	if a.Offset > 0 || a.Limit > 0 {
		content = sliceLines(content, a.Offset, a.Limit)
	}
	// Prefix each line with its 1-based number (cat -n style) so the model can
	// navigate and reference lines accurately.
	return okText("", locatedNote+numberLines(content, start)), nil
}

// numberLines prefixes each line with a right-aligned line number + tab,
// starting at firstLine.
func numberLines(s string, firstLine int) string {
	if s == "" {
		return s
	}
	hadTrailingNL := strings.HasSuffix(s, "\n")
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%6d\t%s", firstLine+i, line)
	}
	if hadTrailingNL {
		b.WriteByte('\n')
	}
	return b.String()
}

// sliceLines returns lines [offset, offset+limit) (1-based), preserving the
// trailing newline of each kept line.
func sliceLines(s string, offset, limit int) string {
	if offset < 1 {
		offset = 1
	}
	lines := strings.SplitAfter(s, "\n")
	// SplitAfter on "a\nb\n" yields ["a\n","b\n",""]; drop the trailing empty.
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	start := offset - 1
	if start >= len(lines) {
		return ""
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return strings.Join(lines[start:end], "")
}
