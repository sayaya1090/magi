package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

// fileModifiers are tools whose successful result should trigger a post-edit
// diagnostics pass so the agent can self-correct (LSP-style feedback loop).
var fileModifiers = map[string]bool{"write": true, "edit": true, "multiedit": true}

// diagnose runs a fast language checker on a just-modified file and returns any
// problems (empty when clean or unsupported). This is a lightweight stand-in for
// full LSP diagnostics; it catches the common syntax-error class immediately.
func (a *App) diagnose(ctx context.Context, workdir, path string) string {
	if a.plat == nil || path == "" {
		return ""
	}
	switch filepath.Ext(path) {
	case ".go":
		return a.diagnoseGo(ctx, workdir, path)
	case ".py":
		res, err := a.plat.Exec(ctx, port.Cmd{Path: "python3", Args: []string{"-m", "py_compile", path}, Dir: workdir})
		if err == nil && res.ExitCode != 0 {
			return strings.TrimSpace(string(res.Stderr))
		}
		return ""
	default:
		return ""
	}
}

// diagnoseGo checks a Go file: gofmt -e for syntax (fast), then go vet for type
// errors when inside a module (compiles, so bounded by a timeout).
func (a *App) diagnoseGo(ctx context.Context, workdir, path string) string {
	if res, err := a.plat.Exec(ctx, port.Cmd{Path: "gofmt", Args: []string{"-e", path}, Dir: workdir}); err == nil && res.ExitCode != 0 {
		return strings.TrimSpace(string(res.Stderr))
	}
	if _, err := os.Stat(filepath.Join(workdir, "go.mod")); err != nil {
		return "" // not a module → skip type-check
	}
	target := "."
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		target = "./" + filepath.ToSlash(dir)
	}
	vctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := a.plat.Exec(vctx, port.Cmd{Path: "go", Args: []string{"vet", target}, Dir: workdir})
	if err == nil && res.ExitCode != 0 {
		return strings.TrimSpace(string(res.Stderr))
	}
	return ""
}

// pathArg extracts the "path" field from a tool's JSON arguments.
func pathArg(raw json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(raw, &a)
	return a.Path
}

// appendToContent appends text to a JSON-encoded string tool-result content.
func appendToContent(content json.RawMessage, extra string) json.RawMessage {
	var s string
	if json.Unmarshal(content, &s) != nil {
		s = string(content)
	}
	b, _ := json.Marshal(s + extra)
	return b
}
