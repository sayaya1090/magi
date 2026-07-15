package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
)

// Every splash section (wordmark, version, identity, input box) must share one
// center axis to within a cell. The wordmark used to sit 3-4 cells right of the
// box: logoBlock's JoinVertical pre-padded the art lines to the block width and
// the per-line centering then centered padding-included widths (double-centering).
func TestSplashSectionsShareCenterAxis(t *testing.T) {
	applyTheme(true)
	const vpw = 120
	box := "╭──────────╮\n│ ❯        │\n╰──────────╯"
	// A symmetric stand-in for the console diagram: equal-width lines (25) whose
	// glyphs are centered on one axis — the property splashConsole guarantees.
	logo := []string{
		"    ╔═══════════════╗    ",
		"    ║     SEAT!     ║    ",
		"    ╚═══════╦═══════╝    ",
		"       M  A  G  I        ",
	}
	content, _, _ := splashCompose(vpw, 40, logo, "model · ~/wd", box)
	for _, r := range strings.Split(content, "\n") {
		plain := strings.TrimRight(ansi.Strip(r), " ")
		if strings.TrimSpace(plain) == "" {
			continue
		}
		pad := len(plain) - len(strings.TrimLeft(plain, " "))
		w := ansi.StringWidth(plain) - pad
		center := pad + w/2
		if d := center - vpw/2; d < -1 || d > 1 {
			t.Errorf("line off the center axis by %d cells: %q", d, strings.TrimSpace(plain))
		}
	}
}

// A long paste that soft-wraps past the input's MaxHeight must reposition the
// textarea's internal viewport: InsertString alone left the view on the TOP rows
// while the reported cursor row pointed past the visible window, so the terminal
// cursor rendered outside the input box until the next keypress.
func TestPasteRepositionsCursorInsideBox(t *testing.T) {
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(st, nil, builtin.Default(), bus.New(), nil, app.Config{})
	sid, _ := a.CreateSession(t.Context(), command.CreateSession{Workdir: t.TempDir()})
	m := New(t.Context(), a, nil, sid, "test-model", "/tmp/wd", true, "")
	m.resize(100, 30)
	m.ready = true
	m.refresh()

	// Sequential inline pastes (each under pasteThreshold) accumulating past
	// maxInputRows of soft-wrapped content — the reported real-usage shape.
	chunk := strings.Repeat("pasted-content ", 12) // 180 chars, inline path
	var mm tea.Model = m
	for i := 0; i < 4; i++ {
		mm, _ = mm.(Model).Update(tea.PasteMsg{Content: chunk})
	}
	m = mm.(Model)

	v := m.View()
	if v.Cursor == nil {
		t.Fatal("no cursor reported")
	}
	rows := strings.Split(v.Content, "\n")
	cy := v.Cursor.Position.Y
	if cy < 0 || cy >= len(rows) {
		t.Fatalf("cursor row %d outside the screen (%d rows)", cy, len(rows))
	}
	plain := ansi.Strip(rows[cy])
	if !strings.Contains(plain, "│") {
		t.Errorf("cursor row %d is not inside the input box: %q", cy, plain)
	}
	// The row under the cursor must hold the pasted TAIL — some non-blank content
	// (soft wrap may split mid-word, so don't demand a whole word), and no row of
	// the box below it may hold more text (the cursor sits on the last content row).
	interior := strings.Trim(plain, " │")
	if strings.TrimSpace(interior) == "" {
		t.Errorf("cursor row should hold the pasted tail, got blank interior: %q", plain)
	}
	for i := cy + 1; i < len(rows); i++ {
		p := ansi.Strip(rows[i])
		if !strings.Contains(p, "│") {
			break // past the box bottom
		}
		if strings.TrimSpace(strings.Trim(p, " │")) != "" {
			t.Errorf("row %d below the cursor still holds content %q — cursor is not on the last row", i, p)
		}
	}
}
