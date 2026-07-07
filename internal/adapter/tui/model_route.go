package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/sayaya1090/magi/internal/app"
)

// sessionRouteRow is the editor's first row: the session's default model.
const sessionRouteRow = "(session)"

type routeRowKind int

const (
	rowSession routeRowKind = iota
	rowAgent
	rowProfile
	rowAddProfile
)

// routeRow is one line in the models & routing editor.
type routeRow struct {
	kind  routeRowKind
	name  string // "(session)", agent name, or profile name
	value string // display value
}

// profileForm is the multi-field sub-editor for an LLM profile definition.
type profileForm struct {
	isNew   bool
	name    string
	fields  []formField
	sel     int // == len(fields) selects the [save] action
	editing bool
	buf     string
}

type formField struct {
	label  string
	value  string
	secret bool // mask in display (api_key)
}

// openRouteEditor opens the models & routing editor: session model, per-agent
// routing, the defined profiles, and an "+ add profile" row.
func (m *Model) openRouteEditor() {
	m.profileForm = nil
	m.refreshRouteList()
	m.routeSel, m.routing, m.routeEditing, m.routeBuf = 0, true, false, ""
}

func (m *Model) refreshRouteList() {
	rows := []routeRow{{kind: rowSession, name: sessionRouteRow, value: m.model}}
	for _, r := range m.app.AgentRoutes(m.sid) {
		v := r.Model
		if r.Provider != "" {
			v += "  @" + r.Provider
		}
		rows = append(rows, routeRow{kind: rowAgent, name: r.Name, value: v})
	}
	for _, p := range m.app.Profiles() {
		ep := p.BaseURL
		if ep == "" {
			ep = "(default endpoint)"
		}
		rows = append(rows, routeRow{kind: rowProfile, name: "profile:" + p.Name, value: ep + " · " + p.Model})
	}
	rows = append(rows, routeRow{kind: rowAddProfile, name: "+ add profile"})
	m.routeList = rows
}

// handleRouteKey drives the editor; delegates to the profile sub-form when open.
func (m *Model) handleRouteKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.profileForm != nil {
		return m.handleProfileForm(msg)
	}
	if len(m.routeList) == 0 {
		m.routing = false
		return nil, true
	}
	if m.routeEditing {
		switch msg.String() {
		case "enter":
			row := m.routeList[m.routeSel]
			val := strings.TrimSpace(m.routeBuf)
			if row.kind == rowSession {
				if val != "" {
					m.app.SetModel(m.sid, val)
					m.model = val
				}
			} else {
				m.app.SetAgentRoute(row.name, val)
			}
			m.refreshRouteList()
			m.routeEditing = false
			m.refresh()
			return nil, true
		case "esc":
			m.routeEditing = false
			m.refresh()
			return nil, true
		case "backspace":
			if n := len(m.routeBuf); n > 0 {
				m.routeBuf = m.routeBuf[:n-1]
			}
			m.routePickIdx = -1 // back to free text
			m.refresh()
			return nil, true
		case "left":
			if m.routeList[m.routeSel].kind == rowAgent {
				m.cycleProfilePick(-1)
			}
			m.refresh()
			return nil, true
		case "right":
			if m.routeList[m.routeSel].kind == rowAgent {
				m.cycleProfilePick(+1)
			}
			m.refresh()
			return nil, true
		}
		if t := msg.Key().Text; t != "" {
			m.routeBuf += t
			m.routePickIdx = -1 // typing overrides the profile picker
			m.refresh()
		}
		return nil, true
	}
	switch msg.String() {
	case "up", "ctrl+p":
		if m.routeSel > 0 {
			m.routeSel--
		}
	case "down", "ctrl+n":
		if m.routeSel < len(m.routeList)-1 {
			m.routeSel++
		}
	case "enter":
		switch row := m.routeList[m.routeSel]; row.kind {
		case rowSession, rowAgent:
			m.routeEditing = true
			m.routeBuf = ""
			m.routePickIdx = -1
		case rowProfile:
			m.openProfileForm(strings.TrimPrefix(row.name, "profile:"))
		case rowAddProfile:
			m.openProfileForm("")
		}
	case "esc", "ctrl+c":
		m.routing = false
	}
	m.refresh()
	return nil, true
}

