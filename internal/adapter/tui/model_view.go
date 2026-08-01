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
	//
	// Truncated with an ANSI-aware cut rather than MaxWidth. The header is assembled from chips
	// that are ALREADY styled, and MaxWidth does not clip such content to the cells it occupies —
	// found by a random session at 53 columns, where a header carrying the scroll meter rendered
	// 59 cells wide and the vertical join padded every other row out to match it. styleHeader pads
	// one cell each side, so the content budget is width-2.
	header := styleHeader.Render(ansi.Truncate(headLine, max(0, m.width-2), "")) +
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
		// The spinner and the meter say the turn is alive and must always fit; the interrupt hint
		// is the droppable part. Unbounded, this row was the widest in the frame on a narrow
		// terminal and JoinVertical padded every other row out to match it.
		// Built longest-first and shortened until it fits: the spinner says the turn is alive and
		// must always be there, the meter is useful, the token/context detail is the first thing a
		// narrow screen can do without. Unbounded, this row overflowed at 46 columns — measured on
		// `working… 0s · ↑33.9k · ctx 0% · 33.9k/65.5k`, 49 cells — and the vertical join then
		// padded every other row out to match it.
		meter := turnMeter(time.Since(m.turnStart), m.turnIn, m.turnOut)
		for _, body := range []string{
			work + meter + gaugeSep(m.ctxGauge()) + "  ",
			work + meter + "  ",
			work + fmtDur(time.Since(m.turnStart)) + "  ",
			work,
		} {
			status = "  " + m.sp.View() + styleFooter.Render(" "+body)
			if m.width <= 0 || lipgloss.Width(status) <= m.width {
				break
			}
		}
		if hint := footerKeys("esc", "interrupt"); m.width <= 0 || lipgloss.Width(status)+lipgloss.Width(hint) <= m.width {
			status += hint
		}
	case m.turnDur > 0:
		// Same degradation as the live row: the elapsed time is the point, the token and context
		// detail is what a narrow screen does without.
		var meter string
		for _, body := range []string{
			"  " + turnMeter(m.turnDur, m.turnIn, m.turnOut) + gaugeSep(m.ctxGauge()),
			"  " + turnMeter(m.turnDur, m.turnIn, m.turnOut),
			"  " + fmtDur(m.turnDur),
		} {
			meter = styleFooter.Render(body)
			if m.width <= 0 || lipgloss.Width(meter) <= m.width {
				break
			}
		}
		status = meter
		if hints := footerWidth(m.width - lipgloss.Width(meter) - 3); hints != "" {
			status += "   " + hints
		}
	default:
		status = footerWidth(m.width)
	}
	if debugFade {
		status += styleFooter.Render(m.fadeDebug())
	}
	// On a screen too short for the chrome, the status row is what gives: the header says where
	// you are and the input is how you get out, and neither can be dropped. Below this the frame
	// is header(2) + input box(3) and cannot shrink further without hiding one of those.
	if m.height > 0 && m.height < m.baseChromeHeight() {
		status = ""
	}

	// The input stays live even while a turn runs — you can keep typing, and
	// pressing enter queues the prompt for when the turn finishes.
	splash := m.splashActive()
	inputStyle := styleInput
	if m.ta.Focused() {
		inputStyle = styleInputFocus
	}
	// Style.Width is the box's TOTAL width — border and padding included — so the
	// text area inside is Width-4. The textarea's view rows are prompt+inner wide
	// (ta.Width() returns the INNER width; SetWidth reserves the prompt), and the
	// box MUST leave them at least that much room: a box even 2 cols short re-wraps
	// any FULL view row. Only space-less input force-breaks rows to full width —
	// prompts with spaces wrap early at word boundaries — which is why this bug hid:
	// with unbroken input (long paths, spaceless Korean) rows lost their prompt,
	// single glyphs spilled onto their own rows, and the reported cursor drifted by
	// the accumulated difference while IME pre-edit rendered at that wrong spot.
	inputContentW := m.width - 2 // total box width; ta rows are m.width-7, +1 slack (see refresh)
	if splash {
		inputContentW = m.ta.Width() + lipgloss.Width(m.ta.Prompt) + 5 // +1 slack: the EOL cursor cell widens a full row by one
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
		vpContent, splashCurRow, splashCurCol = splashCompose(vpw, m.vp.Height(), m.splashConsole(), m.splashIdentity(), input)
	} else if len(m.blocks) == 0 && !m.running && !m.resuming {
		// Fresh session but a modal is open: plain centered splash; the input stays
		// pinned at the bottom under the modal.
		vpContent = splashView(vpw, m.vp.Height(), m.splashConsole(), m.splashIdentity())
	} else {
		vpContent = m.vp.View()
		if strings.TrimSpace(vpContent) == "" {
			vpContent = " " // empty/blank content isn't padded; give it a space
		}
	}
	// Force every row to exactly vpw cells with our terminal-aware measure
	// (blank rows become spaces so panes/input still sit at the bottom).
	// A zero-height viewport contributes NO rows. composeBox already returns "" for it, but an
	// empty string is still a row to JoinVertical, so the frame gained a blank line exactly when
	// it had none to spare — on a terminal too short for the chrome, that one row is the
	// difference between fitting and pushing the header off an alt screen.
	vpv := composeBox(vpContent, vpw, m.vp.Height())
	var leftRows []string
	if vpv != "" {
		leftRows = append(leftRows, vpv)
	}
	aboveInput := 2 + m.vp.Height() // header(2: title+divider) + viewport rows above input
	if pv := m.renderPanes(tw, aboveInput); pv != "" {
		leftRows = append(leftRows, pv)
		aboveInput += lipgloss.Height(pv)
	}
	left := lipgloss.JoinVertical(lipgloss.Left, leftRows...)
	// Same reason as leftRows above: an empty section is not a row. With the transcript squeezed
	// to nothing by a modal on a short screen, joining "" here put the blank back and the frame
	// was one row taller than the terminal — enough, on an alt screen, to take the header with it.
	parts := []string{header}
	if left != "" {
		parts = append(parts, left)
	}
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
	//
	// Clipped to what is left of the row. It floats over the frame rather than joining it, so
	// nothing else measures it and an over-long notice simply ran off the screen — found by a
	// random session at 59 and 62 columns, ordinary widths for a split pane, on the steer notice
	// ("queued · runs after the current task finishes (agent may fold it in sooner)", 76 cells).
	// The style pads one cell each side, and it starts at column 2.
	if m.snackbar != "" {
		if room := m.width - 2 - 2; room > 0 {
			v.Content = overlayLine(v.Content, 1, 2, styleToast.Render(clipLine(m.snackbar, room)))
		}
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
	// Last: the frame is finished, so this reads exactly what will be drawn (MAGI_DEBUG_FRAMES).
	m.dumpFrame(v.Content)
	return v
}

