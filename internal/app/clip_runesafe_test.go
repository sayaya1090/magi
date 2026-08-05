package app

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// Every clip that hands text to a model cuts on a rune boundary. truncateOutput did not: it took
// s[:max] flat, so any non-ASCII in a build log — a compiler quoting an identifier, a UTF-8 path,
// a typographic quote in an error message — could land mid-rune and put a broken byte at the seam
// of the very text the model is being asked to read and fix. It feeds two things straight to the
// model: `Build output:` on a failed verification, and the LSP diagnostics beside it.
func TestEveryClipCutsOnARuneBoundary(t *testing.T) {
	inputs := []string{
		"",
		"hello",
		strings.Repeat("x", 500),
		strings.Repeat("한글", 300),
		strings.Repeat("🔥", 300),
		strings.Repeat("é", 300),
		strings.Repeat("a\n", 300),
		"🔥",
	}
	for _, fn := range []struct {
		name string
		f    func(string, int) string
	}{
		{"clipLine", clipLine},
		{"clipSpec", clipSpec},
		{"truncateForCouncil", truncateForCouncil},
		{"truncateOutput", truncateOutput},
	} {
		for _, n := range []int{0, 1, 3, 10, 100} {
			for _, in := range inputs {
				got := fn.f(in, n)
				if !utf8.ValidString(got) {
					t.Errorf("%s(%q…, %d) split a rune: %q", fn.name, in[:min(12, len(in))], n, got)
				}
				// A payload that fits is handed over untouched.
				if n > 0 && len(in) <= n && got != in {
					t.Errorf("%s: %d bytes fit in %d and must pass through: %q", fn.name, len(in), n, got)
				}
			}
		}
	}
}

// capToolResult enforces the cap on EVERY path. The byte path appended its marker to a full-cap
// prefix, so it returned 65686 bytes for a 65536 cap — on non-JSON bytes, on multibyte non-JSON,
// and on a malformed JSON string, which is the one of the three a broken producer can deliver.
func TestCapToolResultHoldsTheCapOnEveryPath(t *testing.T) {
	big := func(s string, n int) []byte { return []byte(strings.Repeat(s, n)) }
	jsonStr, _ := json.Marshal(strings.Repeat("x", toolResultCap*2))
	band := func() []byte {
		var sb strings.Builder
		for sb.Len() < toolResultCap-600 {
			sb.WriteString("1\tint f(void);\n")
		}
		b, _ := json.Marshal(sb.String())
		return b
	}()
	arr, _ := json.Marshal(make([]string, 4000))
	for _, c := range []struct {
		name string
		in   []byte
	}{
		{"a JSON string over the cap", jsonStr},
		{"decoded under the cap, encoded over", band},
		{"a structured payload", arr},
		{"bytes that are not JSON", big("q", toolResultCap*2)},
		{"multibyte bytes that are not JSON", big("한", toolResultCap)},
		{"a malformed JSON string", append([]byte(`"`), big("w", toolResultCap*2)...)},
	} {
		out := capToolResult(c.in)
		if len(out) > toolResultCap {
			t.Errorf("%s: %d bytes out, cap %d", c.name, len(out), toolResultCap)
		}
		if !utf8.Valid(out) && utf8.Valid(c.in) {
			t.Errorf("%s: split a rune", c.name)
		}
	}
	// Anything already inside the cap is returned byte for byte.
	small, _ := json.Marshal("hello")
	if got := capToolResult(small); string(got) != string(small) {
		t.Errorf("an under-cap result passes through: %s", got)
	}
}
