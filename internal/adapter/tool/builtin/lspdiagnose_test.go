package builtin

import (
	"strings"
	"testing"
)

func TestFormatDiagnostics(t *testing.T) {
	// Empty, and info/hint-only, produce nothing (no correctness signal).
	if formatDiagnostics(nil, "x.py") != "" {
		t.Error("no diagnostics → empty")
	}
	infoOnly := []lspDiagnostic{{Severity: 3, Message: "style"}, {Severity: 4, Message: "hint"}}
	if formatDiagnostics(infoOnly, "x.py") != "" {
		t.Error("info/hint only → empty (not a correctness signal)")
	}
	// Errors sort before warnings; header counts all kept.
	diags := []lspDiagnostic{
		{Range: lspRng{lspPos{Line: 20, Char: 0}}, Severity: 2, Message: "unused import"},
		{Range: lspRng{lspPos{Line: 9, Char: 4}}, Severity: 1, Message: "undefined name 'bar'"},
	}
	out := formatDiagnostics(diags, "app.py")
	if !strings.HasPrefix(out, "2 diagnostic(s):") {
		t.Errorf("header wrong: %q", out)
	}
	ie := strings.Index(out, "error")
	iw := strings.Index(out, "warning")
	if ie < 0 || iw < 0 || ie > iw {
		t.Errorf("errors must be listed before warnings: %q", out)
	}
	if !strings.Contains(out, "app.py:10:5: error: undefined name 'bar'") {
		t.Errorf("1-based line:col + message expected: %q", out)
	}
}

func TestSeverityLabel(t *testing.T) {
	for s, want := range map[int]string{1: "error", 2: "warning", 3: "info", 4: "hint", 0: "note"} {
		if got := severityLabel(s); got != want {
			t.Errorf("severityLabel(%d)=%q want %q", s, got, want)
		}
	}
}
