package tui

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// agentPane is a live view of one spawned subagent's child session (multi-agent
// view B). Each pane subscribes to its child session and renders that agent's
// transcript independently, so several subagents can be watched at once.
type agentPane struct {
	sid       session.SessionID
	role      string // agent name (explore, coder, …)
	task      string // short summary of the request dispatched to this subagent
	blocks    []block
	live      string // streaming text for the current step
	liveThink string
	done      bool
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
	tailLines  []string
	tailCacheW int
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

// openPane starts a live pane for a spawned subagent — one pane PER child
// session (so a re-dispatch or follow-up gets its own window). Each is labelled
// role + a short id (paneLabel) to tell concurrent same-role agents apart.
func (m *Model) openPane(ev event.Event) tea.Cmd {
	var d event.AgentStatusData
	if json.Unmarshal(ev.Data, &d) != nil || d.AgentID == "" {
		return nil
	}
	cid := session.SessionID(d.AgentID)
	if m.paneBySID(cid) != nil {
		return nil // already open for this exact session
	}
	ch, cancel, err := m.app.Subscribe(m.ctx, cid, 0)
	if err != nil {
		return nil
	}
	m.subID++
	p := &agentPane{sid: cid, role: orDefaultStr(d.Role, d.AgentID), ch: ch, cancel: cancel, sub: m.subID}
	m.panes = append(m.panes, p)
	fadeDbg("openPane sid=%s role=%s sub=%d (panes=%d)", shortSID(cid), p.role, p.sub, len(m.panes))
	return waitEvent(ch, cid, p.sub)
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

// taskAgents extracts the target agent name(s) from a task tool call's args,
// supporting both {agent} and {tasks:[{agent},…]}.
func taskAgents(args string) []string {
	var d struct {
		Agent string `json:"agent"`
		Tasks []struct {
			Agent string `json:"agent"`
		} `json:"tasks"`
	}
	if json.Unmarshal([]byte(args), &d) != nil {
		return nil
	}
	var out []string
	if d.Agent != "" {
		out = append(out, d.Agent)
	}
	for _, t := range d.Tasks {
		if t.Agent != "" {
			out = append(out, t.Agent)
		}
	}
	return out
}

// restoreChildPanes rebuilds finished subagent panes for a resumed session, so
// the parent's subagents come back as inspectable (done) panes — their live
// spawn events were transient and aren't replayed.
func (m *Model) restoreChildPanes(parent session.SessionID) {
	m.closePanes()
	children, err := m.app.ChildSessions(m.ctx, m.workdir, parent)
	if err != nil || len(children) == 0 {
		return
	}
	for _, c := range children {
		task := ""
		for _, msg := range c.Messages {
			if msg.Role == session.RoleUser {
				task = strings.TrimSpace(joinTextParts(msg.Parts))
				break
			}
		}
		m.panes = append(m.panes, &agentPane{
			sid:    c.ID,
			role:   orDefaultStr(c.Role, string(c.ID)),
			task:   task,
			blocks: rebuildBlocks(c.Messages),
			done:   true,
		})
	}
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

// paneBySID finds a pane by child session id.
func (m *Model) paneBySID(sid session.SessionID) *agentPane {
	for _, p := range m.panes {
		if p.sid == sid {
			return p
		}
	}
	return nil
}

// paneBySub finds a pane by subscription generation.
func (m *Model) paneBySub(sub int) *agentPane {
	for _, p := range m.panes {
		if p.sub == sub {
			return p
		}
	}
	return nil
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

// applyPaneEvent folds a child-session event into its pane's transcript.
func (m *Model) applyPaneEvent(p *agentPane, e event.Event) {
	p.evCount++
	p.lastEv = string(e.Type)
	if p.started.IsZero() {
		p.started = time.Now() // first event marks the subagent's start (§8.1)
	}
	switch e.Type {
	case event.TypeContextUsage:
		var d event.ContextUsageData
		if json.Unmarshal(e.Data, &d) == nil {
			p.in, p.out = d.Tokens, d.OutTokens
		}
	case event.TypePromptSubmitted:
		// The subagent's first prompt IS its task — capture a summary for the label.
		if p.task == "" {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				p.task = strings.TrimSpace(joinTextParts(d.Parts))
			}
		}
	case event.TypePartDelta:
		var d event.PartDeltaData
		if json.Unmarshal(e.Data, &d) == nil {
			switch d.Kind {
			case session.PartText:
				p.live += d.Text
			case session.PartReasoning:
				p.liveThink += d.Text
			}
		}
	case event.TypePartAppended:
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) != nil {
			return
		}
		switch d.Part.Kind {
		case session.PartReasoning:
			p.liveThink = ""
			p.blocks = append(p.blocks, block{kind: blockReasoning, text: d.Part.Text})
		case session.PartText:
			p.live = ""
			p.blocks = append(p.blocks, block{kind: blockAssistant, text: d.Part.Text})
		case session.PartToolCall:
			if d.Part.ToolCall != nil {
				p.live = ""
				p.blocks = append(p.blocks, block{
					kind: blockToolCall,
					name: d.Part.ToolCall.Name,
					args: string(d.Part.ToolCall.Args),
				})
			}
		case session.PartToolResult:
			if d.Part.ToolResult != nil {
				p.blocks = foldToolResultInto(p.blocks, toolResultText(d.Part.ToolResult), !d.Part.ToolResult.IsError)
			}
		}
	case event.TypeTurnFinished, event.TypeError:
		if !p.done {
			p.done = true
			p.doneAt = time.Now() // start THIS pane's own fade clock
		}
		fadeDbg("pane %s DONE via child %s", shortSID(p.sid), e.Type)
		p.live = ""
		p.liveThink = ""
		if !p.started.IsZero() {
			p.dur = time.Since(p.started)
		}
		if e.Type == event.TypeTurnFinished {
			var fd event.TurnFinishedData
			if json.Unmarshal(e.Data, &fd) == nil {
				if fd.Usage.In > 0 {
					p.in = fd.Usage.In
				}
				if fd.Usage.Out > 0 {
					p.out = fd.Usage.Out
				}
			}
		}
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

// focusedPaneSID returns the focused pane's session id, or "" for the main view.
func (m *Model) focusedPaneSID() (session.SessionID, bool) {
	if m.focusPane < 0 || m.focusPane >= len(m.panes) {
		return "", false
	}
	return m.panes[m.focusPane].sid, true
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

// paneTail renders the last rows of a pane's transcript (cheap, no markdown),
// for the tiled overview where space is tight.
func (m *Model) paneTail(p *agentPane, width, rows int) string {
	// A finished pane's blocks are final — render them once per width and reuse,
	// so they don't recompute (or flicker) every frame.
	var blockLines []string
	if p.done && p.tailCacheW == width && p.tailLines != nil {
		blockLines = p.tailLines
	} else {
		for _, blk := range p.blocks {
			blockLines = append(blockLines, paneBlockLines(blk, width)...)
		}
		if p.done {
			p.tailLines = blockLines
			p.tailCacheW = width
		}
	}
	lines := blockLines
	// Live regions exist only while running; append a COPY so the cached slice
	// is never mutated.
	if !p.done {
		var extra []string
		if s := strings.TrimSpace(p.liveThink); s != "" && p.live == "" {
			extra = append(extra, styleThink.Render("…thinking"))
		}
		if s := strings.TrimSpace(p.live); s != "" {
			extra = append(extra, wrapLines(s, width)...)
		}
		if len(extra) > 0 {
			lines = append(append([]string{}, blockLines...), extra...)
		}
	}
	if len(lines) > rows {
		lines = lines[len(lines)-rows:]
	}
	return strings.Join(lines, "\n")
}

// paneBlockLines renders a single block to plain wrapped lines for a pane.
func paneBlockLines(blk block, width int) []string {
	switch blk.kind {
	case blockToolCall:
		glyph := styleToolName.Render("⚙")
		if blk.done {
			if blk.ok {
				glyph = styleToolOK.Render("✓")
			} else {
				glyph = styleToolErr.Render("✗")
			}
		}
		head := glyph + " " + styleToolName.Render(blk.name)
		if a := compactArgs(blk.args); a != "" {
			head += " " + styleToolArgs.Render(oneLine(a, max(4, width-len(blk.name)-4)))
		}
		if blk.done {
			if s := summarizeResult(blk.result); s != "" {
				head += styleToolResult.Render(" ⟶ " + oneLine(s, max(4, width-len(blk.name)-6)))
			}
		}
		return []string{head}
	case blockToolResult:
		mark := styleToolOK.Render("✓")
		if !blk.ok {
			mark = styleToolErr.Render("✗")
		}
		return []string{mark + " " + styleToolResult.Render(oneLine(summarizeResult(blk.text), max(4, width-2)))}
	case blockReasoning:
		return []string{styleThink.Render(oneLine(blk.text, width))}
	case blockError:
		return []string{styleError.Render(oneLine(blk.text, width))}
	default:
		return wrapLines(blk.text, width)
	}
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
			return "  " + styleKeyLabel.Render(fmt.Sprintf("%d agent(s) — screen too short · ctrl+o to open", more))
		}
		return "  " + styleKeyLabel.Render(fmt.Sprintf("↑%d  ↓%d  (scroll · ctrl+o to open)", off, more-off))
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
			var line string
			if p.fade > 0 {
				// Fading out: re-render the whole row in one color blended toward the
				// surface, so this finished pane dims away before it's removed.
				dc := blendColor(c, colSurface, p.fade)
				line = lipgloss.NewStyle().Foreground(dc).Render("● " + p.desc(width-8) + " " + m.paneStatusPlain(p))
			} else {
				line = lipgloss.NewStyle().Foreground(c).Render("● ") +
					lipgloss.NewStyle().Foreground(c).Bold(true).Render(p.desc(width-8)) + " " + m.paneStatus(p)
				if off+i == m.focusPane {
					line += " " + styleKeyLabel.Render("[focus: ctrl+o to open]")
				}
			}
			p.x, p.y, p.w, p.h = 0, y, width, 1
			rows = append(rows, "  "+line)
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
	// Record the content line where each block begins so a click in the zoom view
	// can be mapped back to a pane block (e.g. to expand a thinking block).
	m.paneLineStart = m.paneLineStart[:0]
	nl := 0
	for i, blk := range p.blocks {
		if i > 0 {
			b.WriteString("\n")
			nl++
		}
		m.paneLineStart = append(m.paneLineStart, nl)
		// In a subagent's detail view, assistant lines are THAT agent, not magi.
		s := m.renderBlockAs(blk, p.label(), c)
		b.WriteString(s)
		nl += strings.Count(s, "\n")
	}
	if s := strings.TrimSpace(p.liveThink); s != "" && p.live == "" {
		b.WriteString("\n" + label(styleBar, "thinking") + "\n" + indent(styleThink.Render(s)))
	}
	if s := strings.TrimSpace(p.live); s != "" {
		b.WriteString("\n" + label(cstyle.Bold(true), p.label()) + "\n" + indent(lipgloss.NewStyle().Width(max(20, width-4)).Render(s)))
	}
	return b.String()
}