// paletteView renders the slash-command completion popup.
func (m *Model) paletteView(matches []cmdInfo) string {
	sel := m.clampSel(len(matches))
	// Bound it to the room this slot has, the way the modals drawn in the same slot do. The popup
	// was unbounded: twenty commands drew 55 rows at 34 columns, where a long "/name description"
	// wraps, and the frame ran that far past the terminal — on an alt-screen UI the top is simply
	// gone. The selected row is kept on screen, because it is the one being chosen, and the cut is
	// marked: a list silently ending at the tenth command reads as a magi with ten commands.
	room := m.modalRoom()
	full := m.paletteBody(matches, sel)
	if m.height <= 0 || lipgloss.Height(full) <= room {
		return full
	}
	// There used to be a `room < 3` arm here returning the unbounded popup, on the reasoning that
	// two rows are not enough to window into. What it actually did was switch the bound OFF on the
	// shortest terminals — the ones that need it. The walk found it at eight rows, where
	// modalRoom is 2 and the popup drew 55: the frame ran 47 rows past an eight-row screen and on
	// an alt screen everything above the last eight is gone. A short terminal falls through to the
	// marker instead, which is one row and still says the list is there.
	// Shrink by MEASURING, not by counting: a long "/name   description" wraps on a narrow
	// terminal, so a row is not a line. The window is centred on the selection — that is the one
	// being chosen — and the cut is marked, because a list silently ending at the tenth command
	// reads as a magi with ten commands.
	for keep := len(matches) - 1; keep >= 1; keep-- {
		start := min(max(0, sel-keep/2), len(matches)-keep)
		body := m.paletteBody(matches[start:start+keep], sel-start)
		out := body + "\n" + m.cutMark(fmt.Sprintf("  … %d more (type to narrow)", len(matches)-keep))
		if lipgloss.Height(out) <= room {
			return out
		}
	}
	// Not even one row and a marker fit: the marker alone still says the list is there.
	return m.cutMark(fmt.Sprintf("  … %d commands (type to narrow)", len(matches)))
}

