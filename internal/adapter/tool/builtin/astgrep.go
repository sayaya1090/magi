package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// AstGrep is a structural (AST-aware) code search built on the external ast-grep
// CLI. Unlike grep, it matches code *shape* — `$A == nil` finds nil comparisons
// regardless of formatting, `func $F($$$) error` finds error-returning funcs —
// so localization lands on real constructs, not text coincidences. It shells out
// (no CGO), and degrades gracefully: when ast-grep isn't installed it tells the
// model to fall back to grep/findcontext. Read-only (search); rewrite is future.
type AstGrep struct{}

type astGrepArgs struct {
	Pattern string `json:"pattern"`
	Lang    string `json:"lang"` // optional: go|python|typescript|rust|java… (inferred if empty)
	Path    string `json:"path"` // optional subdir (default workdir)
}

type astMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text,omitempty"`
}

func (AstGrep) Name() string { return "astgrep" }
func (AstGrep) Description() string {
	return "Structural (AST) code search via ast-grep: match code by SHAPE using metavariables — `$A` (one node), `$$$` (zero+ nodes). Examples: `$X == nil`, `func $F($$$) error`, `if err != nil { $$$ }`. Far more precise than grep for locating constructs to edit. Provide 'pattern' and optionally 'lang' and 'path'. If unavailable, use grep/findcontext instead."
}
func (AstGrep) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"lang":{"type":"string"},"path":{"type":"string"}},"required":["pattern"]}`)
}

// astGrepBin locates the ast-grep CLI (the binary is "ast-grep"; "sg" is an
// alias that collides with ScreenGrab on some systems, so prefer the full name).
func astGrepBin() (string, bool) {
	if p, err := exec.LookPath("ast-grep"); err == nil {
		return p, true
	}
	if p, err := exec.LookPath("sg"); err == nil {
		return p, true
	}
	return "", false
}

func (AstGrep) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a astGrepArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	if a.Pattern == "" {
		return errResult("", "astgrep: 'pattern' is required"), nil
	}
	bin, ok := astGrepBin()
	if !ok {
		return errResult("", "ast-grep is not installed; use grep or findcontext for this search (install: https://ast-grep.github.io)"), nil
	}

	root := env.Workdir
	if a.Path != "" {
		r, err := resolvePath(env.Workdir, a.Path)
		if err != nil {
			return errResult("", err.Error()), nil
		}
		root = r
	}

	args := []string{"run", "--pattern", a.Pattern, "--json=stream"}
	if a.Lang != "" {
		args = append(args, "--lang", a.Lang)
	}
	args = append(args, root)

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Dir = env.Workdir
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.Output()
	if err != nil {
		// ast-grep exits non-zero on a bad pattern; surface stderr-ish hint.
		if len(out) == 0 {
			return errResult("", "astgrep: "+err.Error()+" (check the pattern/lang)"), nil
		}
	}

	matches := parseAstGrepStream(out, env.Workdir)
	if len(matches) == 0 {
		return okText("", "no structural matches"), nil
	}
	const cap = 50
	truncated := false
	if len(matches) > cap {
		matches = matches[:cap]
		truncated = true
	}
	res := okJSON("", matches)
	if truncated {
		res = okJSON("", map[string]any{"matches": matches, "note": "truncated to first 50 matches"})
	}
	return res, nil
}

// parseAstGrepStream parses ast-grep's `--json=stream` output (one JSON object
// per line) into matches with workdir-relative paths and 1-based lines. It is
// tolerant: unparseable lines are skipped.
func parseAstGrepStream(out []byte, workdir string) []astMatch {
	var matches []astMatch
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var m struct {
			File  string `json:"file"`
			Text  string `json:"text"`
			Lines string `json:"lines"`
			Range struct {
				Start struct {
					Line int `json:"line"`
				} `json:"start"`
			} `json:"range"`
		}
		if err := dec.Decode(&m); err != nil {
			break
		}
		if m.File == "" {
			continue
		}
		file := m.File
		if rel, err := filepath.Rel(workdir, m.File); err == nil && !strings.HasPrefix(rel, "..") {
			file = filepath.ToSlash(rel)
		}
		snippet := m.Lines
		if snippet == "" {
			snippet = m.Text
		}
		matches = append(matches, astMatch{
			File: file,
			Line: m.Range.Start.Line + 1, // ast-grep lines are 0-based
			Text: oneLineN(snippet, 160),
		})
	}
	return matches
}
