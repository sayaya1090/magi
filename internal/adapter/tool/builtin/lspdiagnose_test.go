package builtin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// frame wraps a JSON value in an LSP "Content-Length" header, as a server writes it.
func frame(v any) string {
	b, _ := json.Marshal(v)
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(b), b)
}

func diagMsg(uri string, diags []map[string]any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"method":  "textDocument/publishDiagnostics",
		"params":  map[string]any{"uri": uri, "diagnostics": diags},
	}
}

// collectDiagnostics must skip an initial empty publish and a server→client
// request, then return the first populated diagnostics set for our URI.
func TestCollectDiagnosticsReturnsPopulated(t *testing.T) {
	uri := "file:///work/app.py"
	stream := frame(diagMsg(uri, nil)) + // empty (not-yet-analyzed)
		frame(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "workspace/configuration", "params": map[string]any{}}) +
		frame(diagMsg(uri, []map[string]any{{
			"range":    map[string]any{"start": map[string]any{"line": 9, "character": 4}},
			"severity": 1, "message": "undefined name 'bar'",
		}}))
	c := &lspClient{out: bufio.NewReader(strings.NewReader(stream)), in: nopWriteCloser{io.Discard}}
	got := c.collectDiagnostics(context.Background(), uri, 2*time.Second, 500*time.Millisecond)
	if len(got) != 1 || got[0].Severity != 1 || !strings.Contains(got[0].Message, "undefined name") {
		t.Fatalf("got %+v, want one error diagnostic", got)
	}
}

// A clean file (only an empty publish, then the stream ends) yields no diagnostics
// without waiting the full timeout.
func TestCollectDiagnosticsCleanFile(t *testing.T) {
	uri := "file:///work/ok.rs"
	c := &lspClient{out: bufio.NewReader(strings.NewReader(frame(diagMsg(uri, nil)))), in: nopWriteCloser{io.Discard}}
	start := time.Now()
	got := c.collectDiagnostics(context.Background(), uri, 3*time.Second, 500*time.Millisecond)
	if len(got) != 0 {
		t.Fatalf("clean file should have no diagnostics, got %+v", got)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("clean file waited too long (%s) — should return on stream end", time.Since(start))
	}
}

// Diagnostics for a different file are ignored.
func TestCollectDiagnosticsIgnoresOtherURI(t *testing.T) {
	want := "file:///work/a.ts"
	stream := frame(diagMsg("file:///work/other.ts", []map[string]any{{
		"range": map[string]any{"start": map[string]any{"line": 1}}, "severity": 1, "message": "nope",
	}}))
	c := &lspClient{out: bufio.NewReader(strings.NewReader(stream)), in: nopWriteCloser{io.Discard}}
	if got := c.collectDiagnostics(context.Background(), want, time.Second, 300*time.Millisecond); len(got) != 0 {
		t.Errorf("diagnostics for another file must be ignored, got %+v", got)
	}
}

func TestSameURI(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"file:///a/b.py", "file:///a/b.py"},
		{"file:///a/b.py", "file://a/b.py"},
		{"file:///a/b.py", "/a/b.py"},
	} {
		if !sameURI(c.a, c.b) {
			t.Errorf("sameURI(%q,%q) = false, want true", c.a, c.b)
		}
	}
	if sameURI("file:///a/b.py", "file:///a/c.py") {
		t.Error("distinct files must not compare equal")
	}
}

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
