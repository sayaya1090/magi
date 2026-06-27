package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/session"
)

// defaultPanelWidth is the initial width of the right-hand status panel; the
// user can drag its left edge to resize (m.panelW).
const defaultPanelWidth = 44

// panelSID is the session whose status the panel reflects: the focused subagent
// when zoomed, otherwise the main session.
func (m *Model) panelSID() session.SessionID {
	if m.zoom {
		if sid, ok := m.focusedPaneSID(); ok {
			return sid
		}
	}
	return m.sid
}

// hasPanel reports whether the status panel has anything worth showing (a plan
// or active subagents). Hidden otherwise, per "없을 때 숨김".
func (m *Model) hasPanel() bool {
	if m.app == nil {
		return false
	}
	return len(m.app.Todos(m.panelSID())) > 0 || len(m.panes) > 0
}

// panelCols is the horizontal space the panel occupies (0 when hidden), incl. a
// one-column gap from the transcript.
func (m *Model) panelCols() int {
	if m.hasPanel() {
		return m.panelW + 1
	}
	return 0
}

// onPanelSplitter reports whether screen column x is on the panel's draggable
// left edge (the gap/border between the transcript and the panel).
func (m *Model) onPanelSplitter(x int) bool {
	if !m.hasPanel() {
		return false
	}
	split := m.width - m.panelCols()    // gap column; border is at split+1
	return x >= split-2 && x <= split+3 // generous grab zone (easier to grab)
}

// setPanelWidthForSplit resizes the panel so its left edge sits at column x,
// clamped to a sensible range.
func (m *Model) setPanelWidthForSplit(x int) {
	w := m.width - x - 1 // -1 for the gap column
	if w < 24 {
		w = 24
	}
	if maxW := m.width/2 - 1; w > maxW {
		w = maxW
	}
	m.panelW = w
}

// statusPanel renders the right-hand panel (plan/todos, active subagents,
// context) at the given height. Returns "" when hidden.
func (m *Model) statusPanel(height int) string {
	if !m.hasPanel() {
		return ""
	}
	// Total rendered width must equal m.panelW so the layout + splitter column
	// line up: content = panelW - border(1) - padding(2).
	content := m.panelW - 3
	inner := content
	var sections []string

	if todos := m.app.Todos(m.panelSID()); len(todos) > 0 {
		done := 0
		for _, t := range todos {
			if t.Status == "completed" {
				done++
			}
		}
		var b strings.Builder
		b.WriteString(panelHead(fmt.Sprintf("Plan  %d/%d", done, len(todos))))
		for _, t := range todos {
			b.WriteString("\n" + todoLine(t, inner))
		}
		sections = append(sections, b.String())
	}

	if len(m.panes) > 0 {
		var b strings.Builder
		b.WriteString(panelHead("Subagents"))
		for _, p := range m.panes {
			c := m.paneColorOf(p)
			b.WriteString("\n" + lipgloss.NewStyle().Foreground(c).Render("● ") +
				oneLine(p.label(), inner-4) + " " + m.paneStatus(p))
		}
		sections = append(sections, b.String())
	}

	if m.ctxPct > 0 {
		sections = append(sections, panelHead("Context")+"\n"+ctxBar(m.ctxPct, inner))
	}

	body := strings.Join(sections, "\n\n")
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colOutlVar).
		Width(content).
		Height(max(1, height)).
		Padding(0, 1).
		Render(body)
}

// panelHead renders a section header in the panel.
func panelHead(s string) string {
	return lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(s)
}

// todoLine renders one plan item with a status glyph.
func todoLine(t session.Todo, width int) string {
	switch t.Status {
	case "completed":
		return styleToolOK.Render("✓ ") + lipgloss.NewStyle().Foreground(colMuted).Strikethrough(true).Render(oneLine(t.Content, width-2))
	case "in_progress":
		return lipgloss.NewStyle().Foreground(colAccent).Render("◐ ") + lipgloss.NewStyle().Bold(true).Render(oneLine(t.Content, width-2))
	default:
		return lipgloss.NewStyle().Foreground(colMuted).Render("☐ " + oneLine(t.Content, width-2))
	}
}

// ctxBar renders a compact context-usage meter.
func ctxBar(pct float64, width int) string {
	barW := max(4, width-6)
	filled := int(pct / 100 * float64(barW))
	if filled > barW {
		filled = barW
	}
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", barW-filled)
	return lipgloss.NewStyle().Foreground(colPrimary).Render(bar) + fmt.Sprintf(" %2.0f%%", pct)
}
