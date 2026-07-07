package builtin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// isASCIIOnly reports whether s has no byte ≥ 0x80, i.e. no multi-byte rune. Used to
// skip Unicode-normalization work on the common ASCII path.
func isASCIIOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Grep searches file contents by regular expression. (F-TOOL-GREP)
type Grep struct{}

type grepArgs struct {
	Pattern string `json:"pattern"`
	Glob    string `json:"glob"` // optional filename filter, e.g. "*.go"
	Path    string `json:"path"` // optional subdir to search (default workdir)
}

func (Grep) Name() string        { return "grep" }
func (Grep) Description() string { return "Search file contents by regular expression." }
func (Grep) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"glob":{"type":"string"},"path":{"type":"string"}},"required":["pattern"]}`)
}

// grepGlobMatch applies the grep tool's filename filter. Models trained on
// Claude-style tools routinely pass comma-separated multi-globs with "**"
// ("**/*.go,**/*.py"); bare filepath.Match supports neither and silently
// filtered EVERYTHING out (an empty result that reads like "no matches").
// Accept a comma list; a pattern with "/" matches the workdir-relative path
// with ** crossing directories (shared matchGlob), a bare pattern matches the
// basename.
func grepGlobMatch(globs, workdir, path, base string) bool {
	rel, _ := filepath.Rel(workdir, path)
	rel = filepath.ToSlash(rel)
	for _, g := range strings.Split(globs, ",") {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if strings.ContainsRune(g, '/') {
			if matchGlob(g, rel) {
				return true
			}
			continue
		}
		gg, bb := g, base
		if !isASCIIOnly(g) { // fold an NFD filename to the NFC glob (see matchGlob)
			gg, bb = norm.NFC.String(g), norm.NFC.String(base)
		}
		if ok, _ := filepath.Match(gg, bb); ok {
			return true
		}
	}
	return false
}

func (Grep) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a grepArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	// A file saved on macOS is frequently NFD (decomposed Hangul/accents) while a model
	// types the pattern NFC (precomposed) — byte-unequal, so a literal regex silently
	// misses every Korean line. When the pattern has non-ASCII runes, fold both the
	// pattern and each tested line to NFC (the match test only; output is the original
	// bytes). ASCII patterns are untouched, so the common case keeps exact byte behavior.
	pattern := a.Pattern
	normUni := !isASCIIOnly(pattern)
	if normUni {
		pattern = norm.NFC.String(pattern)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return errResult("", "invalid regex: "+err.Error()), nil
	}
	root := env.Workdir
	if a.Path != "" {
		root, err = resolvePath(env.Workdir, a.Path)
		if err != nil {
			return errResult("", err.Error()), nil
		}
	}

	matches := []string{}
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != root {
				return fs.SkipDir
			}
			return nil
		}
		if a.Glob != "" && !grepGlobMatch(a.Glob, env.Workdir, p, d.Name()) {
			return nil
		}
		if symlinkEscapesJail(env.Workdir, p, d) {
			return nil // an in-workdir symlink pointing outside would leak that file
		}
		data, err := os.ReadFile(p)
		if err != nil || isBinary(data) {
			return nil
		}
		rel, _ := filepath.Rel(env.Workdir, p)
		rel = filepath.ToSlash(rel)
		sc := bufio.NewScanner(bytes.NewReader(data))
		line := 0
		for sc.Scan() {
			line++
			text := sc.Text()
			hay := text
			if normUni {
				hay = norm.NFC.String(text)
			}
			if re.MatchString(hay) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, line, text))
			}
		}
		return nil
	})
	if walkErr != nil {
		return errResult("", walkErr.Error()), nil
	}
	return okJSON("", matches), nil
}
