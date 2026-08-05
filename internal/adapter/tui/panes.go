package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// agentPane is a live view of one spawned subagent's child session (multi-agent
// view B). Each pane subscribes to its child session and renders that agent's
// transcript independently, so several subagents can be watched at once.
type agentPane struct {
	sid session.SessionID
	// job is the background command this pane follows (bg_1, …). Set for every pane the strip
	// opens today; the session fields stay for a pane restored from a stored transcript.
	job       string
	exit      int    // the job's exit status, once it has one
	exited    bool   // it ended on its own (as opposed to still running, or killed)
	killed    bool   // it was stopped, so its exit says nothing about the work
	role      string // display name
	task      string // the command this pane runs
	live      string // streaming text for the current step
	liveThink string
	// blocks is a CHILD pane's own transcript, rebuilt from its session the way a resumed session
	// is: the prompt it was given, what it reasoned, every tool it called with its arguments and
	// result, and what it finally said. A background-command pane has none — its content is a log
	// tail, not a conversation.
	//
	// This is the difference between a pane you can read and a pane that says "step 3 · read". The
	// progress line the parent's spinner shows was never meant to be a panel's contents.
	blocks []block
	done   bool
	// Per-pane fade-out: doneAt is when this subagent finished; fade is its current
	// dim level (0=opaque..1=gone). Each pane fades and is removed INDEPENDENTLY a few
	// seconds after IT finishes — it doesn't wait for sibling panes or the turn.
	doneAt time.Time
	fade   float64

	// per-subagent meter (§8.1): elapsed + tokens, shown as the pane's total
	started time.Time
	dur     time.Duration
	in, out int

	// absolute screen Y of this subagent's row in the right status panel, recorded
	// during render for click hit-testing (panel click → zoom). 0 = not shown.
	panelY int

	// subscription
	ch     <-chan event.Event
	cancel func()
	sub    int

	// debug (MAGI_DEBUG_FADE): how many events this pane received and the last type,
	// to diagnose panes whose completion signal never arrives.
	evCount int
	lastEv  string

	// screen rectangle of the last render, for mouse hit-testing
	x, y, w, h int

	// tail render cache: a finished pane's blocks never change, so render their
	// overview lines once (per width) instead of every frame.
}

// roleColorIndex returns a stable palette index for a role within this session.
func (m *Model) roleColorIndex(role string) int {
	if m.roleColor == nil {
		m.roleColor = map[string]int{}
	}
	if i, ok := m.roleColor[role]; ok {
		return i
	}
	i := len(m.roleColor) % len(agentPalette)
	m.roleColor[role] = i
	return i
}

// paneColor returns the base (role-level) color for a subagent role — used by
// the transcript "task → <name>" highlight.
func (m *Model) paneColor(role string) color.Color {
	return agentPalette[m.roleColorIndex(role)]
}

// councilColor returns a council member's hue: the MAGI's named theme colors for
// Melchior/Balthasar/Casper (theme-overridable), else a stable agentPalette
// fallback for custom or extra members.
func (m *Model) councilColor(member string) color.Color {
	switch strings.ToLower(strings.TrimSpace(member)) {
	case "melchior":
		return colMelchior
	case "balthasar":
		return colBalthasar
	case "casper":
		return colCasper
	default:
		return m.paneColor(member)
	}
}

// paneColorOf returns a pane's color: the role's base hue, with brightness
// shifted for the 2nd/3rd/… concurrent pane of the same role. Combined with the
// task summary (see desc), same-role panes are easy to tell apart.
func (m *Model) paneColorOf(p *agentPane) color.Color {
	return shiftLightness(agentPalette[m.roleColorIndex(p.role)], m.paneInstanceIndex(p))
}

// paneInstanceIndex is how many earlier panes share this pane's role (0 = first).
func (m *Model) paneInstanceIndex(p *agentPane) int {
	n := 0
	for _, q := range m.panes {
		if q == p {
			break
		}
		if q.role == p.role {
			n++
		}
	}
	return n
}

// shiftLightness keeps a color's hue but alternately lightens/darkens it per
// step (0 = unchanged) so same-role panes are told apart by brightness.
func shiftLightness(c color.Color, step int) color.Color {
	if step <= 0 {
		return c
	}
	r, g, b, _ := c.RGBA()
	rf, gf, bf := float64(r>>8), float64(g>>8), float64(b>>8)
	f := 0.18 * float64((step+1)/2)
	if f > 0.6 {
		f = 0.6
	}
	if step%2 == 1 { // lighten toward white
		rf, gf, bf = rf+(255-rf)*f, gf+(255-gf)*f, bf+(255-bf)*f
	} else { // darken toward black
		rf, gf, bf = rf*(1-f), gf*(1-f), bf*(1-f)
	}
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", int(rf), int(gf), int(bf)))
}

