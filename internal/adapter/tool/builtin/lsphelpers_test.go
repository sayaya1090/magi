package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

func TestIsGo(t *testing.T) {
	if !isGo("a/b.go") || isGo("a.ts") || isGo("Makefile") {
		t.Error("isGo should be true only for .go files")
	}
}

func TestRelativize(t *testing.T) {
	got := relativize("/w/x/a.go:3:1\n/w/x/b.go:5:2", "/w/x")
	if strings.Contains(got, "/w/x/") {
		t.Errorf("relativize left absolute paths: %q", got)
	}
	if !strings.Contains(got, "a.go:3:1") {
		t.Errorf("relativize = %q", got)
	}
}

func TestSymbolKind(t *testing.T) {
	if symbolKind(12) != "function" || symbolKind(5) != "class" || symbolKind(23) != "struct" {
		t.Errorf("symbolKind mapping wrong: %q %q %q", symbolKind(12), symbolKind(5), symbolKind(23))
	}
	if symbolKind(0) != "symbol" || symbolKind(999) != "symbol" {
		t.Error("out-of-range kind should fall back to 'symbol'")
	}
}

func TestUTF16Col(t *testing.T) {
	if c := utf16Col("abcdef", 3); c != 3 {
		t.Errorf("ascii col = %d, want 3", c)
	}
	// "한" is 3 bytes, 1 UTF-16 unit.
	if c := utf16Col("한국", len("한")); c != 1 {
		t.Errorf("CJK col = %d, want 1", c)
	}
	// byteIdx past end clamps.
	if c := utf16Col("ab", 99); c != 2 {
		t.Errorf("clamped col = %d, want 2", c)
	}
}

func TestFormatLocations(t *testing.T) {
	// LSP positions are 0-based; output is 1-based file:line:col, workspace-relative.
	res := json.RawMessage(`[{"uri":"file:///w/a.go","range":{"start":{"line":4,"character":2}}}]`)
	got := formatLocations(res, "/w")
	if got != "a.go:5:3" {
		t.Errorf("formatLocations = %q, want a.go:5:3", got)
	}
	if formatLocations(json.RawMessage(`null`), "/w") != "" {
		t.Error("null result should format to empty")
	}
}

func TestFormatSymbols(t *testing.T) {
	res := json.RawMessage(`[{"name":"Foo","kind":12,"range":{"start":{"line":2}},"children":[{"name":"bar","kind":6,"range":{"start":{"line":3}}}]}]`)
	got := formatSymbols(res)
	if !strings.Contains(got, "Foo (function) :3") || !strings.Contains(got, "bar (method) :4") {
		t.Errorf("formatSymbols = %q", got)
	}
}

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register(Read{})
	if _, ok := r.Get("read"); !ok {
		t.Fatal("read should be registered")
	}
	r.Unregister("read")
	if _, ok := r.Get("read"); ok {
		t.Error("read should be gone after Unregister")
	}
}

// LSP nav Execute argument-validation paths (no language server needed).
func TestLspNavArgErrors(t *testing.T) {
	ctx := context.Background()
	env := port.ToolEnv{Workdir: t.TempDir()}
	// Non-Go path with neither col nor symbol → error from resolveByteCol.
	if r, _ := (Lsp{}).Execute(ctx, json.RawMessage(`{"kind":"definition","path":"a.ts","line":1}`), env); !r.IsError {
		t.Error("definition without col/symbol should error")
	}
	if r, _ := (Lsp{}).Execute(ctx, json.RawMessage(`{"kind":"references","path":"a.ts","line":0}`), env); !r.IsError {
		t.Error("references with line<1 should error")
	}
	// Invalid JSON args → error.
	if r, _ := (Lsp{}).Execute(ctx, json.RawMessage(`not json`), env); !r.IsError {
		t.Error("invalid args should error")
	}
	// Unknown kind → error.
	if r, _ := (Lsp{}).Execute(ctx, json.RawMessage(`{"kind":"bogus","path":"a.ts"}`), env); !r.IsError {
		t.Error("unknown kind should error")
	}
}

// websearch Execute validates the query before any network call.
func TestWebSearchArgErrors(t *testing.T) {
	ctx := context.Background()
	if r, _ := (WebSearch{}).Execute(ctx, json.RawMessage(`{"query":"  "}`), port.ToolEnv{}); !r.IsError {
		t.Error("blank query should error before any request")
	}
	if r, _ := (WebSearch{}).Execute(ctx, json.RawMessage(`bad`), port.ToolEnv{}); !r.IsError {
		t.Error("invalid args should error")
	}
}
