package builtin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// Every tool (default registry + the orchestration tools) must expose a unique
// non-empty name, a non-empty description, and a schema that is valid JSON.
func TestToolMetadata(t *testing.T) {
	tools := append([]port.Tool{}, Default().List()...)
	tools = append(tools, Ask{}, Report{}, Task{})

	seen := map[string]bool{}
	for _, tl := range tools {
		name := tl.Name()
		if name == "" {
			t.Errorf("%T has an empty name", tl)
			continue
		}
		if seen[name] {
			t.Errorf("duplicate tool name %q", name)
		}
		seen[name] = true
		if strings.TrimSpace(tl.Description()) == "" {
			t.Errorf("%s: empty description", name)
		}
		var js any
		if err := json.Unmarshal(tl.Schema(), &js); err != nil {
			t.Errorf("%s: schema is not valid JSON: %v", name, err)
		}
	}
	for _, want := range []string{"ask", "report", "task"} {
		if !seen[want] {
			t.Errorf("orchestration tool %q missing from the set", want)
		}
	}
}

// serverFor maps file extensions to their language server (or ok=false for Go and
// unsupported types, which take other paths).
func TestServerFor(t *testing.T) {
	cases := map[string]string{
		"a.ts":  "typescript-language-server",
		"a.tsx": "typescript-language-server",
		"a.js":  "typescript-language-server",
		"a.py":  "pyright-langserver",
		"a.rs":  "rust-analyzer",
		"a.c":   "clangd",
		"a.cpp": "clangd",
	}
	for path, want := range cases {
		srv, ok := serverFor(path)
		if !ok || srv.argv[0] != want {
			t.Errorf("serverFor(%q) = %v ok=%v, want %s", path, srv.argv, ok, want)
		}
	}
	for _, path := range []string{"a.go", "a.txt", "noext"} {
		if _, ok := serverFor(path); ok {
			t.Errorf("serverFor(%q) should be unsupported (handled elsewhere)", path)
		}
	}
}
