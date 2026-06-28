package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// LSP navigation tools (gopls CLI, Go-first, same approach as lsp_diagnostics): they
// give the agent SEMANTIC code navigation — precise definitions and references across
// packages — instead of approximating with grep (which also matches comments and
// same-named identifiers). A position is given as line + either col or, more
// conveniently for a model, a symbol name found on that line.

// runGopls runs a gopls subcommand in workdir and returns its output, or an error
// result (gopls missing / execution failure). A non-zero exit with output is fine.
func runGopls(ctx context.Context, workdir string, args ...string) (string, *session.ToolResult) {
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		r := errResult("", "gopls is not installed. Install it with:\n  go install golang.org/x/tools/gopls@latest")
		return "", &r
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, goplsPath, args...)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			r := errResult("", "gopls failed: "+err.Error()+"\n"+stderr.String())
			return "", &r
		}
	}
	out := stdout.String()
	if strings.TrimSpace(out) == "" {
		out = stderr.String()
	}
	return out, nil
}

// lspPosArgs is the shared position for definition/references.
type lspPosArgs struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`    // 1-based column; optional if symbol is given
	Symbol string `json:"symbol"` // a name on `line` to locate the column from
}

// resolvePos turns the args into a "file:line:col" gopls position. If col is unset,
// it locates symbol on the given line.
func resolvePos(workdir string, a lspPosArgs) (string, error) {
	abs, err := resolvePath(workdir, a.Path)
	if err != nil {
		return "", err
	}
	if a.Line < 1 {
		return "", fmt.Errorf("line is required (1-based)")
	}
	col := a.Col
	if col <= 0 {
		if a.Symbol == "" {
			return "", fmt.Errorf("provide either col or symbol")
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return "", err
		}
		lines := strings.Split(string(data), "\n")
		if a.Line > len(lines) {
			return "", fmt.Errorf("line %d is past end of file (%d lines)", a.Line, len(lines))
		}
		idx := strings.Index(lines[a.Line-1], a.Symbol)
		if idx < 0 {
			return "", fmt.Errorf("symbol %q not found on line %d", a.Symbol, a.Line)
		}
		col = idx + 1
	}
	return fmt.Sprintf("%s:%d:%d", abs, a.Line, col), nil
}

// relativize rewrites absolute workdir paths in gopls output to workspace-relative.
func relativize(out, workdir string) string {
	if workdir == "" {
		return out
	}
	return strings.ReplaceAll(out, workdir+string(os.PathSeparator), "")
}

// LspDefinition reports where the symbol at a position is defined.
type LspDefinition struct{}

func (LspDefinition) Name() string { return "lsp_definition" }
func (LspDefinition) Description() string {
	return "Find where a Go symbol is defined (gopls). Give path + line and either col or a symbol name on that line. Resolves across packages. Degrades gracefully without gopls."
}
func (LspDefinition) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer","description":"1-based line"},"col":{"type":"integer","description":"1-based column (optional if symbol given)"},"symbol":{"type":"string","description":"a name on that line to locate the column from"}},"required":["path","line"]}`)
}
func (LspDefinition) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a lspPosArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	pos, err := resolvePos(env.Workdir, a)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	out, errRes := runGopls(ctx, env.Workdir, "definition", pos)
	if errRes != nil {
		return *errRes, nil
	}
	return okText("", strings.TrimSpace(relativize(out, env.Workdir))), nil
}

// LspReferences lists all references to the symbol at a position.
type LspReferences struct{}

func (LspReferences) Name() string { return "lsp_references" }
func (LspReferences) Description() string {
	return "Find all references to a Go symbol across the project (gopls) — precise, unlike grep. Give path + line and either col or a symbol name on that line. Degrades gracefully without gopls."
}
func (LspReferences) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer","description":"1-based line"},"col":{"type":"integer","description":"1-based column (optional if symbol given)"},"symbol":{"type":"string","description":"a name on that line to locate the column from"}},"required":["path","line"]}`)
}
func (LspReferences) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a lspPosArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	pos, err := resolvePos(env.Workdir, a)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	out, errRes := runGopls(ctx, env.Workdir, "references", pos)
	if errRes != nil {
		return *errRes, nil
	}
	body := strings.TrimSpace(relativize(out, env.Workdir))
	if body == "" {
		body = "no references found"
	}
	return okText("", truncateOut(body)), nil
}

// LspSymbols lists the symbols (functions, types, methods) declared in a file.
type LspSymbols struct{}

func (LspSymbols) Name() string { return "lsp_symbols" }
func (LspSymbols) Description() string {
	return "List the symbols (functions, types, methods, vars) declared in a Go file with their kinds and ranges (gopls). A quick file outline. Degrades gracefully without gopls."
}
func (LspSymbols) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (LspSymbols) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}
	abs, err := resolvePath(env.Workdir, a.Path)
	if err != nil {
		return errResult("", err.Error()), nil
	}
	out, errRes := runGopls(ctx, env.Workdir, "symbols", abs)
	if errRes != nil {
		return *errRes, nil
	}
	body := strings.TrimSpace(out)
	if body == "" {
		body = "no symbols found"
	}
	return okText("", truncateOut(body)), nil
}
