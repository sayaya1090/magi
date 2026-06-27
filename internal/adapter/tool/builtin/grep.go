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

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

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

func (Grep) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a grepArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	re, err := regexp.Compile(a.Pattern)
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

	var matches []string
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
		if a.Glob != "" {
			ok, _ := filepath.Match(a.Glob, d.Name())
			if !ok {
				return nil
			}
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
			if re.MatchString(sc.Text()) {
				matches = append(matches, fmt.Sprintf("%s:%d:%s", rel, line, sc.Text()))
			}
		}
		return nil
	})
	if walkErr != nil {
		return errResult("", walkErr.Error()), nil
	}
	return okJSON("", matches), nil
}
