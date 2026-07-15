package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sayaya1090/magi/internal/core/event"
)

func (m Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	// Mouse capture is always on: the wheel scrolls, click focuses a pane, and
	// drag selects+copies in-app — so there's no mode toggle for the user to learn.
	v.MouseMode = tea.MouseModeCellMotion
	if m.quitting {
		return v
	}
	if !m.ready {
		v.Content = "starting magi…"
		return v
	}

	var headLine string
	if m.councilDetail != nil {
		// Council detail view: a clickable breadcrumb back to the transcript.
		c := m.councilColor(m.councilDetail.Member)
		headLine = styleClickable.Render("‹ back") + styleHeader.Render("   ") +
			styleBrand.Render("✦ magi") + styleHeader.Render(" › ") +
			lipgloss.NewStyle().Foreground(c).Bold(true).Render("⚖ "+m.councilDetail.Member+" verdict")
	} else if vp := m.viewedPane(); m.zoom && vp != nil {
		// Zoom view: the header is a clickable breadcrumb back to the overview.
		p := vp
		c := m.paneColorOf(p)
		headLine = styleClickable.Render("‹ back") + styleHeader.Render("   ") +
			styleBrand.Render("✦ magi") + styleHeader.Render(" › ") +
			lipgloss.NewStyle().Foreground(c).Bold(true).Render(p.desc(max(20, m.width-24))) + "  " + m.paneStatus(p)
	} else {
		headLine = styleBrand.Render("✦ magi") +
			styleHeader.Render("   model "+m.model+"   ") +
			permChip(m.app.Permission())
		if m.plannerMode != "" {
			headLine += "  " + styleKeyLabel.Render("◈ plan: "+m.plannerMode)
		}
		// Live plan progress + the currently-running (sub-)step, so a refine/delegate
		// child's re-plan surfaces in the header the same way it nests in the panel. Only
		// the in-progress leaf is shown (accent+bold via styleKeyLabel) — pending/done
		// steps stay in the panel — so the header highlights just the active step.
		if done, total, crumbs := activePlanPath(m.app, m.sid); total > 0 && len(crumbs) > 0 {
			step := oneLine(strings.Join(crumbs, " › "), max(12, m.width/3))
			headLine += "  " + styleKeyLabel.Render(fmt.Sprintf("◐ %s  %d/%d", step, done, total))
		}
		if m.councilRound > 0 {
			chip := fmt.Sprintf("⚖ council r%d", m.councilRound)
			if m.councilMember != "" {
				chip += ": " + m.councilMember
			}
			headLine += "  " + styleKeyLabel.Render(chip)
		}
		if len(m.activeAgents) > 0 {
			// Status chip, not a control: render like the sibling plan/council chips
			// (accent label, no fill). A filled background reads as tappable, but the
			// running-agent count is display-only — nothing happens when you click it.
			headLine += "  " + styleKeyLabel.Render(fmt.Sprintf("⛐ %d: %s", len(m.activeAgents), agentSummary(m.activeAgents)))
		}
		// Right-align the scroll meter to the far edge so it stops crowding the plan/council/
		// agent chips. styleHeader pads 1 cell on each side, so the content budget is width-2;
		// fall back to an inline append when there isn't room to push it right.
		if sm := m.scrollMeter(); sm != "" {
			if gap := m.width - 2 - ansi.StringWidth(headLine) - ansi.StringWidth(sm); gap > 0 {
				headLine += strings.Repeat(" ", gap) + sm
			} else {
				headLine += "  " + sm
			}
		}
	}
	// Cap the header to the screen width so a long chip/badge can't SOFT-WRAP to a
	// second physical row — that would desync physical rows from the logical layout
	// (headerRows=2) and throw off the post-it/toast overlay click hit-testing.
	header := styleHeader.MaxWidth(m.width).Render(headLine) +
		"\n" + styleDivider.Render(strings.Repeat("─", max(1, m.width)))

	var status string
	switch {
	case m.running:
		// While a council round is open the agent is blocked waiting on the panel's
		// verdict, not running tools — a generic "working…" reads as "maybe stuck".
		// Name the awaited judgment (fixed phrase, no model query) so the spinner is
		// clearly attached to the council, resolving on council.decided.
		work := "working… "
		if m.councilRound > 0 {
			work = councilWaitLabel(m.councilPhase) + " "
		}
		status = "  " + m.sp.View() + styleFooter.Render(" "+work+turnMeter(time.Since(m.turnStart), m.turnIn, m.turnOut)+gaugeSep(m.ctxGauge())+"  ") + footerKeys("esc", "interrupt")
	case m.turnDur > 0:
		status = styleFooter.Render("  "+turnMeter(m.turnDur, m.turnIn, m.turnOut)+gaugeSep(m.ctxGauge())) + "   " + footer()
	default:
		status = footer()
	}
	if debugFade {
		status += styleFooter.Render(m.fadeDebug())
	}

	// The input stays live even while a turn runs — you can keep typing, and
	// pressing enter queues the prompt for when the turn finishes.
	splash := m.splashActive()
	inputStyle := styleInput
	if m.ta.Focused() {
		inputStyle = styleInputFocus
	}
	inputContentW := m.width - 2
	if splash {
		// Fresh screen: narrow box centered under the wordmark (ta content + padding;
		// the border adds the remaining 2 cols).
		inputContentW = m.ta.Width() + 2
	}
	input := inputStyle.Width(inputContentW).Render(m.ta.View())

	// Transcript area (viewport + tiled subagent panes) = left column; the status
	// panel, if any, sits to its right at a fixed width.
	tw := m.width - m.panelCols()
	// The viewport may render fewer rows than its height when the transcript is
	// short; place it in a full-height box (blank rows become spaces, which
	// JoinVertical keeps) so the panes/input below sit at the bottom of the screen
	// instead of floating with empty space beneath the input.
	vpw := tw // the transcript content spans the full width (no drawn scrollbar)
	var vpContent string
	var splashCurRow, splashCurCol int
	if splash {
		// Fresh session: host the input prompt inside the viewport, centered directly
		// under the wordmark, and remember where its first text cell landed so the
		// real cursor can be placed there.
		vpContent, splashCurRow, splashCurCol = splashCompose(vpw, m.vp.Height(), m.splashIdentity(), input)
	} else if len(m.blocks) == 0 && !m.running && !m.resuming {
		// Fresh session but a modal is open: plain centered splash; the input stays
		// pinned at the bottom under the modal.
		vpContent = splashView(vpw, m.vp.Height(), m.splashIdentity())
	} else {
		vpContent = m.vp.View()
		if strings.TrimSpace(vpContent) == "" {
			vpContent = " " // empty/blank content isn't padded; give it a space
		}
	}
	// Force every row to exactly vpw cells with our terminal-aware measure
	// (blank rows become spaces so panes/input still sit at the bottom).
	vpv := composeBox(vpContent, vpw, m.vp.Height())
	leftRows := []string{vpv}
	aboveInput := 2 + m.vp.Height() // header(2: title+divider) + viewport rows above input
	if pv := m.renderPanes(tw, aboveInput); pv != "" {
		leftRows = append(leftRows, pv)
		aboveInput += lipgloss.Height(pv)
	}
	left := lipgloss.JoinVertical(lipgloss.Left, leftRows...)
	parts := []string{header, left}
	if m.resuming {
		pv := m.resumeView()
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	} else if m.routing {
		pv := m.routeView()
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	} else if m.searching {
		pv := m.searchView()
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	} else if matches := m.paletteMatches(); !m.running && len(matches) > 0 {
		pv := m.paletteView(matches)
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	} else if m.perm != nil {
		pv := m.permView()
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	} else if m.quest != nil {
		pv := m.questView()
		parts = append(parts, pv)
		aboveInput += lipgloss.Height(pv)
	}
	if !splash {
		parts = append(parts, input) // on splash the input lives inside the viewport
	}
	parts = append(parts, status)
	v.Content = lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Floating post-it panel: when there's something to show, composite it as a
	// content-height rounded box pinned to the top-right (with an M3 margin) over the
	// full-width transcript. The transcript is bottom-anchored, so its top-right is
	// usually blank — the box rarely overlaps text, and only the oldest top lines when
	// the screen is completely full (scroll to reveal them).
	if box, top, left, ok := m.floatPanel(); ok {
		v.Content = overlayBox(v.Content, top, left, box)
	}

	// Toast: overlay a transient notice in the top-left (on the header divider)
	// without reserving a layout row, so it floats and doesn't shift the UI.
	if m.snackbar != "" {
		v.Content = overlayLine(v.Content, 1, 2, styleToast.Render(m.snackbar))
	}

	// Report the real cursor at the input position so IME composition (Korean,
	// etc.) appears inline rather than at the screen corner — including while a
	// turn runs, so queued typing composes correctly.
	if m.perm == nil && m.ta.Focused() {
		if c := m.ta.Cursor(); c != nil {
			if splash {
				// The box lives inside the viewport (below the header's 2 rows);
				// splashCompose gives its first text cell and ta.Cursor is the offset
				// within the textarea content.
				c.Position.X = splashCurCol + c.Position.X
				c.Position.Y = 2 + splashCurRow + c.Position.Y
			} else {
				// Offset by the input box (border+padding = 2 cols) and the rows above
				// it (+1 for the box's top border).
				c.Position.X += 2
				c.Position.Y += aboveInput + 1
			}
			v.Cursor = c
		}
	}
	return v
}

