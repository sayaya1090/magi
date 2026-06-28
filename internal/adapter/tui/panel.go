package tui

import (
	"fmt"
	"sort"
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
	return len(m.app.Todos(m.panelSID())) > 0 || len(m.panes) > 0 || len(m.doneRoster) > 0
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
func (m *Model) statusPanel(panelTop, height int) string {
	if !m.hasPanel() {
		return ""
	}
	// Total rendered width must equal m.panelW so the layout + splitter column
	// line up: content = panelW - border(1) - padding(2).
	content := m.panelW - 3
	inner := content
	// Build the body as flat lines so each subagent row's panel-relative Y can be
	// recorded for click hit-testing (right-panel click → zoom that subagent).
	var lines []string
	sep := func() {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
	}

	if todos := m.app.Todos(m.panelSID()); len(todos) > 0 {
		done := 0
		for _, t := range todos {
			if t.Status == "completed" {
				done++
			}
		}
		sep()
		lines = append(lines, panelHead(fmt.Sprintf("Plan  %d/%d", done, len(todos))))
		for _, t := range todos {
			lines = append(lines, todoLine(t, inner))
		}
	}

	if len(m.panes) > 0 || len(m.doneRoster) > 0 {
		sep()
		lines = append(lines, panelHead("Subagents"))
		paneRow := func(p *agentPane, interactive bool) {
			if interactive {
				p.panelY = panelTop + len(lines) // screen Y for click→zoom (active panes only)
			}
			c := m.paneColorOf(p)
			status := m.paneStatus(p)
			// Budget the label so "● <label> <status>" never exceeds the panel width
			// (a wrap would push later rows off their recorded Y). The extra -1 covers
			// oneLine's "…" ellipsis, which renders one cell past its max on truncation.
			labelW := inner - 4 - lipgloss.Width(status)
			if labelW < 4 {
				labelW = 4
			}
			lines = append(lines, lipgloss.NewStyle().Foreground(c).Render("● ")+
				oneLine(p.label(), labelW)+" "+status)
		}
		// List active panes AND faded-out ones (doneRoster) together in their original
		// SPAWN order (by sub), so a subagent keeps its position after it finishes
		// instead of jumping to the bottom. Active ones stay click-to-zoomable.
		type row struct {
			p           *agentPane
			interactive bool
		}
		rows := make([]row, 0, len(m.panes)+len(m.doneRoster))
		for _, p := range m.panes {
			rows = append(rows, row{p, true})
		}
		for _, p := range m.doneRoster {
			p.panelY = 0 // not inline-zoomable (no live pane)
			rows = append(rows, row{p, false})
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].p.sub < rows[j].p.sub })
		for _, r := range rows {
			paneRow(r.p, r.interactive)
		}
	}

	if m.ctxPct > 0 {
		sep()
		lines = append(lines, panelHead("Context"), ctxBar(m.ctxPct, inner))
	}

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(colOutlVar).
		Width(content).
		Height(max(1, height)).
		Padding(0, 1).
		Render(body)
}

// handlePanelClick maps a click in the right panel's subagent list to that
// subagent's detail view (focus + zoom), so a panel entry behaves like clicking
// its pane. Returns true when consumed.
func (m *Model) handlePanelClick(x, y int) bool {
	if m.zoom || !m.hasPanel() || len(m.panes) == 0 {
		return false
	}
	if split := m.width - m.panelCols(); x <= split { // click is left of the panel
		return false
	}
	for i, p := range m.panes {
		if p.panelY > 0 && y == p.panelY {
			m.focusPane = i
			m.zoom = true // enter the subagent detail directly
			m.vp.GotoBottom()
			return true
		}
	}
	return false
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
