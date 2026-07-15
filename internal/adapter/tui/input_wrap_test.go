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

// Space-less input (long paths, unspaced Korean) force-breaks the textarea's rows to
// their FULL width, and the input box must leave exactly that much room: Style.Width
// is the box's TOTAL width (border+padding included), and a box even 2 cols short
// re-wraps those full rows — rows lose their prompt, stray glyphs spill onto their
// own rows, and the reported cursor (where IME pre-edit composes) drifts by the
// accumulated difference. Spaced input hid the bug by wrapping early at word
// boundaries. Pin: every rendered box row keeps its prompt, the row count matches
// the textarea's, and the cursor lands at the text end (±1 cell).
func TestNoSpaceInputKeepsRowsAndCursor(t *testing.T) {
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(st, nil, builtin.Default(), bus.New(), nil, app.Config{})
	sid, _ := a.CreateSession(t.Context(), command.CreateSession{Workdir: t.TempDir()})

	for _, tc := range []struct{ name, text string }{
		{"korean no spaces", strings.Repeat("한글입력", 20)},
		{"ascii no spaces", strings.Repeat("abcdefgh", 20)},
		{"korean with spaces", strings.Repeat("한글입력 ", 16)},
	} {
		m := New(t.Context(), a, nil, sid, "m", "/tmp/wd", true, "")
		m.resize(100, 30)
		m.ready = true
		m.refresh()
		var mm tea.Model = m
		for _, r := range tc.text {
			mm, _ = mm.(Model).Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		}
		m = mm.(Model)

		taRows := len(strings.Split(m.ta.View(), "\n"))
		v := m.View()
		rows := strings.Split(v.Content, "\n")
		// Count the box's content rows and check each keeps its prompt.
		content := 0
		for _, r := range rows {
			plain := ansi.Strip(r)
			if !strings.Contains(plain, "│") {
				continue
			}
			content++
			if !strings.Contains(plain, "❯") {
				t.Errorf("%s: box row lost its prompt (re-wrapped): %q", tc.name, plain)
			}
		}
		if content != taRows {
			t.Errorf("%s: box shows %d content rows, textarea rendered %d (box re-wrapped)", tc.name, content, taRows)
		}
		// Cursor must sit at the end of the text on its row (±1 for the trailing cell).
		if v.Cursor == nil {
			t.Fatalf("%s: no cursor", tc.name)
		}
		cy := v.Cursor.Position.Y
		if cy < 0 || cy >= len(rows) {
			t.Fatalf("%s: cursor row %d off screen", tc.name, cy)
		}
		plain := strings.TrimRight(strings.TrimSuffix(strings.TrimRight(ansi.Strip(rows[cy]), " "), "│"), " ")
		end := ansi.StringWidth(plain)
		if d := v.Cursor.Position.X - end; d < -1 || d > 1 {
			t.Errorf("%s: cursor x=%d vs text end %d (Δ=%d)", tc.name, v.Cursor.Position.X, end, d)
		}
	}
}