// cycleProfilePick steps the agent-row edit buffer through the defined profiles
// (←/→), so a profile can be picked instead of typed. Wraps around.
func (m *Model) cycleProfilePick(dir int) {
	profs := m.app.Profiles()
	if len(profs) == 0 {
		return
	}
	m.routePickIdx += dir
	n := len(profs)
	if m.routePickIdx < 0 {
		m.routePickIdx = n - 1
	} else if m.routePickIdx >= n {
		m.routePickIdx = 0
	}
	m.routeBuf = profs[m.routePickIdx].Name
}

// openProfileForm opens the profile sub-editor for an existing profile (name set)
// or a new one (empty name).
func (m *Model) openProfileForm(name string) {
	f := &profileForm{isNew: name == "", name: name}
	var def app.ProfileDef
	for _, p := range m.app.Profiles() {
		if p.Name == name {
			def = p
		}
	}
	if f.isNew {
		f.fields = append(f.fields, formField{label: "name"})
	}
	hk, hv := firstHeader(def.Headers)
	f.fields = append(f.fields,
		formField{label: "base_url", value: def.BaseURL},
		formField{label: "api_key", value: def.APIKey, secret: true},
		formField{label: "model", value: def.Model},
		formField{label: "header_key", value: hk},
		formField{label: "header_value", value: hv},
	)
	m.profileForm = f
	m.refresh()
}

func (m *Model) handleProfileForm(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	f := m.profileForm
	if f.editing {
		switch msg.String() {
		case "enter":
			f.fields[f.sel].value = strings.TrimSpace(f.buf)
			f.editing = false
		case "esc":
			f.editing = false
		case "backspace":
			if n := len(f.buf); n > 0 {
				f.buf = f.buf[:n-1]
			}
		default:
			if t := msg.Key().Text; t != "" {
				f.buf += t
			}
		}
		m.refresh()
		return nil, true
	}
	switch msg.String() {
	case "tab":
		m.saveProfileForm() // quick-save from anywhere in the form
		return nil, true
	case "up", "ctrl+p":
		if f.sel > 0 {
			f.sel--
		}
	case "down", "ctrl+n":
		if f.sel < len(f.fields) { // last position == [save]
			f.sel++
		}
	case "enter":
		if f.sel == len(f.fields) {
			m.saveProfileForm()
			return nil, true
		}
		f.editing = true
		f.buf = f.fields[f.sel].value
	case "esc", "ctrl+c":
		m.profileForm = nil // discard, back to the list
	}
	m.refresh()
	return nil, true
}

// saveProfileForm builds a ProfileDef from the fields and applies+persists it.
func (m *Model) saveProfileForm() {
	f := m.profileForm
	get := func(label string) string {
		for _, fl := range f.fields {
			if fl.label == label {
				return strings.TrimSpace(fl.value)
			}
		}
		return ""
	}
	name := f.name
	if name == "" {
		name = get("name")
	}
	if name != "" {
		def := app.ProfileDef{Name: name, BaseURL: get("base_url"), APIKey: get("api_key"), Model: get("model")}
		if hk := get("header_key"); hk != "" {
			def.Headers = map[string]string{hk: get("header_value")}
		}
		m.app.SetProfile(def)
	}
	m.profileForm = nil
	m.refreshRouteList()
	m.refresh()
}

func firstHeader(h map[string]string) (string, string) {
	for k, v := range h {
		return k, v
	}
	return "", ""
}