// blendColor returns the color t of the way (0..1) from a toward b. Used to fade a
// finished pane's row toward the surface as it dims out before being removed.
func blendColor(a, b color.Color, t float64) color.Color {
	if t <= 0 {
		return a
	}
	if t > 1 {
		t = 1
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	lerp := func(x, y uint32) int { return int(float64(x>>8) + (float64(y>>8)-float64(x>>8))*t) }
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X", lerp(ar, br), lerp(ag, bg), lerp(ab, bb)))
}

// paneStatusPlain is paneStatus without styling, for the fade-out path where the
// whole row is re-rendered in one blended (dimming) color.
func (m *Model) paneStatusPlain(p *agentPane) string {
	g := "•"
	if p.done {
		g = "✓"
		if p.job != "" && (p.killed || p.exit != 0) {
			g = "✗"
		}
	}
	if p.job != "" {
		return g + " " + jobStatus(p)
	}
	elapsed := p.dur
	if elapsed == 0 && !p.started.IsZero() {
		elapsed = time.Since(p.started)
	}
	if meter := turnMeter(elapsed, p.in, p.out); meter != "" {
		return g + " " + meter
	}
	return g
}

// label is the assistant attribution used inside a subagent's own transcript.
func (p *agentPane) label() string { return p.role }

// desc is the pane's display label: role + a SHORT gist of its task (first line,
// hard-capped) so same-role panes are distinguishable without dumping the full
// request.
func (p *agentPane) desc(width int) string {
	t := strings.TrimSpace(p.task)
	if t == "" {
		return p.role
	}
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[:i] // first line only
	}
	limit := 28
	if w := width - len(p.role) - 3; w < limit {
		limit = w
	}
	return p.role + " · " + oneLine(t, max(8, limit))
}

// anyPaneRunning reports whether any subagent pane is still working.
func (m *Model) anyPaneRunning() bool {
	for _, p := range m.panes {
		if !p.done {
			return true
		}
	}
	return false
}

// closePanes cancels all pane subscriptions and clears the multi-agent view.
func (m *Model) closePanes() {
	for _, p := range m.panes {
		if p.cancel != nil {
			p.cancel()
		}
	}
	m.panes = nil
	m.doneRoster = nil
	m.focusPane = -1
	m.zoom = false
	m.zoomPane = nil
	m.paneScroll = 0
}

// ensureFocusVisible scrolls the pane window so the focused pane is on screen.
// Called after keyboard focus moves so the selection never slides out of view.
func (m *Model) ensureFocusVisible() {
	nShown, _, _, _ := m.paneLayout()
	if nShown <= 0 || m.focusPane < 0 {
		return
	}
	switch {
	case m.focusPane < m.paneScroll:
		m.paneScroll = m.focusPane
	case m.focusPane >= m.paneScroll+nShown:
		m.paneScroll = m.focusPane - nShown + 1
	}
}

// cyclePaneFocus moves the focus ring across panes (-1 = main transcript).
func (m *Model) cyclePaneFocus(dir int) {
	if len(m.panes) == 0 {
		m.focusPane = -1
		return
	}
	// Range is [-1, len-1]; -1 is the main transcript.
	n := len(m.panes) + 1
	idx := m.focusPane + 1 + dir // shift so -1 maps to 0
	idx = ((idx % n) + n) % n
	m.focusPane = idx - 1
}

// handlePaneClick focuses the subagent pane whose recorded screen rectangle
// contains the click row y. A click on the already-focused pane toggles zoom.
// Returns true when the click was consumed.
func (m *Model) handlePaneClick(y int) bool {
	if m.zoom || len(m.panes) == 0 {
		return false
	}
	for i, p := range m.panes {
		if p.h > 0 && y >= p.y && y < p.y+p.h {
			if m.focusPane == i {
				m.zoom = true // second click zooms in
				m.refresh()
				m.vp.GotoBottom() // show the latest output (e.g. conclusion)
			} else {
				m.focusPane = i
				m.refresh()
			}
			return true
		}
	}
	return false
}

