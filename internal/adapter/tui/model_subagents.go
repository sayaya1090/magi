package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/app"
)

// The /subagents list: what a plugin has registered, and which of them are on.
//
// magi ships none. A subagent is a tool a plugin declared as one, and the list is empty until a
// plugin registers one — the same reason the spawn seam it uses is inert out of the box. This
// exists so that when a plugin does register several, a user can see what they are and switch them
// off one at a time or a group at a time.

// subagentRowKind distinguishes a group header from the subagents under it.
type subagentRowKind int

const (
	subRowGroup subagentRowKind = iota
	subRowAgent
)

// subagentRow is one line in the list. A group header carries no state of its own: its checkbox is
// computed from its members every time it is drawn, so the two cannot drift apart.
type subagentRow struct {
	kind  subagentRowKind
	group string
	info  app.SubagentInfo
}

// openSubagents builds the list and shows it.
func (m *Model) openSubagents() tea.Cmd {
	m.refreshSubagentList()
	m.subSel, m.subagenting = 0, true
	return nil
}

// refreshSubagentList rebuilds the rows from the registry, inserting a header before each group.
// Called on open and after every toggle, so the derived header state is never stale.
func (m *Model) refreshSubagentList() {
	var rows []subagentRow
	last := "\x00" // not a possible group name, so the first row always opens one
	for _, s := range m.app.Subagents() {
		if s.Group != last {
			last = s.Group
			rows = append(rows, subagentRow{kind: subRowGroup, group: s.Group})
		}
		rows = append(rows, subagentRow{kind: subRowAgent, group: s.Group, info: s})
	}
	m.subagentList = rows
	if m.subSel >= len(rows) {
		m.subSel = max(0, len(rows)-1)
	}
}

// groupState reports how many of a group's members are on, and how many there are.
func (m *Model) groupState(group string) (on, total int) {
	for _, r := range m.subagentList {
		if r.kind != subRowAgent || r.group != group {
			continue
		}
		total++
		if r.info.Enabled {
			on++
		}
	}
	return on, total
}

// handleSubagentKey drives the list. Returns false when the key is not ours.
//
// Both settings live on the same row and are reached from the same screen: space switches a
// subagent on or off, enter edits which model it runs on. Putting the model in the plugin's own
// config section would have meant two places to go for one subagent.
func (m *Model) handleSubagentKey(key string) (tea.Cmd, bool) {
	if m.subEditing {
		return m.handleSubagentEditKey(key)
	}
	switch key {
	case "esc", "q":
		m.subagenting = false
		return nil, true
	case "enter":
		// Only a subagent has a model; a group header has nothing to point anywhere.
		if r, ok := m.selectedSubagent(); ok {
			m.subEditing, m.subBuf = true, r.info.Model
		}
		return nil, true
	case "up", "k":
		if m.subSel > 0 {
			m.subSel--
		}
		return nil, true
	case "down", "j":
		if m.subSel < len(m.subagentList)-1 {
			m.subSel++
		}
		return nil, true
	case " ", "x":
		return m.toggleSubagentRow(), true
	}
	return nil, false
}

// handleSubagentEditKey drives the model field while it is being typed.
func (m *Model) handleSubagentEditKey(key string) (tea.Cmd, bool) {
	switch key {
	case "esc":
		m.subEditing, m.subBuf = false, ""
		return nil, true
	case "enter":
		r, ok := m.selectedSubagent()
		m.subEditing = false
		if !ok {
			m.subBuf = ""
			return nil, true
		}
		// An empty field CLEARS the override, which is how a user gets back to "whatever the
		// plugin asked for" without having to remember what that was.
		err := m.app.SetSubagentModel(r.info.Name, m.subBuf, r.info.Provider)
		m.subBuf = ""
		m.refreshSubagentList()
		if err != nil {
			return m.snack("subagents: " + err.Error()), true
		}
		return nil, true
	case "backspace":
		if n := len(m.subBuf); n > 0 {
			m.subBuf = m.subBuf[:n-1]
		}
		return nil, true
	}
	if len(key) == 1 || key == " " {
		m.subBuf += key
	}
	return nil, true
}

// selectedSubagent returns the selected row when it is a subagent (not a group header).
func (m *Model) selectedSubagent() (subagentRow, bool) {
	if m.subSel < 0 || m.subSel >= len(m.subagentList) {
		return subagentRow{}, false
	}
	r := m.subagentList[m.subSel]
	return r, r.kind == subRowAgent
}

