package builtin

import (
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// A file saved NFD on disk (the macOS Hangul case) must match a pattern the model types
// NFC. Before the fix, byte-literal regex silently missed every Korean line.
func TestGrepMatchesNFDFileWithNFCPattern(t *testing.T) {
	nfc := "함수 정의"
	nfd := norm.NFD.String(nfc)
	if nfc == nfd {
		t.Fatal("setup: NFC and NFD forms are identical")
	}
	rows, isErr := runJSON(t, Grep{}, map[string]any{"pattern": nfc}, func(d string) {
		writeFile(d, "kor.go", "package x\n// "+nfd+"\nfunc F() {}\n")
	})
	if isErr {
		t.Fatal("grep should not error")
	}
	got := grepJoin(rows)
	if !strings.Contains(got, "kor.go") {
		t.Errorf("NFC pattern should match the NFD file; got %q", got)
	}
	// The emitted line keeps the original on-disk bytes (NFD), not a re-normalized copy.
	if !strings.Contains(got, nfd) {
		t.Errorf("output should preserve the original file bytes; got %q", got)
	}
}

// grepJoin flattens grep's JSON string-array result into one string for substring checks.
func grepJoin(rows []any) string {
	var b strings.Builder
	for _, r := range rows {
		if s, ok := r.(string); ok {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// ASCII patterns must be untouched by the normalization path — exact byte behavior.
func TestGrepASCIIPatternUnaffected(t *testing.T) {
	rows, isErr := runJSON(t, Grep{}, map[string]any{"pattern": "parse config"}, func(d string) {
		writeFile(d, "eng.go", "package y\n// parse config here\nfunc G() {}\n")
	})
	if isErr {
		t.Fatal("grep should not error")
	}
	if got := grepJoin(rows); !strings.Contains(got, "eng.go") {
		t.Errorf("ASCII pattern regressed; got %q", got)
	}
}

// findcontext ranks by fuzzy relevance; an NFD file must surface for the NFC query the
// model types. Before the fix, strings.Contains missed it and the file scored 0.
func TestFindContextMatchesNFDFileWithNFCQuery(t *testing.T) {
	nfc := "함수 정의"
	nfd := norm.NFD.String(nfc)
	if nfc == nfd {
		t.Fatal("setup: NFC and NFD forms are identical")
	}
	got, isErr := runJSON(t, FindContext{}, findCtxArgs{Query: nfc}, func(d string) {
		writeFile(d, "kor.go", "package x\n// "+nfd+"\nfunc F() {}\n")
		writeFile(d, "util/math.go", "package util\n\nfunc Add(a, b int) int { return a + b }\n")
	})
	if isErr {
		t.Fatalf("findcontext should not error: %v", got)
	}
	if len(got) == 0 {
		t.Fatal("NFC query should locate the NFD file mentioning 함수 정의")
	}
	if top := got[0].(map[string]any)["path"].(string); top != "kor.go" {
		t.Errorf("NFD-content file should rank first; got %q", top)
	}
}
