package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePos(t *testing.T) {
	dir := t.TempDir()
	src := "package p\n\nfunc Foo() int { return 0 }\n"
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// Column resolved from a symbol on the line: "Foo" starts at byte 6 (1-based col 6).
	pos, err := resolvePos(dir, lspPosArgs{Path: "x.go", Line: 3, Symbol: "Foo"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(pos, "x.go:3:6") {
		t.Errorf("pos = %q, want suffix x.go:3:6", pos)
	}

	// Explicit column is used as-is.
	pos, err = resolvePos(dir, lspPosArgs{Path: "x.go", Line: 3, Col: 9})
	if err != nil || !strings.HasSuffix(pos, "x.go:3:9") {
		t.Errorf("explicit col: pos=%q err=%v", pos, err)
	}

	// A symbol not on the line is a clean error.
	if _, err := resolvePos(dir, lspPosArgs{Path: "x.go", Line: 3, Symbol: "Bar"}); err == nil {
		t.Error("expected error for missing symbol")
	}
	// Neither col nor symbol is an error.
	if _, err := resolvePos(dir, lspPosArgs{Path: "x.go", Line: 3}); err == nil {
		t.Error("expected error when neither col nor symbol given")
	}
}
