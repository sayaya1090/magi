package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
)

// Every user/assistant block's label line carries the ⧉ copy chip, clicking the chip
// copies that block's SOURCE text, and clicks elsewhere on the label line fall through.
func TestCopyChip(t *testing.T) {
	st, _ := jsonl.New(t.TempDir())
	a := app.New(st, nil, builtin.Default(), bus.New(), nil, app.Config{})
	sid, _ := a.CreateSession(t.Context(), command.CreateSession{Workdir: t.TempDir()})
	m := New(t.Context(), a, nil, sid, "m", "/tmp/wd", true, "")
	m.resize(100, 30)
	m.ready = true
	m.blocks = append(m.blocks,
		block{kind: blockUser, text: "질문 내용입니다"},
		block{kind: blockAssistant, text: "# 답변\n\n본문 **마크다운** 원문"},
	)
	m.refresh()

	// Label lines carry the chip glyph.
	chips := 0
	for _, l := range m.contentPlain {
		if strings.Contains(l, "⧉") {
			chips++
		}
	}
	if chips != 2 {
		t.Fatalf("want a copy chip on both label lines, found %d", chips)
	}

	// Assistant block: chip sits after "▌ magi " — click inside it copies the SOURCE.
	line := m.blockLineStart[1]
	chipCol := 2 + ansi.StringWidth("magi") + 1 + 1 // bar + name + space + inside chip padding
	cmd, ok := m.copyBlockAt(line, chipCol)
	if !ok || cmd == nil {
		t.Fatalf("chip click not consumed (line=%d col=%d)", line, chipCol)
	}
	// A click on the label text itself (col 3) is NOT the chip.
	if _, ok := m.copyBlockAt(line, 3); ok {
		t.Error("label-text click must fall through, not copy")
	}
	// A click on a body line is not the chip either.
	if _, ok := m.copyBlockAt(line+1, chipCol); ok {
		t.Error("body-line click must fall through")
	}
	// User block chip works too (custom labels shift the chip; geometry mirrors render).
	uline := m.blockLineStart[0]
	ucol := 2 + ansi.StringWidth(m.userLabel()) + 1 + 1
	if _, ok := m.copyBlockAt(uline, ucol); !ok {
		t.Error("user block chip click not consumed")
	}
}
