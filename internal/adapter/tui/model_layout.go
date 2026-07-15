package tui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

func (m *Model) resize(w, h int) {
	m.width, m.height = w, h
	m.ta.SetWidth(w - 6)
	m.buildGlam(m.width - m.panelCols()) // wrap to the transcript width (panel-aware)

	if !m.ready {
		m.vp = viewport.New()
		m.ready = true
	}
	// Width/height/content (and follow-to-bottom) are handled by refresh(), which
	// the caller invokes right after resize.
}

// buildGlam (re)creates the markdown renderer to wrap at transcript width tw,
// matching the theme. No-op if tw is unchanged since the last build.
func (m *Model) buildGlam(tw int) {
	if tw == m.glamWidth && m.glam != nil {
		return
	}
	mdStyle := "light"
	if m.isDark {
		mdStyle = "dark"
	}
	if glam, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(mdStyle),
		glamour.WithWordWrap(max(20, tw-6)),
	); err == nil {
		m.glam = glam
		m.glamWidth = tw
	}
}

// maxInputRows caps how tall the input box grows for multi-line input.
const maxInputRows = 6

// chromeHeight is the number of rows used by header + input + footer (+ modal /
// command palette).
// minViewport is the smallest transcript viewport we keep visible; the pane block
// is capped so chrome + viewport never exceeds the screen (input stays on screen).
const minViewport = 3

// panesMaxNumer/panesMaxDenom bound the subagent pane block to a fraction of the
// screen (≈ 1/2) so a burst of agents can't swallow the transcript. Panes past the
// cap scroll (renderPanes window) instead of growing the block. This is a stricter
// limit than minViewport alone, which would let panes shrink the transcript to 3 rows.
const panesMaxNumer, panesMaxDenom = 1, 3

// After a turn ends, the finished inline pane strip stays fully visible for
// paneFadeAfter, then dims to gone over paneFadeDur and is removed from the
// transcript column (the status panel keeps the roster for later tracking). The
// fade pauses while a pane is focused or zoomed so it never vanishes mid-read.
const (
	paneFadeAfter = 4 * time.Second
	paneFadeDur   = 1500 * time.Millisecond
)

// advancePaneFade recomputes each finished pane's INDEPENDENT fade for this frame and
// returns whether anything changed (so the caller can request a repaint). Every pane
// fades on its OWN clock (doneAt) — a finished subagent fades and is removed without
// waiting for its siblings or the turn. The focused/zoomed pane's fade pauses so it
// never vanishes mid-read. Removed panes disappear from the inline strip AND the
// right-panel roster (the plan/todos panel persists — it's app state, not m.panes).
func (m *Model) advancePaneFade() bool {
	if len(m.panes) == 0 {
		return false
	}
	var focused *agentPane
	if m.focusPane >= 0 && m.focusPane < len(m.panes) {
		focused = m.panes[m.focusPane]
	}
	changed := false
	kept := m.panes[:0:0]
	for _, p := range m.panes {
		lvl := 0.0
		if p.done && !p.doneAt.IsZero() && p != focused { // focused pane's fade is paused
			switch elapsed := time.Since(p.doneAt); {
			case elapsed < paneFadeAfter:
				lvl = 0
			case elapsed < paneFadeAfter+paneFadeDur:
				lvl = float64(elapsed-paneFadeAfter) / float64(paneFadeDur)
			default:
				lvl = 1
			}
		}
		if lvl >= 1 { // fade complete → drop from the inline strip, keep in the panel roster
			if p.cancel != nil {
				p.cancel()
			}
			p.fade = 0 // shown undimmed in the panel record
			m.doneRoster = append(m.doneRoster, p)
			fadeDbg("pane %s faded out of inline — moved to panel roster", shortSID(p.sid))
			changed = true
			continue
		}
		if lvl != p.fade {
			p.fade = lvl
			changed = true
		}
		kept = append(kept, p)
	}
	if len(kept) != len(m.panes) {
		// Removal shifted indices — re-point focus to the SAME pane by identity. Only
		// drop zoom if the zoomed/focused pane itself was removed (a focused pane's fade
		// is paused, so it survives — don't eject the user when a sibling fades out).
		m.panes = kept
		newFocus := -1
		if focused != nil {
			for i, p := range kept {
				if p == focused {
					newFocus = i
					break
				}
			}
		}
		m.focusPane = newFocus
		// Only leave zoom if the LIVE focused pane vanished. A pinned finished pane
		// (zoomPane, focusPane already -1) must stay open while siblings fade out.
		if newFocus < 0 && m.zoomPane == nil {
			m.zoom = false
		}
	}
	return changed
}

