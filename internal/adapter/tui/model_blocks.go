package tui

// Transcript block interactions: the council-verdict detail, expandable
// reasoning ("thought") blocks in the main and zoom views, per-block copy, and
// the zoom view's viewed-pane state. Pure moves from model.go.

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/core/event"
)

// handleKey processes global keybindings and the permission modal.
// handleMouse processes mouse events: drag selects+copies in-app, plain click
// focuses a subagent pane, and the wheel scrolls (mid-drag too). One mode, no
// toggle — wheel and drag-select coexist because the app owns the selection.
// toggleThoughtAt flips the expand state of the reasoning block at content line
// `line` (a click target). Returns true if a reasoning block was toggled.
// roundVerdicts returns the per-member verdicts recorded for a council round,
// scanning back to the most recent verdict block that matches (rounds are emitted
// in order, so the last matching block is the one being decided).
func (m *Model) roundVerdicts(round int) []event.CouncilVerdictData {
	for i := len(m.blocks) - 1; i >= 0; i-- {
		b := m.blocks[i]
		if b.kind == blockCouncilVerdict && len(b.councilVerdicts) > 0 && b.councilVerdicts[0].Round == round {
			return b.councilVerdicts
		}
	}
	return nil
}

// planTierTally counts a plan-audit round by what each verdict means for proceeding,
// not by the raw done/continue split: done→approve, abstain→abstain, and a continue
// is bucketed by its severity tier (critical→revise, warn→advise, info→note) so the
// summary mirrors the per-member labels and shows what was blocking vs advisory.
// "N approve" is always shown; the other tiers appear only when non-zero.
func planTierTally(vs []event.CouncilVerdictData) string {
	if len(vs) == 0 {
		return ""
	}
	var approve, revise, advise, note, abstain int
	for _, v := range vs {
		switch v.Decision {
		case "done":
			approve++
		case "abstain":
			abstain++
		default: // continue, bucketed by severity
			switch v.Severity {
			case "warn":
				advise++
			case "info":
				note++
			default: // critical or unset → blocking
				revise++
			}
		}
	}
	parts := []string{fmt.Sprintf("%d approve", approve)}
	if revise > 0 {
		parts = append(parts, fmt.Sprintf("%d revise", revise))
	}
	if advise > 0 {
		parts = append(parts, fmt.Sprintf("%d advise", advise))
	}
	if note > 0 {
		parts = append(parts, fmt.Sprintf("%d note", note))
	}
	if abstain > 0 {
		parts = append(parts, fmt.Sprintf("%d abstain", abstain))
	}
	return strings.Join(parts, " / ")
}

// openCouncilDetailAt opens the council detail modal if the clicked content line
// falls in a council-verdict block. Same block-lookup as toggleThoughtAt.
func (m *Model) openCouncilDetailAt(line int) bool {
	i := -1
	for j := len(m.blockLineStart) - 1; j >= 0; j-- {
		if line >= m.blockLineStart[j] {
			i = j
			break
		}
	}
	if i < 0 || i >= len(m.blocks) || m.blocks[i].kind != blockCouncilVerdict || len(m.blocks[i].councilVerdicts) == 0 {
		return false
	}
	// The row holds several members on one line; pick the one under the click column.
	// Segment k spans [x, x+width(member k)); separators (councilRowSep) sit between.
	vs := m.blocks[i].councilVerdicts
	pick := len(vs) - 1 // default to the last member if the click is past the end
	x := 2              // indent() prepends 2 spaces before the first member
	for k, v := range vs {
		w := ansi.StringWidth(councilMemberPlain(v))
		if m.selAC < x+w {
			pick = k
			break
		}
		x += w + ansi.StringWidth(councilRowSep)
	}
	vd := vs[pick]
	m.councilDetail = &vd
	m.councilDetailEvidence = m.blocks[i].evidence
	return true
}

// toggleLiveThinkAt folds/unfolds the streaming "thinking" block when the click
// lands on it. That block is the streaming tail, not a member of m.blocks, so
// toggleThoughtAt (which scans blockLineStart) can't reach it — handle it here.
// It shares the global showThink flag, so a click behaves exactly like ctrl+t.
func (m *Model) toggleLiveThinkAt(line int) bool {
	if m.liveThinkStart < 0 || line < m.liveThinkStart {
		return false
	}
	m.showThink = !m.showThink
	return true
}

