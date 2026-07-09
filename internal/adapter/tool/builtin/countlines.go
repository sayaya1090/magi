package builtin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// CountLines is a pure-Go wc: line, word, and byte counts over a file or a glob of
// files (LOC, file sizes). Lines are counted CRLF-tolerantly; a final line without a
// trailing newline still counts.
type CountLines struct{}

type countLinesArgs struct {
	Path string `json:"path"`
	Glob string `json:"glob"`
}

func (CountLines) Name() string { return "countlines" }
func (CountLines) Description() string {
	return "Count lines, words, and bytes over a file or a glob of files (a pure-Go wc). Use for LOC and size tallies instead of reading whole files."
}
func (CountLines) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"path":{"type":"string","description":"single file (workdir-relative)"},` +
		`"glob":{"type":"string","description":"file filter, e.g. internal/**/*.go — use instead of path to tally many files"}` +
		`},"required":[]}`)
}

func (CountLines) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a countLinesArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", invalidArgs(err)), nil
	}
	var lines, words, bytesN int
	visited, truncated, errMsg := walkFiles(env, a.Glob, a.Path, func(rel string, data []byte) {
		bytesN += len(data)
		for _, ln := range splitLines(data) {
			lines++
			words += len(strings.Fields(ln))
		}
	})
	if errMsg != "" {
		return errResult("", errMsg), nil
	}
	out := map[string]any{
		"lines": lines, "words": words, "bytes": bytesN, "files": visited,
	}
	if truncated {
		out["truncated"] = true // a counted file exceeded the read cap; totals cover only the first bytes
	}
	return okJSON("", out), nil
}