// baseChromeHeight is the fixed chrome (everything except the agent-pane block):
// header, bordered input, footer, and any active modal. It must NOT call
// panesBlockHeight (panesBlockHeight derives its cap from this — no recursion).
// splashActive reports whether the fresh-session splash is showing with the input
// prompt hosted inside the viewport (no transcript, nothing running, no modal). In
// that state the input is centered under the wordmark rather than pinned at the
// bottom, so chrome sizing, input width, and cursor placement all key off this.
func (m *Model) splashActive() bool {
	return m.ready && len(m.blocks) == 0 && !m.running && !m.resuming &&
		!m.routing && !m.searching && !m.zoom && m.councilDetail == nil &&
		m.perm == nil && m.quest == nil && len(m.paletteMatches()) == 0
}

func (m *Model) baseChromeHeight() int {
	if m.splashActive() {
		return 3 // header(2) + footer(1); the input lives inside the splash viewport
	}
	inputRows := m.ta.Height()
	if inputRows < 1 {
		inputRows = 1
	}
	h := 2 + (inputRows + 2) + 1 // header(2) + bordered input(rows+border) + footer
	if m.perm != nil {
		// Reserve the modal's *rendered* height, not a nominal row count: a long
		// tool line, policy reason, or the hint can wrap at narrow widths, and an
		// under-reserve pushes the input/footer off the bottom of the screen.
		h += lipgloss.Height(m.permView())
	}
	if m.quest != nil {
		// ask_user modal: title + question + options inside a border. Measure the
		// render so a wrapping question or long option can't under-reserve.
		h += lipgloss.Height(m.questView())
	}
	if m.resuming {
		rows := len(m.resumeList)
		if rows > resumeRows {
			rows = resumeRows + 1 // +1 for the "n/N" position line
		}
		h += rows + 1 // header line of the picker
	}
	if m.routing {
		if m.profileForm != nil {
			h += len(m.profileForm.fields) + 3 // fields + spacer + Save + header
		} else {
			h += len(m.routeList) + 3 // rows + header + profiles separator (blank + label)
			// The session-row suggest box adds its own lines below that row.
			if m.routeEditing && len(m.routeList) > 0 && m.routeList[m.routeSel].kind == rowSession {
				if n := len(m.modelSuggestions()); n > 0 {
					h += n
				} else if !m.catalogLoaded {
					h++ // "loading models…" hint line
				}
			}
		}
	}
	if n := len(m.paletteMatches()); !m.running && n > 0 {
		h += n + 2 // palette rows + box border
	}
	// The snackbar is a floating toast overlay (overlayLine), so it reserves no row.
	return h
}

func (m *Model) chromeHeight() int { return m.baseChromeHeight() + m.panesBlockHeight() }

// overlayLine composites overlay onto a screen row of content at display column
// col (ANSI-aware), without changing the content's dimensions — used for toasts.
func overlayLine(content string, row, col int, overlay string) string {
	lines := strings.Split(content, "\n")
	if row < 0 || row >= len(lines) {
		return content
	}
	line := lines[row]
	w := ansi.StringWidth(line)
	ow := ansi.StringWidth(overlay)
	if col < 0 {
		col = 0
	}
	left := ansi.Cut(line, 0, col)
	if lw := ansi.StringWidth(left); lw < col {
		left += strings.Repeat(" ", col-lw) // pad if the line is shorter than col
	}
	right := ""
	if col+ow < w {
		right = ansi.Cut(line, col+ow, w)
	}
	lines[row] = left + overlay + right
	return strings.Join(lines, "\n")
}

// overlayBox composites a multi-line box onto content, its top-left at (top, left).
func overlayBox(content string, top, left int, box string) string {
	for i, bl := range strings.Split(box, "\n") {
		content = overlayLine(content, top+i, left, bl)
	}
	return content
}

