package tui

import (
	"image/color"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// lexerFor returns a chroma lexer for the file at path (Fallback if unknown).
func lexerFor(path string) chroma.Lexer {
	if l := lexers.Match(path); l != nil {
		return l
	}
	return lexers.Fallback
}

// codeStyle returns the chroma color style matching the active theme.
func (m *Model) codeStyle() *chroma.Style {
	if m.isDark {
		return styles.Get("github-dark") // never nil — Get falls back to a default
	}
	return styles.Get("github")
}

// highlightTokens tokenizes code and renders it with each token's syntax color over
// the given base style (which may carry a background). Falls back to base on error.
func highlightTokens(code string, lexer chroma.Lexer, st *chroma.Style, base lipgloss.Style) string {
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return base.Render(code)
	}
	var b strings.Builder
	for _, tok := range it.Tokens() {
		val := strings.TrimRight(tok.Value, "\n")
		if val == "" {
			continue
		}
		seg := base
		if e := st.Get(tok.Type); e.Colour.IsSet() {
			seg = seg.Foreground(lipgloss.Color(e.Colour.String()))
		}
		b.WriteString(seg.Render(val))
	}
	return b.String()
}

// renderCodeDiff renders unified-diff text (from editDiff) as a syntax-highlighted
// view: code keeps its keyword colors (chroma, language inferred from path) while a
// full-width background wash — green for additions, red for removals — marks the
// change, instead of recoloring the whole line. width is the content width the wash
// fills. A line that isn't a +/-/space diff line (e.g. the "… (N more)" note) is
// rendered muted.
func (m *Model) renderCodeDiff(diffText, path string, width int) string {
	lexer := lexerFor(path)
	st := m.codeStyle()

	var out []string
	for _, line := range strings.Split(diffText, "\n") {
		if line == "" {
			out = append(out, "")
			continue
		}
		r0, _ := utf8.DecodeRuneInString(line)
		var bg, markerFg color.Color
		switch r0 {
		case '+':
			bg, markerFg = colDiffAddBg, colSuccess
		case '-':
			bg, markerFg = colDiffDelBg, colError
		case ' ':
			bg, markerFg = nil, colMuted // context: highlight only, no wash
		default:
			out = append(out, styleToolResult.Render(line)) // summary/other line
			continue
		}
		// marker is ASCII (+/-/space), so byte slicing is safe.
		out = append(out, highlightDiffLine(line[:1], line[1:], lexer, st, bg, markerFg, width))
	}
	return strings.Join(out, "\n")
}

// highlightDiffLine renders one diff line: the marker, then the code tokenized and
// colored by chroma, every segment carrying bg (so the wash is continuous across the
// per-token ANSI resets), padded to width so the wash spans the row.
func highlightDiffLine(marker, code string, lexer chroma.Lexer, st *chroma.Style, bg, markerFg color.Color, width int) string {
	base := lipgloss.NewStyle()
	if bg != nil {
		base = base.Background(bg)
	}
	line := base.Foreground(markerFg).Render(marker) + highlightTokens(code, lexer, st, base)
	if bg != nil {
		if vis := lipgloss.Width(line); vis < width {
			line += base.Render(strings.Repeat(" ", width-vis))
		}
	}
	return line
}