// paletteView renders the slash-command completion popup.
func (m *Model) paletteView(matches []cmdInfo) string {
	sel := m.clampSel(len(matches))
	// Pad command names to a common width so descriptions align in a column.
	nameW := 0
	for _, c := range matches {
		if len(c.name) > nameW {
			nameW = len(c.name)
		}
	}
	var b strings.Builder
	for i, c := range matches {
		if i > 0 {
			b.WriteString("\n")
		}
		name := c.name + strings.Repeat(" ", nameW-len(c.name))
		if i == sel {
			b.WriteString(stylePalSelRow.Render("› " + name + "   " + c.desc))
		} else {
			// Every segment (incl. the literal gaps) carries the box's surface background
			// so the row is uniformly cream — otherwise the foreground-only styled spans
			// reset the bg and the terminal default (white in light themes) shows through
			// behind the text, making a cream/white checkerboard.
			onSurf := lipgloss.NewStyle().Background(colSurface)
			b.WriteString(onSurf.Render("  ") +
				stylePalName.Background(colSurface).Render(name) +
				onSurf.Render("   ") +
				styleToolResult.Background(colSurface).Render(c.desc))
		}
	}
	return stylePalBox.Width(m.width - 2).Render(b.String())
}

// renderCouncilDetail is the FULL-SCREEN detail for one clicked council verdict —
// the same destination style as a zoomed subagent pane. It shows the member's vote
// (decision/lens/confidence/rationale/feedback) AND the evidence the members were
// given that round (task/plan/report/diff), so the user sees both what was judged
// and how. Returned as viewport content (scrolls); closed with esc or a click.
func (m *Model) renderCouncilDetail(width int) string {
	v := m.councilDetail
	if v == nil {
		return ""
	}
	hue := m.councilColor(v.Member)
	wrap := lipgloss.NewStyle().Width(max(8, width-2))
	var b strings.Builder
	// (The "‹ back" breadcrumb is the fixed header — see View.)
	icon, word := councilVerdictLabel(v.Phase, v.Decision, v.Severity)
	b.WriteString(lipgloss.NewStyle().Foreground(hue).Bold(true).Render("⚖ "+v.Member) + "  " + councilVerdictStyle(v.Phase, v.Decision, v.Severity).Render(icon+" "+word))
	if v.Lens != "" {
		b.WriteString(styleFooter.Render("  [" + v.Lens + "]"))
	}
	if v.Confidence > 0 {
		b.WriteString(styleFooter.Render(fmt.Sprintf("  · confidence %.0f%%", v.Confidence*100)))
	}
	b.WriteString("\n")
	// 기승전결: what the member SAW (evidence) first, then the verdict's reasoning.
	if ev := strings.TrimSpace(m.councilDetailEvidence); ev != "" {
		b.WriteString("\n" + styleFooter.Render("— evidence the council saw —") + "\n\n" + wrap.Render(ev) + "\n")
	}
	if v.Rationale != "" {
		b.WriteString("\n" + styleFooter.Render("rationale") + "\n" + wrap.Render(v.Rationale) + "\n")
	}
	if v.Feedback != "" {
		b.WriteString("\n" + styleFooter.Render("next step") + "\n" + wrap.Render(v.Feedback) + "\n")
	}
	return b.String()
}

