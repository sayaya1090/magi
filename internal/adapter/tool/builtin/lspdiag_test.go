package builtin

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestLspDiag(t *testing.T) {
	tool := LspDiag{}
	env := port.ToolEnv{Workdir: t.TempDir()}

	// Check if gopls is available
	_, goplsAvailable := exec.LookPath("gopls")

	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatal(err)
	}

	s := string(res.Content)

	if goplsAvailable != nil {
		// gopls not installed - should get helpful error message
		if !res.IsError {
			t.Fatal("expected an error result when gopls is missing")
		}
		if !strings.Contains(s, "gopls") || !strings.Contains(s, "go install") {
			t.Errorf("missing-gopls message should suggest installation, got: %s", s)
		}
	} else {
		// gopls is installed - should get diagnostics or "clean" message
		if res.IsError {
			t.Errorf("expected non-error result when gopls is available, got error: %s", s)
		}
		// Should contain either diagnostics or "clean" message
		if !strings.Contains(s, "diagnostic") && !strings.Contains(s, "clean") {
			t.Errorf("expected diagnostics or clean message, got: %s", s)
		}
	}
}

func TestLspDiagFallbackWhenMissing(t *testing.T) {
	// Test with manipulated PATH to ensure gopls is not found
	tool := LspDiag{}
	env := port.ToolEnv{Workdir: t.TempDir()}

	// Save and restore PATH
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)

	// Set PATH to empty to ensure gopls is not found
	os.Setenv("PATH", "")

	res, err := tool.Execute(context.Background(), json.RawMessage(`{}`), env)
	if err != nil {
		t.Fatal(err)
	}

	if !res.IsError {
		t.Fatal("expected an error result when gopls is not in PATH")
	}

	s := string(res.Content)
	if !strings.Contains(s, "gopls") || !strings.Contains(s, "go install") {
		t.Errorf("missing-gopls message should suggest installation, got: %s", s)
	}
}

func TestFormatGoplsDiagnostics(t *testing.T) {
	workdir := "/Users/test/project"
	output := "/Users/test/project/main.go:10:5: undefined: foo\n/Users/test/project/pkg/bar.go:20:1: unused import\n"
	result := formatGoplsDiagnostics(output, workdir)
	if !strings.Contains(result, "Found 2 diagnostic(s)") {
		t.Errorf("expected count in result, got: %s", result)
	}
	if !strings.Contains(result, "main.go:10:5") || !strings.Contains(result, "pkg/bar.go:20:1") {
		t.Errorf("expected relative paths, got: %s", result)
	}
}