// routeView renders the editor (or the profile sub-form when open).
func (m *Model) routeView() string {
	if m.profileForm != nil {
		return m.profileFormView()
	}
	var b strings.Builder
	hint := "↑/↓ select · enter edit/open · esc close"
	if m.routeEditing {
		hint = "type value · ←/→ pick profile · enter apply · empty clears · esc"
	}
	b.WriteString(stylePermTitle.Render("models & routing") + "  " + styleFooter.Render(hint) + "\n")
	sepDrawn := false
	for i, r := range m.routeList {
		// Set the profiles section (profile rows + add button) apart from the
		// session/agent rows with a blank line and a dim header.
		if !sepDrawn && (r.kind == rowProfile || r.kind == rowAddProfile) {
			b.WriteString("\n" + styleFooter.Render("backends (profiles)") + "\n")
			sepDrawn = true
		}
		if r.kind == rowAddProfile {
			btn := " + add profile "
			if i == m.routeSel {
				b.WriteString("  " + styleBtnSel.Render(btn) + "\n")
			} else {
				b.WriteString("  " + styleBtn.Render(btn) + "\n")
			}
			continue
		}
		val := r.value
		if i == m.routeSel && m.routeEditing {
			val = m.routeBuf + "▌"
		}
		line := fmt.Sprintf("%-16s %s", r.name, val)
		if i == m.routeSel {
			b.WriteString(stylePalSelRow.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + styleToolResult.Render(line) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// profileFormView renders the multi-field profile sub-editor.
func (m *Model) profileFormView() string {
	f := m.profileForm
	var b strings.Builder
	title := "edit profile: " + f.name
	if f.isNew {
		title = "new profile"
	}
	hint := "↑/↓ field · enter edit · esc cancel"
	if f.editing {
		hint = "type · enter ok · esc cancel"
	}
	b.WriteString(stylePermTitle.Render(title) + "  " + styleFooter.Render(hint) + "\n")
	for i, fl := range f.fields {
		val := fl.value
		if fl.secret && val != "" && !(f.editing && i == f.sel) {
			val = "••••"
		}
		if f.editing && i == f.sel {
			val = f.buf + "▌"
		}
		line := fmt.Sprintf("%-13s %s", fl.label, val)
		if i == f.sel {
			b.WriteString(stylePalSelRow.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + styleToolResult.Render(line) + "\n")
		}
	}
	b.WriteString("\n") // spacer: set the action apart from the fields
	if f.sel == len(f.fields) {
		b.WriteString("  " + styleBtnSel.Render(" Save "))
	} else {
		b.WriteString("  " + styleBtn.Render(" Save ") + styleFooter.Render("  (Tab)"))
	}
	return b.String()
}

// resumeRows caps how many sessions the picker shows at once.
const resumeRows = 12

// resumeView renders the interactive session picker (↑/↓ select, enter resume).
func (m *Model) resumeView() string {
	var b strings.Builder
	b.WriteString(stylePermTitle.Render("resume a session") + "  " +
		styleFooter.Render("↑/↓ select · enter resume · esc cancel") + "\n")
	start := 0
	if m.resumeSel >= resumeRows {
		start = m.resumeSel - resumeRows + 1
	}
	end := start + resumeRows
	if end > len(m.resumeList) {
		end = len(m.resumeList)
	}
	for i := start; i < end; i++ {
		s := m.resumeList[i]
		title := s.Title
		if title == "" {
			title = styleFooter.Render("(no messages)")
		}
		// Lead with how fresh the session is — "which one was I just in" is the
		// question the picker answers; older sessions keep the absolute stamp.
		when := relAge(s.LastActivity)
		if when == "" {
			when = s.Created.Format("01-02 15:04")
		}
		line := fmt.Sprintf("%-11s %s", when, oneLine(title, max(20, m.width-24)))
		if i == m.resumeSel {
			b.WriteString(stylePalSelRow.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + styleToolResult.Render(line) + "\n")
		}
	}
	if len(m.resumeList) > resumeRows {
		b.WriteString(styleFooter.Render(fmt.Sprintf("  %d/%d", m.resumeSel+1, len(m.resumeList))))
	}
	return strings.TrimRight(b.String(), "\n")
}

// relAge renders a compact relative age ("42s ago", "5m ago", "3h ago",
// "6d ago") for timestamps within the last week; "" otherwise (caller falls
// back to an absolute stamp) — including zero times from legacy metadata.
func relAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return ""
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return ""
}