// copyBlockAt copies a user/assistant block's SOURCE text when (line, col) hits the
// copy chip on its label line. The geometry mirrors the render exactly: bar (2 cells)
// + label + one space + a 3-cell chip (fold-chip padding around ⧉). Returns the
// clipboard command and whether the click was consumed.
func (m *Model) copyBlockAt(line, col int) (tea.Cmd, bool) {
	for i, start := range m.blockLineStart {
		if start != line || i >= len(m.blocks) {
			continue
		}
		b := m.blocks[i]
		var name string
		switch b.kind {
		case blockUser:
			name = m.userLabel()
		case blockAssistant:
			name = "magi"
		default:
			return nil, false
		}
		// bar (2) + label + timestamp chip (between label and copy chip, B-layout) + 1 space
		// + the 3-cell copy chip. The timestamp width must be included or the click column
		// for the chip drifts left by its width and the copy button becomes unclickable.
		chipStart := 2 + lipgloss.Width(name) + lipgloss.Width(tsChip(b.ts)) + 1
		if col < chipStart || col >= chipStart+3 {
			return nil, false
		}
		text := strings.TrimRight(b.text, "\n")
		if text == "" {
			return nil, false
		}
		copyToOSClipboard(text)
		return tea.Batch(tea.SetClipboard(text), m.snack(fmt.Sprintf("copied %d chars", len([]rune(text))))), true
	}
	return nil, false
}

func (m *Model) toggleThoughtAt(line int) bool {
	i := -1
	for j := len(m.blockLineStart) - 1; j >= 0; j-- {
		if line >= m.blockLineStart[j] {
			i = j
			break
		}
	}
	if i < 0 || i >= len(m.blocks) {
		return false
	}
	// Foldable blocks: a reasoning ("thought") block, or a tool block whose output
	// overflows the collapsed cap (e.g. a long bash result). Other lines fall through
	// so the click can focus a pane instead.
	b := m.blocks[i]
	if b.kind != blockReasoning && !(b.kind == blockToolCall && m.toolBodyOverflows(b)) {
		return false
	}
	if !m.thoughtClickSkip(line) {
		m.blocks[i].expanded = !m.blocks[i].expanded
		if len(m.cache) > i {
			m.cache = m.cache[:i] // re-render this block (and those after) at new height
		}
	}
	return true
}

// thoughtClickSkip reports whether this thought click is the 2nd half of a
// double-click on the same line (within a short window) and should therefore NOT
// re-toggle — so a double-click reliably EXPANDS instead of toggling twice back
// to collapsed. It always records the click for the next comparison.
func (m *Model) thoughtClickSkip(line int) bool {
	now := time.Now()
	skip := m.lastThoughtLine == line && now.Sub(m.lastThoughtAt) < 350*time.Millisecond
	m.lastThoughtAt = now
	m.lastThoughtLine = line
	return skip
}

// viewedPane returns the subagent pane shown in the zoom view: a finished pane
// pinned via zoomPane (clicked from the status panel's done roster), otherwise the
// focused live pane. nil when nothing is zoomable.
func (m *Model) viewedPane() *agentPane {
	if m.zoomPane != nil {
		return m.zoomPane
	}
	if m.focusPane >= 0 && m.focusPane < len(m.panes) {
		return m.panes[m.focusPane]
	}
	return nil
}

// exitZoom leaves the zoom view, dropping any pinned finished pane so the next
// zoom follows the live focus again.
func (m *Model) exitZoom() {
	m.zoom = false
	m.zoomPane = nil
}

// toggleThoughtAtZoom flips the expand state of the reasoning block at content
// line `line` in the focused subagent's zoom view. Returns true if a reasoning
// block was toggled. The zoom view rebuilds from p.blocks each frame (no cache),
// so toggling expanded and refreshing is enough to re-render at the new height.
func (m *Model) toggleThoughtAtZoom(line int) bool {
	p := m.viewedPane()
	if p == nil {
		return false
	}
	i := -1
	for j := len(m.paneLineStart) - 1; j >= 0; j-- {
		if line >= m.paneLineStart[j] {
			i = j
			break
		}
	}
	if i < 0 || i >= len(p.blocks) || p.blocks[i].kind != blockReasoning {
		return false
	}
	if !m.thoughtClickSkip(line) {
		p.blocks[i].expanded = !p.blocks[i].expanded
	}
	return true
}
