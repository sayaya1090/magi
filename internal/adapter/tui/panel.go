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
	// The box's outer width is exactly panelW-4 TERMINAL cells (roundedBox guarantees
	// it); use that rather than lipgloss.Width(box), which counts emoji as two cells and
	// would drag the whole box left on rows that carry one.
	left = m.width - (m.panelW - 4) - floatMarginRight
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
		// Render the plan as a tree: after each step, the todos of any child session
		// spawned for that step (delegate/refine re-plan) render one level deeper. Each
		// node's status comes from its own session (single source of truth); the parent
		// step ↔ child session edge (PlanChildren) supplies only the structure.
		lines = m.appendPlanTree(lines, m.panelSID(), inner, 0)
	}

	if len(m.panes) > 0 || len(m.doneRoster) > 0 {
		sep()
		lines = append(lines, panelHead("Subagents"))
		paneRow := func(p *agentPane) {
			p.panelY = panelTop + len(lines) // screen Y for click→zoom (active and finished)
			c := m.paneColorOf(p)
			status := m.paneStatus(p)
			// Budget the label so "● <label> <status>" never exceeds the text width
			// (a wrap would push later rows off their recorded Y).
			labelW := inner - 3 - lipgloss.Width(status)
			if labelW < 4 {
				labelW = 4
			}
			lines = append(lines, lipgloss.NewStyle().Foreground(c).Render("● ")+
				oneLine(p.label(), labelW)+" "+status)
		}
		// List active panes AND faded-out ones (doneRoster) together in their original
		// SPAWN order (by sub), so a subagent keeps its position after it finishes
		// instead of jumping to the bottom. Both stay click-to-zoomable — a finished
		// pane opens via zoomPane since it's no longer in m.panes.
		rows := make([]*agentPane, 0, len(m.panes)+len(m.doneRoster))
		rows = append(rows, m.panes...)
		rows = append(rows, m.doneRoster...)
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].sub < rows[j].sub })
		for _, p := range rows {
			paneRow(p)
		}
	}

	if m.ctxPct > 0 {
		sep()
		lines = append(lines, panelHead("Context"), ctxBar(m.ctxPct, inner))
	}

	body := strings.Join(lines, "\n")
	return roundedBox(body, content)
}

// roundedBox draws body inside a rounded outline whose OUTER width is exactly
// `content` TERMINAL cells (per cellWidth), replacing lipgloss's Border().Width()
// render. lipgloss measures each row with lipgloss.Width — which counts emoji as
// two cells even when the terminal draws them in one — so an emoji in a todo left
// its row short and pushed the right │ out of line. Here every body row is laid to
// `content-4` cells with padOrTruncate (cellWidth-based, emoji-aware), then wrapped
// in "│ … │", so all rows occupy the same cells and the right border stays plumb.
// Outline only, interior transparent (no fill) — same rationale as before: a
// background would spill past the border cells.
func roundedBox(body string, content int) string {
	if content < 2 {
		return ""
	}
	bs := lipgloss.NewStyle().Foreground(colOutlVar)
	inner := content - 4 // text area: minus 2 border + 2 padding columns
	if inner < 0 {
		inner = 0
	}
	bar := bs.Render(strings.Repeat("─", content-2))
	var b strings.Builder
	b.WriteString(bs.Render("╭") + bar + bs.Render("╮"))
	for _, row := range strings.Split(body, "\n") {
		b.WriteByte('\n')
		b.WriteString(bs.Render("│") + " " + padOrTruncate(row, inner) + " " + bs.Render("│"))
	}
	b.WriteString("\n" + bs.Render("╰") + bar + bs.Render("╯"))
	return b.String()
}

// handlePanelClick maps a click in the right panel's subagent list to that
// subagent's detail view (focus + zoom), so a panel entry behaves like clicking
// its pane. Returns true when consumed.
func (m *Model) handlePanelClick(x, y int) bool {
	box, top, left, ok := m.floatPanel()
	if !ok {
		return false // no post-it on screen — let the click reach the transcript
	}
	w, h := m.panelW-4, lipgloss.Height(box) // outer width is exactly panelW-4 cells (see roundedBox)
	if x < left || x >= left+w || y < top || y >= top+h {
		return false // outside the floating box
	}
	for i, p := range m.panes {
		if p.panelY > 0 && y == p.panelY {
			m.focusPane = i
			m.zoomPane = nil // a live pane: follow focus
			m.zoom = true    // enter the subagent detail directly
			m.vp.GotoBottom()
			return true
		}
	}
	for _, p := range m.doneRoster {
		if p.panelY > 0 && y == p.panelY {
			m.focusPane = -1 // finished pane isn't in m.panes…
			m.zoomPane = p   // …so pin it directly for the zoom view
			m.zoom = true
			m.vp.GotoBottom()
			return true
		}
	}
	// Inside the box but not on a subagent row — consume it so it doesn't fall through
	// to the transcript and toggle a thought block that shares the clicked screen line.
	return true
}

// panelHead renders a post-it section header.
func panelHead(s string) string {
	return lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(s)
}

// appendPlanTree renders sid's todos at the given depth and recurses into each
// step's child sessions (PlanChildren), so a delegate/refine child's own sub-plan
// appears indented beneath the parent step it serves. The session tree is acyclic
// by construction (a child's Parent is an already-created session); planTreeMaxDepth
// is a defensive bound so pathological nesting can't run indentation off the panel.
func (m *Model) appendPlanTree(lines []string, sid session.SessionID, inner, depth int) []string {
	if depth > planTreeMaxDepth {
		return lines
	}
	todos := m.app.Todos(sid)
	for i, t := range todos {
		lines = append(lines, todoLine(t, inner, depth))
		for _, kid := range m.app.PlanChildren(sid, i) {
			lines = m.appendPlanTree(lines, kid, inner, depth+1)
		}
	}
	return lines
}

// planTreeMaxDepth caps plan-tree nesting depth in the panel. Recursive planning is
// itself bounded (MaxPlanDepth), so this only guards against unexpected deep chains.
const planTreeMaxDepth = 6

// todoLine renders one plan item with a status glyph. depth indents nested plan
// nodes (a child session's todos rendered under the parent step they serve), two
// spaces per level; the text width shrinks to match so the row still fits.
func todoLine(t session.Todo, width, depth int) string {
	indent := strings.Repeat("  ", depth)
	text := oneLine(t.Content, width-2-len(indent))
	switch t.Status {
	case "completed":
		return indent + lipgloss.NewStyle().Foreground(colSuccess).Render("✓ ") + lipgloss.NewStyle().Foreground(colMuted).Strikethrough(true).Render(text)
	case "in_progress":
		return indent + lipgloss.NewStyle().Foreground(colAccent).Render("◐ ") + lipgloss.NewStyle().Bold(true).Render(text)
	case "cancelled":
		return indent + lipgloss.NewStyle().Foreground(colError).Render("✗ ") + lipgloss.NewStyle().Foreground(colMuted).Strikethrough(true).Render(text)
	default:
		return indent + lipgloss.NewStyle().Foreground(colMuted).Render("☐ "+text)
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
