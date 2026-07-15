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

// colorizeChanges turns the council's "### path" change evidence into a per-file "◆ path"
// header plus colored +/- lines (assertions run on the ANSI-stripped text).
func TestColorizeChanges(t *testing.T) {
	applyTheme(true)
	in := "### main.go\n-old\n+new\n### util.go\n+added"
	out := stripANSI(colorizeChanges(in))
	for _, want := range []string{"◆ main.go", "◆ util.go", "-old", "+new", "+added"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in colorized changes:\n%s", want, out)
		}
	}
	if strings.Contains(out, "### ") {
		t.Errorf("the raw ### marker should be replaced by the ◆ header:\n%s", out)
	}
	if colorizeChanges("") != "" {
		t.Error("empty changes should stay empty")
	}
}

// councilVerdictLabel tiers a plan-audit continue by severity, while termination stays
// done/reject and abstain is unchanged.
func TestCouncilVerdictLabelSeverity(t *testing.T) {
	cases := []struct {
		phase, decision, severity, wantIcon, wantWord string
	}{
		{"plan", "done", "", "✓", "approve"},
		{"plan", "continue", "critical", "↻", "revise"},
		{"plan", "continue", "warn", "✎", "advise"},
		{"plan", "continue", "info", "·", "note"},
		{"plan", "continue", "", "✎", "advise"},      // absent severity → warn (non-blocking)
		{"plan", "continue", "bogus", "↻", "revise"}, // unrecognized → fail safe to blocking
		{"", "done", "", "✓", "done"},
		{"", "continue", "", "✗", "reject"},
		{"", "abstain", "", "∅", "abstain"},
	}
	for _, c := range cases {
		icon, word := councilVerdictLabel(c.phase, c.decision, c.severity)
		if icon != c.wantIcon || word != c.wantWord {
			t.Errorf("label(%q,%q,%q) = (%q,%q), want (%q,%q)",
				c.phase, c.decision, c.severity, icon, word, c.wantIcon, c.wantWord)
		}
	}
}
