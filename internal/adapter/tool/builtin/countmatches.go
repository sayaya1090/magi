package builtin

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// CountMatches counts how many times a pattern occurs across a file or a glob of
// files. grep reports the matching lines but cannot total them; this answers
// quantitative questions ("how many test funcs / TODOs / error sites?") directly.
type CountMatches struct{}

type countMatchArgs struct {
	Pattern    string   `json:"pattern"`
	Path       string   `json:"path"`
	Glob       string   `json:"glob"`
	Fixed      flexBool `json:"fixed"`
	IgnoreCase flexBool `json:"ignore_case"`
}

func (CountMatches) Name() string { return "countmatches" }
func (CountMatches) Description() string {
	return "Count occurrences of a regex (or fixed string) across a file or a glob of files. Returns the total match count and the number of files with at least one match — for quantitative questions grep's line list cannot answer directly."
}
func (CountMatches) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{` +
		`"pattern":{"type":"string","description":"regular expression (matched over the whole file, not per line — use (?m) to make ^/$ match line boundaries), or a literal string when fixed=true"},` +
		`"path":{"type":"string","description":"single file (workdir-relative)"},` +
		`"glob":{"type":"string","description":"file filter, e.g. **/*.go — use instead of path to scan many files"},` +
		`"fixed":{"type":"boolean","description":"treat pattern as a literal substring, not a regex"},` +
		`"ignore_case":{"type":"boolean","description":"case-insensitive match"}` +
		`},"required":["pattern"]}`)
}

func (CountMatches) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a countMatchArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", invalidArgs(err)), nil
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return errResult("", "pattern is required"), nil
	}
	pat := a.Pattern
	normUni := !isASCIIOnly(pat)
	if normUni {
		pat = norm.NFC.String(pat)
	}
	if a.Fixed {
		pat = regexp.QuoteMeta(pat)
	}
	if a.IgnoreCase {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return errResult("", "invalid regex: "+err.Error()), nil
	}

	total := 0
	filesWith := 0
	visited, truncated, errMsg := walkFiles(env, a.Glob, a.Path, func(rel string, data []byte) {
		hay := string(data)
		if normUni {
			hay = norm.NFC.String(hay)
		}
		n := len(re.FindAllStringIndex(hay, -1))
		if n > 0 {
			total += n
			filesWith++
		}
	})
	if errMsg != "" {
		return errResult("", errMsg), nil
	}
	out := map[string]any{
		"matches": total, "files_with_matches": filesWith, "files_scanned": visited, "pattern": a.Pattern,
	}
	if truncated {
		out["truncated"] = true // a scanned file exceeded the read cap; count covers only the first bytes
	}
	return okJSON("", out), nil
}