// panesBlockHeight is the rows reserved for the tiled subagent overview. Zero
// when there are no panes or when zoomed (zoom takes over the whole viewport).
// paneLayout is the SINGLE source of truth for how the agent panes fit on screen,
// consumed by both panesBlockHeight (reserve) and renderPanes (render) so they can
// never drift. It caps the pane block to the space left after base chrome and a
// minimum viewport, and folds any overflow into a "+N more" summary row.
//
//	nShown  = panes actually rendered
//	perPane = rows each running pane gets (collapsed panes are 1 row)
//	more    = panes hidden behind the "+N more" line (0 = none)
//	total   = exact rendered height (== panesBlockHeight)
func (m *Model) paneLayout() (nShown, perPane, more, total int) {
	n := len(m.panes)
	if n == 0 || m.zoom {
		return 0, 0, 0, 0
	}
	capH := m.height - m.baseChromeHeight() - minViewport
	if capH < 0 {
		capH = 0
	}
	// Cap the block to ~half the screen so panes never crowd out the transcript;
	// the overflow scrolls instead of growing. (minViewport alone would allow the
	// block to fill everything down to a 3-row viewport.)
	if frac := m.height * panesMaxNumer / panesMaxDenom; capH > frac {
		capH = frac
	}
	if !m.running {
		// Collapsed: one row per finished pane (the post-turn strip). Mid-turn, finished
		// panes stay as boxes and fade out individually (renderPanes dims by p.fade).
		if n <= capH {
			return n, 1, 0, n
		}
		show := capH - 1 // reserve one row for "+N more"
		if show <= 0 {
			if capH >= 1 {
				return 0, 0, n, 1 // only the summary fits
			}
			return 0, 0, 0, 0
		}
		return show, 1, n - show, show + 1
	}
	// Running: a pane box is title + >=1 content + 2 border = >=4 rows (and renders
	// exactly perPane rows when perPane>=4). Size the COUNT so show*4 fits the budget
	// (capH minus a "+N more" row on overflow), so per-pane never floors past the cap.
	const minPane, maxPane = 4, 6
	maxShow := capH / minPane
	if n > maxShow {
		maxShow = (capH - 1) / minPane // overflow → one row goes to "+N more"
	}
	show := n
	if show > maxShow {
		show = maxShow
	}
	if show <= 0 {
		if capH >= 1 {
			return 0, 0, n, 1 // only the summary fits
		}
		return 0, 0, 0, 0
	}
	more = n - show
	budget := capH
	if more > 0 {
		budget-- // reserve the "+N more" row
	}
	perPane = budget / show // >= minPane by construction (show*minPane <= budget)
	if perPane > maxPane {
		perPane = maxPane // compact panes; a couple of agents shouldn't fill the screen
	}
	total = show * perPane
	if more > 0 {
		total++
	}
	return show, perPane, more, total
}

func (m *Model) panesBlockHeight() int {
	_, _, _, total := m.paneLayout()
	return total
}

// refresh recomputes the viewport content and pins it to the bottom.
func (m *Model) refresh() {
	if !m.ready {
		return
	}
	// The input box height is owned by textarea.DynamicHeight (set in newModel):
	// it sizes to the true visual line count — soft wraps included — capped at
	// maxInputRows, recomputed inside ta.Update before refresh runs. So we read
	// m.ta.Height() below rather than setting it here.
	// On the fresh splash screen the input sits centered under the wordmark at a
	// narrower width; elsewhere it spans the transcript. Keep the textarea wrapped to
	// whichever is active so its rendered lines match the box we draw around it.
	wantW := m.width - 6 // box total is m.width-2; border+padding eat 4, so rows must be m.width-6
	if m.splashActive() {
		// Proportional, not a fixed cap: at ~100-col terminals a fixed 100-col cap made
		// the box a near-full-width bar under the 49-col console diagram — technically
		// centered, but reading as a bottom bar rather than a centered prompt. 60% of
		// the width keeps it visibly narrower than the transcript at every size while
		// staying wide enough to type a real first prompt without immediate wrapping.
		wantW = clampInt((m.width*3)/5, 44, 100)
	}
	if wantW != m.ta.Width() {
		m.ta.SetWidth(wantW)
	}
	// Capture follow intent from the CURRENT state, before any height/content
	// change — a layout change (subagent pane added/removed, terminal resize) or
	// new content must not flip whether we were pinned to the bottom.
	follow := m.vp.AtBottom()
	vpHeight := m.height - m.chromeHeight()
	if vpHeight < minViewport {
		vpHeight = minViewport
	}
	// Reserve the right status panel's width; the transcript wraps to the rest.
	tw := m.width - m.panelCols()
	vpw := tw
	m.buildGlam(vpw)
	m.vp.SetWidth(vpw)
	m.vp.SetHeight(vpHeight)
	content := m.transcript()
	if m.councilDetail != nil {
		// Council verdict detail takes over the viewport (like a zoomed pane).
		content = m.renderCouncilDetail(vpw)
	} else if m.zoom {
		// Zoomed: the viewport shows the focused subagent's full transcript.
		content = m.renderZoom(vpw)
	}
	// Keep styled + ANSI-stripped copies for cell-precise selection + copy.
	m.contentLines = strings.Split(content, "\n")
	m.contentPlain = make([]string, len(m.contentLines))
	for i, l := range m.contentLines {
		m.contentPlain[i] = ansi.Strip(l)
	}
	if m.selecting || m.selActive {
		content = m.highlightSelection()
	}
	if m.searching {
		content = m.highlightSearch(content)
	}
	m.vp.SetContent(content)
	if follow {
		m.vp.GotoBottom()
	}
}