// cutMark renders the "N more" line under a windowed list, cut to the terminal. Its rows sit
// inside a box that was given a width; this line does not, and "  … 20 commands (type to narrow)"
// is 33 cells however narrow the terminal is — found by the walk at 21 columns, and found again
// at 23 the moment a second list grew a marker of its own. A marker that reports a cut by
// overflowing the screen is the defect it was added to report, so every list that windows itself
// builds its marker here rather than writing the same truncation a third time.
//
// styleFooter pads a cell on each side, so the budget for the text is two less than the terminal.
func (m *Model) cutMark(text string) string {
	if m.width > 0 {
		text = ansi.Truncate(text, max(1, m.width-2), "")
	}
	return styleFooter.Render(text)
}

// paletteBody renders the rows themselves; paletteView decides how many there are room for.
func (m *Model) paletteBody(matches []cmdInfo, sel int) string {
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
	// The member's advisory "keep": what a revision must NOT lose. It is produced, persisted per
	// member, and prepended to the feedback injected into the model — but it had no rendering path
	// at all, so the one instruction protecting work already done was the only part of a verdict
	// the user could not read. It belongs directly under "next step": the two are read together —
	// what to change, and what changing it must not break.
	if k := strings.TrimSpace(v.Keep); k != "" {
		b.WriteString("\n" + styleFooter.Render("keep — the revision must not lose this") + "\n" + wrap.Render(k) + "\n")
	}
	// The grounds. magi looked this fragment up in the material the member was shown, so it is the
	// one part of a verdict a reader can check rather than weigh — and the empty case is the one
	// worth seeing most: a "done" standing on nothing observed is a fact about that vote, and
	// leaving it blank on screen hides exactly the verdict a reader should look at twice.
	b.WriteString("\n" + styleFooter.Render("grounds") + "\n" + wrap.Render(citeLabel(v.Cite)) + "\n")
	return b.String()
}

// citeLabel renders a verdict's grounds, including the two ways there are none. "NO-EVIDENCE" is
// the member saying plainly it judged the report's substance; an empty field is a member that did
// not answer, and those are different enough to be worth distinct words.
func citeLabel(cite string) string {
	switch c := strings.TrimSpace(cite); {
	case c == "":
		return "none given"
	case strings.EqualFold(c, "NO-EVIDENCE"):
		return "none observed — judged on the report's substance"
	default:
		return "\"" + c + "\""
	}
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
		add("The agent's own plan", d.Plan)
		add("Agent report (the claim)", d.Report)
	}
	if len(d.Signals) > 0 {
		add("Signals", strings.Join(d.Signals, "\n"))
	}
	// Between Signals and Changes, where the members' own prompt puts it.
	add("What the turn's tools produced", d.Actions)
	add("Changes", colorizeChanges(d.Changes))
	if d.NoChanges {
		b.WriteString("(no files changed — a read-only / answer turn)\n")
	}
	// Whether the round ASKED for a keep. Without it a verdict with no keep section is ambiguous —
	// nobody was asked, or everyone was asked and none answered — and a gate that silently stops
	// asking looks exactly like one that asks and gets nothing back.
	if d.Keep {
		b.WriteString("(members were asked what a revision must preserve — each verdict's \"keep\")\n")
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
	// Same rule as the permission modal: it must fit the screen it draws over. The question and
	// the options are the prompt — the keyboard hint is not — so the hint is what goes first, and
	// past that the option list is windowed rather than the frame overflowing.
	room := m.modalRoom()
	full := strings.TrimRight(b.String(), "\n")
	if out := stylePermBox.Width(m.width - 4).Render(full); m.height <= 0 || lipgloss.Height(out) <= room {
		return out
	}
	// The window is centred on the selection and the cut is marked — the same two rules the
	// palette drawn in this slot follows. It used to keep the FIRST keep options instead, which on
	// a short terminal drew a numbered list ending at "4." with the answer being chosen off
	// screen and no highlight anywhere in the box: the user is picking blind from a list that
	// looks complete. This modal is the one thing they have to read and answer.
	for keep := len(q.options) - 1; keep >= 1; keep-- {
		start := min(max(0, q.sel-keep/2), len(q.options)-keep)
		var t strings.Builder
		t.WriteString(stylePermTitle.Render("question") + "\n" + q.question + "\n")
		for i := start; i < start+keep; i++ {
			line := fmt.Sprintf("%d. %s", i+1, q.options[i])
			if i == q.sel {
				t.WriteString(stylePalSelRow.Render("› "+line) + "\n")
			} else {
				t.WriteString("  " + styleToolResult.Render(line) + "\n")
			}
		}
		t.WriteString(styleFooter.Render(fmt.Sprintf("  … %d more (↑/↓ to reach them)",
			len(q.options)-keep)))
		out := stylePermBox.Width(m.width - 4).Render(strings.TrimRight(t.String(), "\n"))
		if lipgloss.Height(out) <= room {
			return out
		}
	}
	return ansi.Truncate(stylePermTitle.Render("question")+" "+q.question, max(1, m.width), "")
}

