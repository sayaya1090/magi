package tui

import (
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// bgSet matches an SGR run that sets a background: 48;2;r;g;b (truecolor) or 49 (reset to
// default). Reading the escapes is the only way to ask this question — the rendered text looks
// identical either way, and the defect is what the terminal paints behind it.
// styledPart is one SGR run and the text it paints.
type styledPart struct{ sgr, text string }

// splitStyled pairs each escape sequence with the text it introduces, so a span can be judged
// together with what it draws — a border glyph and a description are not the same question.
func splitStyled(row string) []styledPart {
	locs := ansiSeq.FindAllStringIndex(row, -1)
	var out []styledPart
	for k, loc := range locs {
		end := len(row)
		if k+1 < len(locs) {
			end = locs[k+1][0]
		}
		out = append(out, styledPart{sgr: row[loc[0]:loc[1]], text: row[loc[1]:end]})
	}
	return out
}

var bgSet = regexp.MustCompile(`\x1b\[[0-9;]*?(?:48;2;\d+;\d+;\d+|48;5;\d+|\b49\b)[0-9;]*m`)

// The palette's rows sit inside a box filled with the surface colour, and every segment of a row
// has to carry that fill. A foreground-only styled span RESETS the background, so the terminal's
// own default shows through behind those cells — invisible on a dark theme whose default is
// already dark, and a cream/white checkerboard on a light one. The palette carries a comment
// saying exactly this, because it happened there.
//
// No test in this package had ever rendered the light theme: eighty-six call applyTheme(true) and
// none called applyTheme(false). This is the one question that theme answers differently.
func TestThePaletteRowsCarryTheirFillOnALightTheme(t *testing.T) {
	applyTheme(false)
	defer applyTheme(true)

	s := newScript(t)
	// After newScript: it builds a Model through New, which applies its own theme.
	applyTheme(false)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.assistantText("some work happened")
	s.typeText("/")
	if len(s.m.paletteMatches()) < 3 {
		t.Fatal("too few matches for a row to be crowded, so nothing here is under test")
	}
	body := s.m.paletteBody(s.m.paletteMatches(), 0)

	rows := strings.Split(body, "\n")
	checked := 0
	for i, row := range rows {
		plain := ansiSeq.ReplaceAllString(row, "")
		if strings.TrimSpace(strings.Trim(plain, "│╭╮╰╯─")) == "" {
			continue // a border or a blank line has nothing to fill
		}
		checked++
		// Every span that draws CONTENT must name a background; a span that names only a
		// foreground hands those cells back to the terminal default. The box's own border is the
		// exception and the reason this walks spans with their text rather than spans alone: a
		// border glyph is drawn in the outline colour on the terminal's own ground, by design.
		for _, part := range splitStyled(row) {
			if strings.TrimLeft(part.text, "│╭╮╰╯─ ") == "" {
				continue // border or padding
			}
			if strings.Contains(part.sgr, "38;2;") && !bgSet.MatchString(part.sgr) {
				t.Errorf("row %d draws %q with a foreground-only span (%q), so the light terminal "+
					"shows through behind it", i, part.text, part.sgr)
				break
			}
		}
	}
	if checked == 0 {
		t.Error("no filled row was examined, so this verified nothing")
	}
}

// The same frame, drawn under both themes, must have the same shape. The themes differ only in
// colour, and a style that quietly gained a border or a pad on one of them would be a layout that
// only half the users see.
func TestBothThemesDrawTheSameShape(t *testing.T) {
	shape := func(dark bool) []int {
		applyTheme(dark)
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 80, Height: 24})
		s.assistantText("an answer that is long enough to wrap somewhere on this terminal, twice over")
		s.toolCall("bash", "c1")
		s.toolResult("c1", "line one\nline two\nline three")
		s.typeText("/")
		var out []int
		for _, l := range strings.Split(s.rawView(), "\n") {
			out = append(out, len(ansiSeq.ReplaceAllString(l, "")))
		}
		return out
	}
	light, dark := shape(false), shape(true)
	applyTheme(true)
	if len(light) != len(dark) {
		t.Fatalf("the light frame is %d rows and the dark one is %d", len(light), len(dark))
	}
	for i := range light {
		if light[i] != dark[i] {
			t.Errorf("row %d is %d cells on light and %d on dark", i, light[i], dark[i])
		}
	}
}