// paneTail renders the last rows of a pane's content, for the tiled overview where space is tight.
//
// A CHILD pane shows the tail of its own transcript — the same blocks, through the same renderer as
// the main one, so a tool call reads the same in a pane as it does above it. A BACKGROUND-COMMAND
// pane has no transcript and shows its live log tail instead.
func (m *Model) paneTail(p *agentPane, width, rows int) string {
	if len(p.blocks) > 0 {
		var lines []string
		for _, blk := range p.blocks {
			lines = append(lines, strings.Split(m.renderBlockAs(blk, p.label(), m.paneColorOf(p)), "\n")...)
		}
		if len(lines) > rows {
			lines = lines[len(lines)-rows:] // the newest is what a glance wants
		}
		return strings.Join(lines, "\n")
	}
	// Live regions exist only while running.
	var lines []string
	if !p.done {
		if s := strings.TrimSpace(p.liveThink); s != "" && p.live == "" {
			lines = append(lines, styleThink.Render("…thinking"))
		}
		if s := strings.TrimSpace(p.live); s != "" {
			lines = append(lines, wrapLines(s, width)...)
		}
	}
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	return strings.Join(lines, "\n")
}

func wrapLines(s string, width int) []string {
	wrapped := lipgloss.NewStyle().Width(max(4, width)).Render(strings.TrimRight(s, "\n"))
	return strings.Split(wrapped, "\n")
}

// paneStatus is the trailing status glyph + the subagent's meter (§8.1).
// paneStatus renders the spinner/✓ + the time/token meter.
func (m *Model) paneStatus(p *agentPane) string {
	glyph := styleToolName.Render(m.sp.View())
	if p.done {
		glyph = styleToolOK.Render("✓")
		// A job that ended badly must not wear the same ✓ as one that ended clean: the strip is
		// where a failing build should be visible without opening it.
		if p.job != "" && (p.killed || p.exit != 0) {
			glyph = styleToolErr.Render("✗")
		}
	}
	if p.job != "" {
		return glyph + " " + styleFooter.Render(jobStatus(p))
	}
	elapsed := p.dur
	if elapsed == 0 && !p.started.IsZero() {
		elapsed = time.Since(p.started)
	}
	meter := turnMeter(elapsed, p.in, p.out)
	if meter == "" {
		return glyph
	}
	return glyph + " " + styleFooter.Render(meter)
}

// paneTitle renders a pane's colored title bar: ● role  <id>  status.
func (m *Model) paneTitle(p *agentPane, width int, focused bool) string {
	c := m.paneColorOf(p)
	dot := lipgloss.NewStyle().Foreground(c).Render("●")
	name := lipgloss.NewStyle().Foreground(c).Bold(true).Render(p.desc(width - 4))
	title := dot + " " + name + " " + m.paneStatus(p)
	if focused {
		title += " " + styleKeyLabel.Render("[focused]")
	}
	// Truncate the COMPOSED title to the inner width: only `name` was width-bounded
	// above, but the appended status/[focused] can still push the line past the box,
	// where it would WRAP to a second row and overflow the pane's fixed height —
	// clipping the bottom border. MaxWidth is ANSI-aware and truncates (no wrap).
	return lipgloss.NewStyle().MaxWidth(width).Render(title)
}

