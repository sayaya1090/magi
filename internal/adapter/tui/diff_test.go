package tui

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestParseTrailingAt(t *testing.T) {
	cases := map[string]int{
		"edited main.c @3":                 3,
		"edited a.go (EOL-normalized) @12": 12,
		"edited main.c":                    0,
		"wrote 79 bytes to main.c":         0,
	}
	for in, want := range cases {
		if got := parseTrailingAt(in); got != want {
			t.Errorf("parseTrailingAt(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestRenderCodeDiffGutter(t *testing.T) {
	applyTheme(true)
	m := &Model{width: 100, isDark: true}
	// A write (base line 1): additions numbered 1,2,3 in the gutter.
	out := stripANSI(m.renderCodeDiff("+a\n+b\n+c", "x.go", 80, 1))
	for _, n := range []string{"1 ", "2 ", "3 "} {
		if !strings.Contains(out, n) {
			t.Errorf("gutter missing %q\n%s", n, out)
		}
	}
	// base 0 → no gutter. Use content that itself contains a digit ("9") so "no digit
	// anywhere" can't be a false proxy; assert the line-number gutter cell ("1 ") that a
	// gutter WOULD start with is absent, and that base 1 of the same content DOES have it.
	bare := stripANSI(m.renderCodeDiff("+x9", "x.go", 80, 0))
	if strings.Contains(bare, "1 ") {
		t.Errorf("base 0 should have no line-number gutter, got: %q", bare)
	}
	withGutter := stripANSI(m.renderCodeDiff("+x9", "x.go", 80, 1))
	if !strings.Contains(withGutter, "1 ") {
		t.Errorf("base 1 should number line 1 (gutter cell \"1 \"), got: %q", withGutter)
	}
}

func TestHighlightDiffLine(t *testing.T) {
	applyTheme(true)
	lexer := lexers.Get("c")
	if lexer == nil {
		lexer = lexers.Fallback
	}
	st := styles.Get("github-dark")
	// An added line carries a background wash across the full content width.
	add := highlightDiffLine("+", "    return 0;", lexer, st, colDiffAddBg, colSuccess, 40)
	if w := lipgloss.Width(add); w != 40 {
		t.Errorf("added line should be padded to width 40 for the wash, got %d", w)
	}
	if !strings.Contains(add, "\x1b[") {
		t.Error("expected ANSI styling (syntax + background) in the added line")
	}
	// A context line has no wash, so it is not padded to full width.
	ctx := highlightDiffLine(" ", "int main() {", lexer, st, nil, colMuted, 40)
	if w := lipgloss.Width(ctx); w >= 40 {
		t.Errorf("context line should not be padded to full width, got %d", w)
	}
}

// prettyCouncilDiff strips git plumbing headers, shows a clean per-file header, and keeps
// the +/- content (color is applied but assertions run on the ANSI-stripped text).
func TestPrettyCouncilDiff(t *testing.T) {
	applyTheme(true)
	in := "diff --git a/main.go b/main.go\n" +
		"index 111..222 100644\n--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/logo.png b/logo.png\nBinary files /dev/null and b/logo.png differ"
	out := stripANSI(prettyCouncilDiff(in))
	for _, gone := range []string{"diff --git", "index 111", "--- a/main.go", "+++ b/main.go", "Binary files"} {
		if strings.Contains(out, gone) {
			t.Errorf("plumbing line %q should be stripped:\n%s", gone, out)
		}
	}
	for _, want := range []string{"◆ main.go", "◆ logo.png", "@@ -1 +1 @@", "-old", "+new"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in cleaned diff:\n%s", want, out)
		}
	}
	if prettyCouncilDiff("") != "" {
		t.Error("empty diff should stay empty")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func countLines(s string) int {
	n := 1
	for _, c := range s {
		if c == '\n' {
			n++
		}
	}
	return n
}