// toggleSubagentRow flips the selected row. On a group header it is a BULK ACTION over the
// members, not a state of its own: if any member is off, the whole group goes on — the reading
// that matches what a half-filled box invites you to do.
func (m *Model) toggleSubagentRow() tea.Cmd {
	if m.subSel < 0 || m.subSel >= len(m.subagentList) {
		return nil
	}
	r := m.subagentList[m.subSel]
	var err error
	switch r.kind {
	case subRowGroup:
		on, total := m.groupState(r.group)
		err = m.app.SetGroupEnabled(r.group, on < total)
	default:
		err = m.app.SetSubagentEnabled(r.info.Name, !r.info.Enabled)
	}
	m.refreshSubagentList()
	if err != nil {
		return m.snack("subagents: " + err.Error())
	}
	return nil
}

// subagentsView renders the list: a checkbox, the name, and what the plugin says it does.
func (m *Model) subagentsView() string {
	hint := "↑/↓ select · space on/off · enter set model · esc close"
	if m.subEditing {
		hint = "type a model · empty clears the override · enter apply · esc"
	}
	head := stylePermTitle.Render("subagents") + "  " + styleFooter.Render(hint)
	if m.width > 0 && lipgloss.Width(head) > m.width {
		head = ansi.Truncate(stylePermTitle.Render("subagents"), m.width, "")
	}
	if len(m.subagentList) == 0 {
		// Say WHY it is empty. magi ships none of its own, so an empty list is the expected state
		// and not a failure to load something.
		return head + "\n" + clipRow(styleFooter.Render(
			"  none registered — a plugin declares one with register_tool{subagent=true}"), m.bodyWidth())
	}

	inner := max(20, m.bodyWidth()-4)
	lines := make([]string, 0, len(m.subagentList))
	for i, r := range m.subagentList {
		sel := i == m.subSel
		var row string
		switch r.kind {
		case subRowGroup:
			on, total := m.groupState(r.group)
			name := r.group
			if name == "" {
				name = "(ungrouped)"
			}
			row = fmt.Sprintf("%s %s  %s", groupBox(on, total),
				styleToolName.Render(name), styleFooter.Render(fmt.Sprintf("%d/%d", on, total)))
		default:
			box := "[ ]"
			if r.info.Enabled {
				box = "[✓]"
			}
			// On/off, name, model, description — one row, one screen, in the order a reader
			// needs them. The model comes before the description because it is the other thing
			// they came here to change; the description is what gives when the window is narrow.
			label := "    " + box + " " + r.info.Name
			switch {
			case sel && m.subEditing:
				label += "  " + styleToolArgs.Render(m.subBuf+"▌")
			case r.info.Model != "":
				label += "  " + styleToolArgs.Render(r.info.Model)
			}
			if d := strings.TrimSpace(r.info.Description); d != "" {
				if room := inner - lipgloss.Width(label) - 2; room > 8 {
					label += "  " + styleToolResult.Render(oneLine(d, room))
				}
			}
			row = label
		}
		if sel {
			row = stylePalSelRow.Render(clipRow("› "+row, inner))
		} else {
			row = clipRow("  "+row, inner)
		}
		lines = append(lines, row)
	}

	// Window the rows to the room the modal has, the way every other list here does. Unbounded,
	// the frame grows past the terminal and the whole view is pushed off — a list of subagents can
	// be longer than a short window, and a plugin decides how many there are.
	draw := func(keep int) string {
		var b strings.Builder
		b.WriteString(head + "\n")
		start := 0
		if keep < len(lines) {
			start = min(max(0, m.subSel-keep/2), len(lines)-keep)
		}
		for i := start; i < start+keep; i++ {
			b.WriteString(lines[i] + "\n")
		}
		if keep < len(lines) {
			// Say what is outside the window, so a list that stops at the fourth does not read as
			// four being all there is.
			b.WriteString(m.cutMark(fmt.Sprintf("  %d/%d rows (↑/↓ to reach them)",
				m.subSel+1, len(lines))) + "\n")
		}
		return strings.TrimRight(b.String(), "\n")
	}
	room := m.modalRoom()
	for keep := len(lines); keep > 0; keep-- {
		if out := draw(keep); m.height <= 0 || lipgloss.Height(out) <= room {
			return out
		}
	}
	return clipRow(lines[min(m.subSel, len(lines)-1)], inner)
}

// groupBox is the header's tri-state checkbox, derived from the members every draw: all on, all
// off, or mixed. Nothing stores it, so nothing can disagree with the rows underneath.
func groupBox(on, total int) string {
	switch {
	case total == 0 || on == 0:
		return "[ ]"
	case on == total:
		return "[✓]"
	default:
		return "[~]"
	}
}
