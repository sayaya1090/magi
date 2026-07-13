package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// LspDiag runs gopls check to collect LSP diagnostics (type errors, unused vars, etc.).
// Gracefully degrades to a suggestion if gopls is not installed.
type LspDiag struct{}

type lspDiagArgs struct {
	Path string `json:"path"` // optional: file or package to check (default: ./...)
}

func (LspDiag) Name() string { return "lsp_diagnostics" }
func (LspDiag) Description() string {
	return "Report LSP diagnostics (type errors, unused/undefined symbols, etc.) for a file. Go uses gopls (path defaults to ./...); other languages (Python, C/C++, Rust, TypeScript/JS, and more) open the file in their language server. " +
		"Gracefully degrades to a suggestion when no server is installed for the language."
}
func (LspDiag) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"file to check (Go: file or package, default ./...)"}},"required":[]}`)
}

func (LspDiag) Execute(ctx context.Context, raw json.RawMessage, env port.ToolEnv) (session.ToolResult, error) {
	var a lspDiagArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return errResult("", "invalid arguments: "+err.Error()), nil
	}

	// Non-Go files go through the generic LSP publishDiagnostics path.
	if ext := strings.ToLower(filepath.Ext(a.Path)); a.Path != "" && ext != ".go" {
		if _, ok := serverFor(a.Path); ok {
			return lspDiagnoseResult(ctx, env.Workdir, a.Path), nil
		}
	}

	// Check if gopls is available
	goplsPath, err := exec.LookPath("gopls")
	if err != nil {
		return session.ToolResult{
			Content: []byte("gopls is not installed. Install it with:\n  go install golang.org/x/tools/gopls@latest\n\n" +
				"Alternatively, run `go build ./...` or `go test ./...` to see compiler diagnostics."),
			IsError: true,
		}, nil
	}

	target := a.Path
	if target == "" {
		target = "./..."
	}

	// Run gopls check
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cctx, goplsPath, "check", target)
	cmd.Dir = env.Workdir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	// gopls check exits non-zero when diagnostics are found, which is not an error.
	// Only treat non-exit-status errors (e.g., exec failure, OOM) as actual errors.
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			// Actual execution failure (not just non-zero exit)
			return session.ToolResult{
				Content: []byte(fmt.Sprintf("gopls execution failed: %v\nStderr: %s", err, stderr.String())),
				IsError: true,
			}, nil
		}
		// ExitError is expected when diagnostics are found - continue parsing
	}

	// Parse output
	output := stdout.String()
	if output == "" && stderr.Len() > 0 {
		output = stderr.String()
	}

	// Format diagnostics
	result := formatGoplsDiagnostics(output, env.Workdir)
	if result == "" {
		result = "No diagnostics found. Code is clean!"
	}

	return session.ToolResult{
		Content: []byte(result),
		IsError: false,
	}, nil
}

// lspDiagnoseResult runs the generic LSP diagnostics path for a non-Go file and
// packages it as a tool result. A missing server binary or an unsupported
// language degrades to a non-error suggestion, never a hard failure.
func lspDiagnoseResult(ctx context.Context, workdir, relPath string) session.ToolResult {
	abs, err := resolvePath(workdir, relPath)
	if err != nil {
		return errResult("", err.Error())
	}
	diags, err := lspDiagnose(ctx, workdir, abs)
	if err != nil {
		return session.ToolResult{Content: []byte("no diagnostics available: " + err.Error() +
			"\nInstall the language server to enable diagnostics, or build/run the project to surface compiler errors."), IsError: false}
	}
	out := formatDiagnostics(diags, filepath.ToSlash(relPath))
	if out == "" {
		out = "No diagnostics found. Code is clean!"
	}
	return okText("", out)
}

// formatGoplsDiagnostics parses gopls check output and formats it for the agent.
// gopls check output format: file:line:col: message
func formatGoplsDiagnostics(output, workdir string) string {
	if strings.TrimSpace(output) == "" {
		return ""
	}

	var buf bytes.Buffer
	lines := strings.Split(output, "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Make paths relative
		if rel := makeRelative(line, workdir); rel != line {
			line = rel
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		count++
	}

	if count == 0 {
		return ""
	}
	return fmt.Sprintf("Found %d diagnostic(s):\n\n%s", count, buf.String())
}

func makeRelative(line, workdir string) string {
	// Try to extract file path and make it relative
	parts := strings.SplitN(line, ":", 3)
	if len(parts) < 2 {
		return line
	}
	absPath := parts[0]
	if !filepath.IsAbs(absPath) {
		return line
	}
	rel, err := filepath.Rel(workdir, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return line
	}
	return filepath.ToSlash(rel) + ":" + strings.Join(parts[1:], ":")
}