// modalRoom is how many rows a modal may occupy: the screen minus the chrome that is always
// there — header(2), the bordered input (its rows plus a border above and below) and the status
// row. baseChromeHeight already ADDS the modal's own height to that, so a modal sized against
// anything larger makes the whole frame taller than the terminal.
func (m *Model) modalRoom() int {
	inputRows := m.ta.Height()
	if inputRows < 1 {
		inputRows = 1
	}
	return m.height - (2 + inputRows + 2 + 1)
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

// permIndex is the button row position of a decision, so a key can FOCUS a button without
// pressing it. -1 is impossible for the decisions passed here, but a wrong name must not index
// out of range: fall back to the first button (allow), which is also the default selection.
func permIndex(decision string) int {
	for i, b := range permButtons() {
		if b.decision == decision {
			return i
		}
	}
	return 0
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
	buttons := strings.Join(btns, " ")
	hint := styleFooter.Render("tab/click move · enter pick · p saves to .magi/config.toml")

	// The modal must FIT. It is the one thing the user has to read and answer, and it draws over
	// a screen whose height it never consulted — found by a random session: 20 rows in an 11-row
	// terminal, which on an alt screen puts the question itself off the display while the buttons
	// remain. Shed from the least load-bearing end: the keyboard hint, then the policy reason,
	// then the arguments. The tool's NAME and the buttons are never dropped — without them the
	// prompt is unanswerable.
	room := m.modalRoom()
	full := body + buttons + "\n" + hint
	for _, candidate := range []string{
		full,
		body + buttons,
		stylePermTitle.Render("permission required") + "\n" +
			fmt.Sprintf("run tool %s %s\n", styleToolName.Render(m.perm.name), styleToolArgs.Render(compactArgs(m.perm.args))) +
			buttons,
		stylePermTitle.Render("permission required") + "\n" +
			"run tool " + styleToolName.Render(m.perm.name) + "\n" + buttons,
	} {
		out := stylePermBox.Width(m.width - 4).Render(candidate)
		if m.height <= 0 || lipgloss.Height(out) <= room {
			return out
		}
	}
	// Nothing fits vertically: the name and the buttons, unboxed — but still cut to the screen,
	// or a prompt that was too tall becomes one that is too wide.
	bare := styleToolName.Render(m.perm.name) + " " + buttons
	return ansi.Truncate(bare, max(1, m.width), "")
}

// updateSearch recomputes the hit list for the current query (case-insensitive
// substring over the plain transcript) and jumps to the first hit at or below
// the current scroll position, so typing narrows in place instead of yanking
// the view back to the top.
func (m *Model) updateSearch() {
	if m.searchQuery == "" {
		m.searchHits = m.searchHits[:0]
		m.refresh()
		return
	}
	m.recomputeSearchHits()
	m.searchCur = 0
	for i, h := range m.searchHits {
		if h >= m.vp.YOffset() {
			m.searchCur = i
			break
		}
	}
	m.searchJump()
}

// recomputeSearchHits rebuilds the hit list against the transcript AS IT IS NOW.
//
// A hit is a line index, and the line numbering is not stable: every resize rewraps the
// transcript, and every new block lengthens it. The list was built once when the query was
// typed and never rebuilt, so after a resize the indexes pointed into a transcript that no
// longer had those lines — "next match" scrolled to nothing and the "3/7" counter counted
// matches that were no longer there. Found by the random walk on fourteen seeds, tripped by
// an ordinary resize.
//
// It does not jump: this runs from the render path, which cannot move the viewport without
// re-entering itself. searchCur is clamped rather than reset, so a reflow leaves the user on
// the match they were on instead of throwing them back to the first.
func (m *Model) recomputeSearchHits() {
	m.searchHits = m.searchHits[:0]
	if m.searchQuery == "" {
		m.searchCur = 0
		return
	}
	q := strings.ToLower(m.searchQuery)
	for i, l := range m.contentPlain {
		if strings.Contains(strings.ToLower(l), q) {
			m.searchHits = append(m.searchHits, i)
		}
	}
	if m.searchCur >= len(m.searchHits) {
		m.searchCur = max(0, len(m.searchHits)-1)
	}
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
//
// It gives up its hints one at a time rather than drawing past the right edge. The bar is a
// single row of fixed text — three key hints are already ~40 cells before the query — so on a
// narrow terminal it overflowed, and an over-wide row in a vertically joined frame pads every
// other row to match: the whole screen goes wider than the terminal and the shell wraps it.
// What survives to the last is the part that is not decoration: the query being typed and how
// many matches it has.
func (m *Model) searchView() string {
	pos := "0/0"
	if n := len(m.searchHits); n > 0 {
		pos = fmt.Sprintf("%d/%d", m.searchCur+1, n)
	}
	head := styleFooter.Render("  find: ") + m.searchQuery + styleFooter.Render("▏ "+pos+"  ")
	hints := []string{
		footerKeys("enter/↓", "next"),
		footerKeys("↑", "prev"),
		footerKeys("esc", "close"),
	}
	if m.width <= 0 {
		return head + strings.Join(hints, "")
	}
	// Drop hints from the LEFT: "esc close" is the one a user who cannot find the way out
	// needs, so it is the last to go.
	for i := 0; i <= len(hints); i++ {
		row := head + strings.Join(hints[i:], "")
		if ansi.StringWidth(row) <= m.width {
			return row
		}
	}
	return ansi.Truncate(head, m.width, "")
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

// footerWidth renders the key hints, dropping the least important ones until they fit the terminal.
//
// It used to render all three unconditionally, and it is the one row in the frame that never
// measured itself against the screen: at 20 columns everything else clipped correctly and the
// hints alone were 48 cells, so JoinVertical padded EVERY row to 48 and the whole frame overflowed
// a 20-column terminal. That is the wrap the header goes out of its way to prevent — the comment
// there says a soft-wrapped row desyncs physical rows from the logical layout and throws off the
// overlay click hit-testing — arriving from the bottom of the screen instead.
//
// Dropped from the right: quit is discoverable elsewhere (/quit), interrupt matters while a turn
// runs, and send is the one a new user needs. Below even that, nothing — an unreadable smear of
// half a hint is worse than a clean empty row.
// footerWidth is the hint row bounded to w cells. Non-positive means there is NO room, and no room
// means no hints — it used to mean "unbounded", which is the same value a caller computes when the
// meter beside the hints has already eaten the whole row, so the row that had least space printed
// the most (88 cells in a 31-column terminal).
func footerWidth(w int) string {
	if w <= 0 {
		return ""
	}
	hints := [][2]string{{"enter", "send"}, {"esc", "interrupt"}, {"ctrl+q", "quit"}}
	for n := len(hints); n > 0; n-- {
		out := styleFooter.Render("")
		for _, h := range hints[:n] {
			out += footerKeys(h[0], h[1])
		}
		if lipgloss.Width(out) <= w {
			return out
		}
	}
	return ""
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
