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

// panelCols is the horizontal space the panel RESERVES in the layout. The panel is a
// floating post-it overlaid on the top-right corner, so it reserves nothing — the
// transcript uses the full width and the box is composited over it (see floatPanel).
func (m *Model) panelCols() int { return 0 }

// onPanelSplitter reports whether (x,y) is on the post-it's draggable LEFT edge —
// drag it to resize the box's width (the height stays content-driven).
func (m *Model) onPanelSplitter(x, y int) bool {
	box, top, left, ok := m.floatPanel()
	if !ok {
		return false
	}
	return y >= top && y < top+lipgloss.Height(box) && x >= left-1 && x <= left+1
}

// setPanelWidthForSplit resizes the post-it so its left edge follows column x
// (the box's right edge stays at width-floatMarginRight), clamped to a usable range.
func (m *Model) setPanelWidthForSplit(x int) {
	// The box's outer width is panelW-4 (border+padding inset), so its left edge sits
	// at width-(panelW-4)-floatMarginRight. Solve for panelW that lands the edge at x.
	w := m.width - floatMarginRight - x + 4
	if w < 24 {
		w = 24
	}
	if maxW := m.width/2 - 1; w > maxW {
		w = maxW
	}
	m.panelW = w
}

// floatMarginTop/Right are the M3-style margins keeping the post-it off the very edge.
const (
	floatMarginTop   = 1
	floatMarginRight = 2
	headerRows       = 2 // title + divider (stable)
)

// floatPanel renders the post-it box and its top-left screen position, or ok=false
// when there's nothing to show or the terminal is too narrow to float it without
// crowding the transcript. statusPanel records each subagent row's panelY for clicks.
func (m *Model) floatPanel() (box string, top, left int, ok bool) {
	if m.zoom || !m.hasPanel() {
		return "", 0, 0, false
	}
	top = headerRows + floatMarginTop
	box = m.statusPanel(top + 1) // panelTop = first content row (just inside the top border)
	left = m.width - lipgloss.Width(box) - floatMarginRight
	if left < 24 {
		return "", 0, 0, false // keep a usable transcript width
	}
	// Don't paint over the input/footer (or a modal above them) on a short terminal:
	// reserve a few bottom rows. If the box can't fit above them, hide it.
	if m.height > 0 && top+lipgloss.Height(box) > m.height-4 {
		return "", 0, 0, false
	}
	return box, top, left, true
}

// statusPanel renders the floating post-it: a content-height rounded box of width
// panelW. panelTop is the SCREEN row of its first content line, so each subagent
// row's panelY maps clicks correctly. Returns "" when hidden.
func (m *Model) statusPanel(panelTop int) string {
	if !m.hasPanel() {
		return ""
	}
	// content is the box's OUTER width (lipgloss insets border+padding); the usable
	// text area inside is content - border(2) - padding(2). Budget rows to `inner` so
	// they never wrap — a wrapped row would shift every later panelY and break clicks.
	content := m.panelW - 4
	inner := content - 4
	surf := lipgloss.NewStyle().Background(colSurface) // cream fill behind every inner segment
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
			status := m.paneStatus(p, colSurface)
			// Budget the label so "● <label> <status>" never exceeds the panel width
			// (a wrap would push later rows off their recorded Y). The extra -1 covers
			// oneLine's "…" ellipsis, which renders one cell past its max on truncation.
			labelW := inner - 3 - lipgloss.Width(status)
			if labelW < 4 {
				labelW = 4
			}
			lines = append(lines, surf.Foreground(c).Render("● ")+
				surf.Render(oneLine(p.label(), labelW)+" ")+status)
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
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colOutlVar).
		BorderBackground(colSurface). // border cell is cream too — the whole card is one
		Background(colSurface).       // surface color, so cream meets white at the card's
		Width(content).               // outer edge (a clean cell boundary), not mid-glyph.
		Padding(0, 1).
		Render(body)
}

// handlePanelClick maps a click in the right panel's subagent list to that
// subagent's detail view (focus + zoom), so a panel entry behaves like clicking
// its pane. Returns true when consumed.
func (m *Model) handlePanelClick(x, y int) bool {
	box, top, left, ok := m.floatPanel()
	if !ok {
		return false // no post-it on screen — let the click reach the transcript
	}
	w, h := lipgloss.Width(box), lipgloss.Height(box)
	if x < left || x >= left+w || y < top || y >= top+h {
		return false // outside the floating box
	}
	for i, p := range m.panes {
		if p.panelY > 0 && y == p.panelY {
			m.focusPane = i
			m.zoom = true // enter the subagent detail directly
			m.vp.GotoBottom()
			return true
		}
	}
	// Inside the box but not on a subagent row — consume it so it doesn't fall through
	// to the transcript and toggle a thought block that shares the clicked screen line.
	return true
}

// panelHead renders a post-it section header (on the cream surface).
func panelHead(s string) string {
	return lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Background(colSurface).Render(s)
}

// todoLine renders one plan item with a status glyph (on the cream surface).
func todoLine(t session.Todo, width int) string {
	surf := lipgloss.NewStyle().Background(colSurface)
	text := oneLine(t.Content, width-2)
	switch t.Status {
	case "completed":
		return surf.Foreground(colSuccess).Render("✓ ") + surf.Foreground(colMuted).Strikethrough(true).Render(text)
	case "in_progress":
		return surf.Foreground(colAccent).Render("◐ ") + surf.Bold(true).Render(text)
	default:
		return surf.Foreground(colMuted).Render("☐ " + text)
	}
}

// ctxBar renders a compact context-usage meter (on the cream surface).
func ctxBar(pct float64, width int) string {
	surf := lipgloss.NewStyle().Background(colSurface)
	barW := max(4, width-6)
	filled := int(pct / 100 * float64(barW))
	if filled > barW {
		filled = barW
	}
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", barW-filled)
	return surf.Foreground(colPrimary).Render(bar) + surf.Render(fmt.Sprintf(" %2.0f%%", pct))
}
