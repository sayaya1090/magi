package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// runEdit applies an edit to a seeded file and returns (newContent, resultText, isErr).
func runEdit(t *testing.T, seed string, a editArgs) (string, string, bool) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "f.go")
	if err := os.WriteFile(p, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	a.Path = "f.go"
	raw, _ := json.Marshal(a)
	res, _ := Edit{}.Execute(context.Background(), raw, port.ToolEnv{Workdir: dir})
	b, _ := os.ReadFile(p)
	var msg string
	_ = json.Unmarshal(res.Content, &msg)
	return string(b), msg, res.IsError
}

// Trailing-whitespace differences in `old` are tolerated.
func TestEditToleratesTrailingWhitespace(t *testing.T) {
	seed := "package x\n\nfunc F() {\n\tx := 1   \n}\n" // note trailing spaces after "1"
	got, _, isErr := runEdit(t, seed, editArgs{Old: "\tx := 1\n", New: "\tx := 2\n"})
	if isErr {
		t.Fatalf("expected tolerant match, got error")
	}
	if !strings.Contains(got, "x := 2") || strings.Contains(got, "x := 1") {
		t.Errorf("edit not applied: %q", got)
	}
}

// CRLF files match an old string written with LF.
func TestEditToleratesCRLF(t *testing.T) {
	seed := "package x\r\n\r\nvar A = 1\r\n"
	got, _, isErr := runEdit(t, seed, editArgs{Old: "var A = 1\n", New: "var A = 2\n"})
	if isErr {
		t.Fatalf("expected CRLF-tolerant match, got error")
	}
	if !strings.Contains(got, "var A = 2") {
		t.Errorf("edit not applied: %q", got)
	}
	if !strings.Contains(got, "\r\n") {
		t.Errorf("CRLF endings should be preserved: %q", got)
	}
}

// Exact unique match is unchanged behavior.
func TestEditExactStillWorks(t *testing.T) {
	got, _, isErr := runEdit(t, "a=1\nb=2\n", editArgs{Old: "b=2", New: "b=3"})
	if isErr || !strings.Contains(got, "b=3") {
		t.Errorf("exact edit failed: %q err=%v", got, isErr)
	}
}

// An ambiguous (multi) whitespace-tolerant match errors without replaceAll.
func TestEditAmbiguousErrors(t *testing.T) {
	// Both lines have trailing whitespace so neither is an exact match; both match
	// flexibly → ambiguous.
	seed := "\tx := 1 \nmid\n\tx := 1  \n"
	_, msg, isErr := runEdit(t, seed, editArgs{Old: "\tx := 1\n", New: "\tx := 9\n"})
	if !isErr || !strings.Contains(msg, "not unique") {
		t.Errorf("expected not-unique error, got msg=%q isErr=%v", msg, isErr)
	}
}

// A leading-indentation mismatch is reported as a near-miss, not silently mis-applied.
func TestEditNearMissHint(t *testing.T) {
	// File indents with a TAB; old uses SPACES — not a substring and not a
	// flexible match (leading whitespace must match), so it's a near-miss.
	seed := "func F() {\n\tdeeply := indented\n}\n"
	_, msg, isErr := runEdit(t, seed, editArgs{Old: "    deeply := indented\n", New: "    deeply := changed\n"})
	if !isErr {
		t.Fatalf("expected near-miss error for indentation mismatch")
	}
	if !strings.Contains(msg, "indentation") {
		t.Errorf("expected indentation hint, got %q", msg)
	}
}