// renderPanes renders the tiled subagent overview into a block of the given
// width and height. Panes stack vertically, sharing the height evenly; the
// focused pane gets an M3 focus ring (its agent color). It records each pane's
// screen rectangle (relative to the panes block) for hit-testing.
func (m *Model) renderPanes(width, originY int) string {
	nShown, perPane, more, _ := m.paneLayout()
	// Reset every pane's hit-test rect; only the shown panes get a fresh rect below.
	// Otherwise a pane pushed behind "+N more" keeps a stale rect and a click could
	// route to it (handlePaneClick scans all panes by p.h>0).
	for _, p := range m.panes {
		p.x, p.y, p.w, p.h = 0, 0, 0, 0
	}
	if nShown == 0 && more == 0 {
		return ""
	}
	// Scroll window: clamp the offset and show panes[off:off+nShown]. paneLayout stays
	// offset-free (so the reserve == render invariant holds); only the slice moves.
	off := clampInt(m.paneScroll, 0, max(0, len(m.panes)-nShown))
	m.paneScroll = off
	// The single reserved overflow row shows how many are hidden above/below; when the
	// screen is too short to show any box at all, it just reports the count.
	moreLine := func() string {
		if nShown == 0 {
			return clipRow("  "+styleKeyLabel.Render(fmt.Sprintf("%d agent(s) — screen too short · ctrl+o to open", more)), width)
		}
		return clipRow("  "+styleKeyLabel.Render(fmt.Sprintf("↑%d  ↓%d  (scroll · ctrl+o to open)", off, more-off)), width)
	}
	// Turn finished → compact one-line-per-pane strip (still focusable/zoomable) so
	// finished subagents don't keep eating the screen; each fades out (per pane) a few
	// seconds after IT finished and is then removed. Capped to nShown.
	if !m.running {
		var rows []string
		y := originY
		for i := 0; i < nShown; i++ {
			p := m.panes[off+i]
			c := m.paneColorOf(p)
			// The row is indent(2) + "● " + desc + " " + status, and on the focused pane a
			// hint after that. Only desc used to be bounded, and against width-8 — while the
			// status ("✗ exit 1", "⣾ running 1m30s") and the hint together run past forty
			// cells, so the row overran an 80-column terminal by two and a 40-column one by
			// half its width. Everything after desc is measured first and desc gets what is
			// actually left; the hint is dropped before the status, which carries the fact.
			status := m.paneStatus(p)
			if p.fade > 0 {
				status = m.paneStatusPlain(p)
			}
			hint := ""
			if p.fade == 0 && off+i == m.focusPane {
				hint = " " + styleKeyLabel.Render("[focus: ctrl+o to open]")
			}
			budget := width - 4 - ansi.StringWidth(status) - 1 - ansi.StringWidth(hint)
			if budget < 8 {
				// Nothing left for a hint: the status says what the job did, the hint only
				// says how to open it.
				hint = ""
				budget = width - 4 - ansi.StringWidth(status) - 1
			}
			desc := p.desc(budget)
			var line string
			if p.fade > 0 {
				// Fading out: re-render the whole row in one color blended toward the
				// surface, so this finished pane dims away before it's removed.
				dc := blendColor(c, colSurface, p.fade)
				line = lipgloss.NewStyle().Foreground(dc).Render("● " + desc + " " + status)
			} else {
				line = lipgloss.NewStyle().Foreground(c).Render("● ") +
					lipgloss.NewStyle().Foreground(c).Bold(true).Render(desc) + " " + status + hint
			}
			p.x, p.y, p.w, p.h = 0, y, width, 1
			rows = append(rows, clipRow("  "+line, width))
			y++
		}
		if more > 0 {
			rows = append(rows, moreLine())
		}
		return strings.Join(rows, "\n")
	}
	// Each pane: 1 title + content, plus a 2-row border. perPane comes from paneLayout.
	var rendered []string
	y := originY
	for i := 0; i < nShown; i++ {
		p := m.panes[off+i]
		focused := off+i == m.focusPane
		c := m.paneColorOf(p)
		border := colOutlVar
		if focused {
			border = c
		}
		if p.fade > 0 { // this finished pane is fading out — dim its border toward the surface
			border = blendColor(border, colSurface, p.fade)
		}
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Width(max(8, width-2)).
			MaxHeight(perPane) // hard cap: a wrapping title can't push the box past its reserve
		inner := max(4, width-4)
		contentRows := max(1, perPane-3) // body lines: total perPane minus border(2) minus title(1)
		body := m.paneTitle(p, inner, focused) + "\n" + m.paneTail(p, inner, contentRows)
		// lipgloss Height/MaxHeight are TOTAL height (border included), so set perPane
		// directly to match paneLayout's reserve (avoids a render-vs-reserve drift).
		r := box.Height(perPane).Render(body)
		// Record screen rect for click hit-testing.
		p.x, p.y, p.w, p.h = 0, y, width, lipgloss.Height(r)
		y += p.h
		rendered = append(rendered, r)
	}
	if more > 0 {
		rendered = append(rendered, moreLine())
	}
	return strings.Join(rendered, "\n")
}

// renderZoom renders the focused pane full-screen for detailed inspection, using
// the shared viewport for scrolling. Returns the content string.
func (m *Model) renderZoom(width int) string {
	p := m.viewedPane()
	if p == nil {
		return ""
	}
	c := m.paneColorOf(p)
	cstyle := lipgloss.NewStyle().Foreground(c)
	// The breadcrumb is rendered as the fixed header (see View); here just the body.
	var b strings.Builder
	// The child's whole transcript, from the prompt it was handed down. This is the view that
	// answers "what was it actually told, what did it think, what did it run" — the questions the
	// one-line progress heartbeat could not.
	for _, blk := range p.blocks {
		b.WriteString("\n" + m.renderBlockAs(blk, p.label(), c))
	}
	if s := strings.TrimSpace(p.liveThink); s != "" && p.live == "" {
		b.WriteString("\n" + label(styleBar, "thinking") + "\n" + indent(styleThink.Render(s)))
	}
	if s := strings.TrimSpace(p.live); s != "" {
		b.WriteString("\n" + label(cstyle.Bold(true), p.label()) + "\n" + indent(lipgloss.NewStyle().Width(max(20, width-4)).Render(s)))
	}
	return b.String()
}