// formatCouncilEvidence renders the round's evidence (what every member saw) for
// the detail view.
func formatCouncilEvidence(d event.CouncilConvenedData) string {
	var b strings.Builder
	add := func(title, body string) {
		if strings.TrimSpace(body) == "" {
			return
		}
		b.WriteString("# " + title + "\n" + strings.TrimSpace(body) + "\n\n")
	}
	add("Task", d.Task)
	if d.Phase == "plan" {
		add("Proposed plan", d.Plan)
	} else {
		add("Plan / acceptance criteria", d.Plan)
		add("Agent report (the claim)", d.Report)
	}
	if len(d.Signals) > 0 {
		add("Signals", strings.Join(d.Signals, "\n"))
	}
	add("Changes", colorizeChanges(d.Changes))
	if d.NoChanges {
		b.WriteString("(no files changed — a read-only / answer turn)\n")
	}
	return strings.TrimSpace(b.String())
}

// colorizeChanges colors the council's change evidence for the detail view: a bold "◆ path"
// per-file header (from the "### path" markers), additions green, removals red. Per-line
// foreground only, so it flows through the detail view's word-wrap.
func colorizeChanges(changes string) string {
	if strings.TrimSpace(changes) == "" {
		return changes
	}
	add := lipgloss.NewStyle().Foreground(colSuccess)
	del := lipgloss.NewStyle().Foreground(colError)
	hdr := lipgloss.NewStyle().Foreground(colSuccess).Bold(true)
	var b strings.Builder
	for _, ln := range strings.Split(changes, "\n") {
		switch {
		case strings.HasPrefix(ln, "### "):
			b.WriteString("\n" + hdr.Render("◆ "+strings.TrimPrefix(ln, "### ")) + "\n")
		case strings.HasPrefix(ln, "+"):
			b.WriteString(add.Render(ln) + "\n")
		case strings.HasPrefix(ln, "-"):
			b.WriteString(del.Render(ln) + "\n")
		default:
			b.WriteString(ln + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

// questOptionAt maps a screen click to an option index, or ok=false if the click
// isn't on an option row. The modal stacks the title and question above the options
// inside the box, so option i sits 3 rows (box border + title + question) below the
// box top — the same top the perm modal uses.
func (m *Model) questOptionAt(y int) (int, bool) {
	if m.quest == nil {
		return 0, false
	}
	first := 2 + m.vp.Height() + m.panesBlockHeight() + 3
	i := y - first
	if i < 0 || i >= len(m.quest.options) {
		return 0, false
	}
	return i, true
}

// questView renders the ask_user selection modal: the question plus numbered
// options, the current pick highlighted.
func (m *Model) questView() string {
	q := m.quest
	var b strings.Builder
	b.WriteString(stylePermTitle.Render("question") + "  " +
		styleFooter.Render("↑/↓/tab or click · enter answer · esc dismiss") + "\n")
	b.WriteString(q.question + "\n")
	for i, opt := range q.options {
		line := fmt.Sprintf("%d. %s", i+1, opt)
		if i == q.sel {
			b.WriteString(stylePalSelRow.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + styleToolResult.Render(line) + "\n")
		}
	}
	return stylePermBox.Width(m.width - 4).Render(strings.TrimRight(b.String(), "\n"))
}

// permButton is one choice in the permission modal. key is the direct hotkey,
// word the button label, decision the value sent to respond().
type permButton struct{ key, word, decision string }

// permButtons is the ordered choice set for the permission modal — the single
// source of truth shared by the renderer (permView) and the click hit-tester
// (permButtonAt), so their geometry can't drift.
func permButtons() []permButton {
	return []permButton{
		{"y", "allow", "allow"},
		{"a", "always", "always"},
		{"p", "project", "persist"},
		{"n", "deny", "deny"},
	}
}

// permButtonWidth is a button's rendered cell width: label ("key word") plus the
// styleBtn horizontal padding (2 cells each side). Labels are ASCII, so byte len
// equals cell width.
func permButtonWidth(b permButton) int { return len(b.key) + 1 + len(b.word) + 4 }

// permModalHeight is the perm modal's rendered row count. It MUST match the reserve
// in baseChromeHeight (title + tool line + buttons + hint + box border, +1 for a
// reason line) so the layout and the click hit-test agree on where the box sits.
func (m *Model) permModalHeight() int {
	h := 6
	if m.perm.reason != "" {
		h++
	}
	return h
}

// permButtonAt maps a screen click to a button index, or ok=false if the click
// isn't on the button row. The modal is bottom-anchored chrome (not viewport
// content), so its top row is 2 (header) + the viewport + the reserved pane block,
// exactly as View() stacks them; the buttons are the box's last content row (one
// above the bottom border). Content starts at screen X=3 (border 1 + padding 2),
// independent of lipgloss Width semantics.
func (m *Model) permButtonAt(x, y int) (int, bool) {
	if m.perm == nil {
		return 0, false
	}
	top := 2 + m.vp.Height() + m.panesBlockHeight()
	// Buttons are the second-to-last content row: below them sit the hint line and
	// the box's bottom border, so the row is modalHeight-3 rows down from the top.
	rowY := top + m.permModalHeight() - 3
	if y != rowY {
		return 0, false
	}
	cx := 3 // box border (1) + left padding (2)
	for i, b := range permButtons() {
		w := permButtonWidth(b)
		if x >= cx && x < cx+w {
			return i, true
		}
		cx += w + 1 // + 1-cell gap between buttons
	}
	return 0, false
}

func (m *Model) permView() string {
	body := stylePermTitle.Render("permission required") + "\n" +
		fmt.Sprintf("run tool %s %s\n", styleToolName.Render(m.perm.name), styleToolArgs.Render(compactArgs(m.perm.args)))
	// Say WHY when the policy forced this prompt (bash scan verdicts) — the user
	// should decide on the policy's grounds, not just the raw command text.
	if m.perm.reason != "" {
		body += styleError.Render("⚠ "+m.perm.reason) + "\n"
	}
	// Focusable/clickable buttons: the selected one is a brighter filled pill.
	// Tab cycles, the hotkeys (y/a/p/n) still fire directly, and a click on a
	// pill activates it (permButtonAt shares this geometry).
	btns := make([]string, 0, 4)
	for i, b := range permButtons() {
		st := styleBtn
		if i == m.perm.sel {
			st = styleBtnSel
		}
		btns = append(btns, st.Render(b.key+" "+b.word))
	}
	body += strings.Join(btns, " ") + "\n" +
		styleFooter.Render("tab/click move · enter pick · p saves to .magi/config.toml")
	return stylePermBox.Width(m.width - 4).Render(body)
}

// updateSearch recomputes the hit list for the current query (case-insensitive
// substring over the plain transcript) and jumps to the first hit at or below
// the current scroll position, so typing narrows in place instead of yanking
// the view back to the top.
func (m *Model) updateSearch() {
	m.searchHits = m.searchHits[:0]
	q := strings.ToLower(m.searchQuery)
	if q == "" {
		m.refresh()
		return
	}
	for i, l := range m.contentPlain {
		if strings.Contains(strings.ToLower(l), q) {
			m.searchHits = append(m.searchHits, i)
		}
	}
	m.searchCur = 0
	for i, h := range m.searchHits {
		if h >= m.vp.YOffset() {
			m.searchCur = i
			break
		}
	}
	m.searchJump()
}

// searchStep moves to the next/previous hit, wrapping at either end.
func (m *Model) searchStep(d int) {
	if len(m.searchHits) == 0 {
		return
	}
	m.searchCur = (m.searchCur + d + len(m.searchHits)) % len(m.searchHits)
	m.searchJump()
}

// searchJump scrolls the current hit into view (roughly centered) and repaints.
func (m *Model) searchJump() {
	if len(m.searchHits) > 0 {
		off := m.searchHits[m.searchCur] - m.vp.Height()/2
		if off < 0 {
			off = 0
		}
		m.vp.SetYOffset(off)
	}
	m.refresh()
}

// highlightSearch overlays the query matches on the rendered content: every
// occurrence is tinted, the current hit's line uses the selection style. Cells
// are cut ANSI-aware (like highlightSelection) so markdown styling survives.
func (m *Model) highlightSearch(content string) string {
	q := strings.ToLower(m.searchQuery)
	if q == "" || len(m.searchHits) == 0 {
		return content
	}
	cur := -1
	if m.searchCur < len(m.searchHits) {
		cur = m.searchHits[m.searchCur]
	}
	lines := strings.Split(content, "\n")
	for _, ln := range m.searchHits {
		if ln < 0 || ln >= len(lines) || ln >= len(m.contentPlain) {
			continue
		}
		plain := m.contentPlain[ln]
		lower := strings.ToLower(plain)
		styled := lines[ln]
		w := ansi.StringWidth(styled)
		st := styleKeyLabel
		if ln == cur {
			st = styleSelection
		}
		// Rebuild the line left→right, wrapping each match in the highlight style.
		var b strings.Builder
		done := 0 // display cells consumed
		for from := 0; ; {
			idx := strings.Index(lower[from:], q)
			if idx < 0 {
				break
			}
			start := from + idx
			// Cut coordinates must be in ansi.StringWidth cells, since ansi.Cut and
			// w below measure that way. cellWidth adds a per-ambiguous-rune correction
			// that ansi.Cut does not understand, so using it here shifts the highlight
			// right by one column per ambiguous rune in the prefix on wide terminals.
			c0 := ansi.StringWidth(plain[:start])
			c1 := ansi.StringWidth(plain[:start+len(q)])
			if c0 < done { // overlapping match — skip
				from = start + len(q)
				continue
			}
			b.WriteString(ansi.Cut(styled, done, c0))
			b.WriteString(st.Render(ansi.Strip(ansi.Cut(styled, c0, c1))))
			done = c1
			from = start + len(q)
		}
		b.WriteString(ansi.Cut(styled, done, w))
		lines[ln] = b.String()
	}
	return strings.Join(lines, "\n")
}

// searchView renders the search bar shown in place of the palette while open.
func (m *Model) searchView() string {
	pos := "0/0"
	if n := len(m.searchHits); n > 0 {
		pos = fmt.Sprintf("%d/%d", m.searchCur+1, n)
	}
	return styleFooter.Render("  find: ") + m.searchQuery + styleFooter.Render("▏ "+pos+"  ") +
		footerKeys("enter/↓", "next") + footerKeys("↑", "prev") + footerKeys("esc", "close")
}

// scrollMeter renders the transcript scroll-position chip — the drawn
// scrollbar's replacement (see composeBox). Empty when everything fits.
// "⇅ 42% (120/300)" = the bottom visible line over the total; when the user
// has scrolled away from a still-streaming bottom, an "↓ new" marker warns
// that fresh output is arriving below (End jumps back).
func (m *Model) scrollMeter() string {
	total := len(m.contentPlain)
	h := m.vp.Height()
	if total <= h || h <= 0 {
		return ""
	}
	bottom := m.vp.YOffset() + h
	if bottom > total {
		bottom = total
	}
	chip := fmt.Sprintf("⇅ %d%% (%d/%d)", bottom*100/total, bottom, total)
	if !m.vp.AtBottom() && m.running {
		chip += " · ↓ new"
	}
	return styleKeyLabel.Render(chip)
}

// ctxGauge renders the persistent context-window usage gauge for the footer, e.g.
// "ctx 42% · 55.2k/131.0k". When the window is unknown (no catalog entry and the
// probe found nothing) it falls back to tokens only, "ctx ~55.2k". Empty until the
// first live usage event arrives (ctxTokens == 0), so the footer stays clean.
func (m *Model) ctxGauge() string {
	if m.ctxTokens <= 0 {
		return ""
	}
	if m.ctxWindow > 0 {
		return fmt.Sprintf("ctx %.0f%% · %s/%s", m.ctxPct, humanTokens(m.ctxTokens), humanTokens(m.ctxWindow))
	}
	return "ctx ~" + humanTokens(m.ctxTokens)
}

// gaugeSep prefixes the context gauge with a separator when it is non-empty, so an
// empty gauge (before the first usage event) adds no dangling " · " to the meter.
func gaugeSep(gauge string) string {
	if gauge == "" {
		return ""
	}
	return " · " + gauge
}

// councilWaitLabel is the fixed footer phrase shown (with the spinner) while a
// council round is open, naming which judgment is awaited so the wait doesn't read
// as a stall. Phase "plan" is the pre-execution plan audit; anything else is the
// finalize/consensus review of the answer.
func councilWaitLabel(phase string) string {
	if phase == "plan" {
		return "⚖ 플랜 감사 판정 대기 중…"
	}
	return "⚖ 카운슬 심의 판정 대기 중…"
}

// turnSummary renders the end-of-turn receipt line, e.g.
// "▣ turn: 14 steps · 3 files · council r2 · 3m49s". Parts with nothing to say
// are omitted; a pure conversational turn (no tools) renders nothing at all.
func (m *Model) turnSummary() string {
	if m.turnSteps == 0 {
		return ""
	}
	parts := []string{fmt.Sprintf("%d steps", m.turnSteps)}
	if n := len(m.turnFiles); n > 0 {
		parts = append(parts, fmt.Sprintf("%d file(s)", n))
	}
	if m.turnCouncil > 0 {
		parts = append(parts, fmt.Sprintf("council r%d", m.turnCouncil))
	}
	if m.turnDur > 0 {
		parts = append(parts, fmtDur(m.turnDur))
	}
	if m.turnUnverified {
		// The execution-evidence gate could not confirm the current version was run to a
		// passing result — surface it plainly instead of letting the turn read as a clean finish.
		parts = append(parts, "⚠ UNVERIFIED")
	}
	return "▣ turn: " + strings.Join(parts, " · ")
}

// lastAssistantText returns the most recent assistant block's text this turn
// (stopping at the last user block), or "" when there is none.
func (m *Model) lastAssistantText() string {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		switch m.blocks[i].kind {
		case blockAssistant:
			return m.blocks[i].text
		case blockUser:
			return ""
		}
	}
	return ""
}

// sameAnswer reports whether two answers are the same modulo whitespace.
func sameAnswer(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// collapseReviewedReport folds the most recent assistant report of THIS turn (back
// to the last user block) into a one-line stub. Called when a council REVIEW round
// sends the answer back for revision: for a "검수해줘"-style request the flow is
// report → council review → revised report, so the pre-review copy is superseded the
// moment the round rejects it — showing both full reports is just noise. Unlike the
// near-verbatim sameAnswer dedup, this collapses unconditionally (the revision may
// differ substantially), keeping only the final result. No-op when no assistant block
// follows the last user turn. Truncates the render cache at the folded block so it
// re-renders as the stub.
func (m *Model) collapseReviewedReport() {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		switch m.blocks[i].kind {
		case blockAssistant:
			m.blocks[i] = block{kind: blockInfo, text: "≡ (검수 전 보고서 — 접힘, 아래 최종본 참고)"}
			if len(m.cache) > i {
				m.cache = m.cache[:i]
			}
			return
		case blockUser:
			return
		}
	}
}

// turnMeter renders elapsed + token usage, e.g. "3m49s · ↑28.1k ↓10.4k". Token
// parts are omitted when unknown (a backend that reports no usage). (§8.1)
func turnMeter(d time.Duration, in, out int) string {
	s := fmtDur(d)
	if in > 0 {
		s += " · ↑" + humanTokens(in)
	}
	if out > 0 {
		s += " ↓" + humanTokens(out)
	}
	return s
}

// fmtDur formats a duration compactly: "47s", "3m49s", "1h02m".
func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// humanTokens abbreviates token counts: 847, 10.4k, 1.2M.
func humanTokens(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return strconv.Itoa(n)
	}
}

func footer() string {
	return styleFooter.Render("") +
		footerKeys("enter", "send") + footerKeys("esc", "interrupt") + footerKeys("ctrl+q", "quit")
}

func footerKeys(key, desc string) string {
	return "  " + styleKeyLabel.Render(key) + " " + styleFooter.Render(desc)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
