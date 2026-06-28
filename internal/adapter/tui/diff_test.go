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
	// base 0 → no gutter (no leading line numbers).
	bare := stripANSI(m.renderCodeDiff("+a", "x.go", 80, 0))
	if strings.ContainsAny(strings.TrimSpace(bare), "0123456789") {
		t.Errorf("base 0 should produce no gutter numbers: %q", bare)
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

func TestLineDiff(t *testing.T) {
	// A one-line change keeps surrounding lines as context and marks the swap.
	old := "#include <stdio.h>\n\nint main() {\n    printf(\"Hello, World!\\n\");\n    return 0;\n}"
	neu := "#include <stdio.h>\n\nint main() {\n    printf(\"Hello, World!!\\n\");\n    return 0;\n}"
	got := lineDiff(old, neu)
	wantHas := []string{
		" #include <stdio.h>",                 // context, space-prefixed
		"-    printf(\"Hello, World!\\n\");",  // removal
		"+    printf(\"Hello, World!!\\n\");", // addition
		" }",                                  // trailing context
	}
	for _, w := range wantHas {
		if !contains(got, w) {
			t.Errorf("lineDiff missing %q\n--- got ---\n%s", w, got)
		}
	}
}

func TestEditDiffWriteIsAllAdds(t *testing.T) {
	// A write shows its content as added lines.
	got := editDiff("write", `{"path":"main.c","content":"line1\nline2"}`)
	if got != "+line1\n+line2" {
		t.Errorf("write diff = %q", got)
	}
	// A non-edit/write tool yields nothing.
	if d := editDiff("bash", `{"cmd":"ls"}`); d != "" {
		t.Errorf("bash should have no diff, got %q", d)
	}
	// Unparseable args yield nothing, not a panic.
	if d := editDiff("edit", `not json`); d != "" {
		t.Errorf("bad args should yield no diff, got %q", d)
	}
}

func TestClampDiffBounds(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = "+x"
	}
	got := clampDiff(lines)
	if n := countLines(got); n != 41 { // 40 capped + 1 summary line
		t.Errorf("clampDiff line count = %d, want 41", n)
	}
	if !contains(got, "… (60 more lines)") {
		t.Errorf("clampDiff missing truncation note:\n%s", got)
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
