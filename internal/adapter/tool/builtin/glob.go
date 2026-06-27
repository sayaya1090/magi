package builtin

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Glob lists files matching a glob pattern, supporting ** recursion. (F-TOOL-GLOB)
type Glob struct{}

type globArgs struct {
	Pattern string `json:"pattern"`
}

func (Glob) Name() string        { return "glob" }
func (Glob) Description() string { return "List files matching a glob pattern (supports **)." }
func (Glob) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`)
}

func (Glob) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a globArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}

	var out []string
	root := filepath.Clean(env.Workdir)
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if matchGlob(a.Pattern, rel) {
			out = append(out, rel)
		}
		return nil
	})
	if walkErr != nil {
		return errResult("", walkErr.Error()), nil
	}
	sort.Strings(out)
	return okJSON("", out), nil
}

// matchGlob matches a slash-separated name against a glob pattern. The "**"
// segment matches zero or more path segments; other segments use filepath.Match
// semantics (*, ?, [..]).
func matchGlob(pattern, name string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			if len(rest) == 0 {
				return true // trailing ** matches the remainder (including none)
			}
			for i := 0; i <= len(name); i++ {
				if matchSegs(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		ok, _ := filepath.Match(filepath.FromSlash(pat[0]), filepath.FromSlash(name[0]))
		if !ok {
			return false
		}
		pat = pat[1:]
		name = name[1:]
	}
	return len(name) == 0
}