// selBounds returns the selection ordered so (sl,sc) <= (el,ec).
func (m *Model) selBounds() (sl, sc, el, ec int) {
	sl, sc, el, ec = m.selAL, m.selAC, m.selHL, m.selHC
	if sl > el || (sl == el && sc > ec) {
		sl, sc, el, ec = el, ec, sl, sc
	}
	return
}

// snapSelCols snaps a line's selection columns to grapheme cell boundaries of the
// plain text: the start floors to the glyph's first cell, the end ceils past its
// last. Without this, an endpoint landing on the SECOND cell of a wide character
// (Hangul, emoji, CJK) bisects the glyph, and the three ANSI-aware cuts around it
// disagree about which side owns it — the left cut drops the glyph while the
// middle cut adopts it — so the highlight edge jumps a full glyph as the pointer
// crosses each wide character. Snapping makes the cut seams always coincide with
// glyph seams, and the whole glyph under either endpoint is selected.
func snapSelCols(plain string, c0, c1 int) (int, int) {
	s0, s1, w := 0, -1, 0
	g := uniseg.NewGraphemes(plain)
	for g.Next() {
		cw := g.Width()
		if w < c0 && w+cw <= c0 {
			// still left of the start — advance the floor candidate
		} else if w <= c0 {
			s0 = w // c0 lands inside/at this glyph → floor to its first cell
		}
		if s1 < 0 && w+cw >= c1 {
			s1 = w + cw // c1 lands inside/at this glyph → ceil past its last cell
			if c1 == w {
				s1 = w // …unless it sits exactly on the glyph's leading seam
			}
		}
		w += cw
	}
	if c0 >= w {
		s0 = w
	}
	if s1 < 0 || c1 >= w {
		s1 = w
	}
	return s0, s1
}

// highlightSelection reverse-videos the selected cell range on each line,
// preserving the surrounding markdown styling (cells are cut ANSI-aware).
func (m *Model) highlightSelection() string {
	sl, sc, el, ec := m.selBounds()
	out := make([]string, len(m.contentLines))
	copy(out, m.contentLines)
	for i := sl; i <= el && i < len(out); i++ {
		if i < 0 {
			continue
		}
		styled := m.contentLines[i]
		w := ansi.StringWidth(styled)
		c0, c1 := 0, w
		if i == sl {
			c0 = sc
		}
		if i == el {
			c1 = ec
		}
		if c0 < 0 {
			c0 = 0
		}
		if c1 > w {
			c1 = w
		}
		if c0 >= c1 {
			continue
		}
		if i < len(m.contentPlain) {
			c0, c1 = snapSelCols(m.contentPlain[i], c0, c1)
		}
		if c0 >= c1 {
			continue
		}
		mid := ansi.Strip(ansi.Cut(styled, c0, c1))
		out[i] = ansi.Cut(styled, 0, c0) + styleSelection.Render(mid) + ansi.Cut(styled, c1, w)
	}
	return strings.Join(out, "\n")
}

// screenToContent maps a screen cell (x,y) to a (content line, display column),
// accounting for the header height and the current scroll offset.
func (m *Model) screenToContent(x, y int) (line, col int) {
	const vpTop = 2 // header: title + divider
	line = m.vp.YOffset() + (y - vpTop)
	if line < 0 {
		line = 0
	}
	if n := len(m.contentPlain); n > 0 && line >= n {
		line = n - 1
	}
	col = x
	if col < 0 {
		col = 0
	}
	if line >= 0 && line < len(m.contentPlain) {
		if w := ansi.StringWidth(m.contentPlain[line]); col > w {
			col = w
		}
	}
	return line, col
}

// selectedText returns the plain text of the current cell-precise selection.
func (m *Model) selectedText() string {
	if len(m.contentPlain) == 0 {
		return ""
	}
	sl, sc, el, ec := m.selBounds()
	if sl < 0 {
		sl = 0
	}
	if el >= len(m.contentPlain) {
		el = len(m.contentPlain) - 1
	}
	var rows []string
	for i := sl; i <= el; i++ {
		plain := m.contentPlain[i]
		w := ansi.StringWidth(plain)
		c0, c1 := 0, w
		if i == sl {
			c0 = sc
		}
		if i == el {
			c1 = ec
		}
		if c0 < 0 {
			c0 = 0
		}
		if c1 > w {
			c1 = w
		}
		if c0 > c1 {
			c0 = c1
		}
		// Same grapheme snap as the highlight, so what is copied is exactly what
		// reads as selected (a wide glyph under either endpoint is included whole).
		c0, c1 = snapSelCols(plain, c0, c1)
		rows = append(rows, strings.TrimRight(ansi.Cut(plain, c0, c1), " "))
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}
