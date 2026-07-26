package builtin

import (
	"context"
	"encoding/json"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

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
	if strings.TrimSpace(a.Pattern) == "" {
		return errResult("", "pattern is required"), nil
	}
	// Reject a malformed pattern up front (e.g. an unclosed "["), so a syntax error
	// surfaces as an error instead of silently matching nothing — mirrors grep. Every
	// non-"**" segment is probed, since ErrBadPattern can hide in any of them.
	for _, seg := range strings.Split(a.Pattern, "/") {
		if seg == "**" {
			continue
		}
		if _, err := filepath.Match(filepath.FromSlash(seg), ""); err != nil {
			return errResult("", "invalid glob pattern: "+err.Error()), nil
		}
	}
	out := []string{}
	root := filepath.Clean(env.Workdir)
	// Every other path-taking tool (read, list, bash) accepts an absolute path, so a model
	// that just ran `list /app/src` writes the glob the same way. Matching happens against
	// workspace-RELATIVE paths, so an absolute pattern cannot match anything — and the reply
	// came back as an empty list, which reads as the FACT "there are no such files" rather
	// than as a mistake in the call. Observed: `/app/<dir>/*.c` answered `[]` one line before
	// an `ls` of that same directory listed dozens of them; the agent then did every later
	// file lookup through bash. Re-anchor it, and when it points outside the workspace say so
	// instead of answering with a false emptiness.
	pattern, wasAbs, inside := anchorPattern(root, a.Pattern)
	if wasAbs && !inside {
		return errResult("", "pattern is outside the workspace ("+filepath.ToSlash(root)+"): "+
			a.Pattern+" — glob searches the workspace, so give a path relative to it"), nil
	}
	// A pattern that explicitly names a dotted segment (".github/…") opts into
	// otherwise-hidden paths; a plain pattern keeps skipping dot dirs/files as noise.
	wantHidden := patternWantsHidden(pattern)

	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root && !wantHidden {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && !wantHidden {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if matchGlob(pattern, rel) {
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

// patternWantsHidden reports whether any pattern segment explicitly starts with a
// dot (so the caller is deliberately targeting hidden paths like ".github/...").
// anchorPattern re-expresses an ABSOLUTE glob pattern as one relative to root, since matching
// runs against workspace-relative paths. It reports whether the pattern was absolute and, if so,
// whether it names something inside root — the two are separate answers because an absolute
// pattern pointing elsewhere is a mistake worth naming, while a relative one is simply used as is.
//
// Patterns are slash-separated on every platform, so a leading "/" counts as absolute even where
// filepath.IsAbs would say otherwise; filepath.IsAbs still catches a Windows "C:\…" spelling.
func anchorPattern(root, pattern string) (rel string, abs bool, inside bool) {
	if !strings.HasPrefix(pattern, "/") && !filepath.IsAbs(pattern) {
		return pattern, false, true
	}
	r := filepath.ToSlash(filepath.Clean(root))
	p := path.Clean(filepath.ToSlash(pattern)) // Clean leaves *, ?, [..] and ** alone
	switch {
	case p == r:
		return ".", true, true
	case r == "/": // a root workspace: everything absolute is inside it
		return strings.TrimPrefix(p, "/"), true, true
	case strings.HasPrefix(p, r+"/"):
		return strings.TrimPrefix(p, r+"/"), true, true
	}
	return pattern, true, false
}

func patternWantsHidden(pattern string) bool {
	for _, seg := range strings.Split(pattern, "/") {
		if strings.HasPrefix(seg, ".") && seg != "." && seg != ".." {
			return true
		}
	}
	return false
}

// matchGlob matches a slash-separated name against a glob pattern. The "**"
// segment matches zero or more path segments; other segments use filepath.Match
// semantics (*, ?, [..]).
//
// A non-ASCII pattern folds both sides to NFC so an NFD filename on disk (macOS
// decomposes Hangul/accents) matches the NFC pattern a model types. ASCII patterns
// are untouched, so the common case keeps exact byte behavior.
func matchGlob(pattern, name string) bool {
	if !isASCIIOnly(pattern) {
		pattern = norm.NFC.String(pattern)
		name = norm.NFC.String(name)
	}
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
