package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/app"
)

// sessionRouteRow is the editor's first row: the session's default model.
const sessionRouteRow = "(session)"

// modelSugMax caps how many model suggestions the session-row box shows at once.
const modelSugMax = 8

// modelCatalogMsg carries the gateway's live model catalog back to the Update
// loop, off the goroutine that fetched it (so opening /route never blocks).
type modelCatalogMsg struct{ models []string }

// fetchModelsCmd asks the default backend for its model catalog in the background.
// Errors/empty results collapse to an empty slice — the suggest box then falls
// back to configured profile models (and ultimately free text). A short timeout
// keeps a slow or unreachable gateway from stalling the prefetch.
func (m *Model) fetchModelsCmd() tea.Cmd {
	a := m.app
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ids, err := a.ListModels(ctx)
		if err != nil {
			ids = nil
		}
		return modelCatalogMsg{models: ids}
	}
}

// modelSuggestions returns the session-row suggest list: configured profile
// models first, then the gateway's live catalog, de-duplicated and filtered by
// the current routeBuf (case-insensitive substring), capped at modelSugMax. An
// empty routeBuf shows the merged list head. Profiles alone populate it when the
// gateway is unreachable, so the box stays useful without a live catalog.
func (m *Model) modelSuggestions() []string {
	seen := map[string]bool{}
	merged := make([]string, 0, len(m.modelCatalog)+4)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		merged = append(merged, s)
	}
	for _, p := range m.app.Profiles() {
		add(p.Model)
	}
	for _, id := range m.modelCatalog {
		add(id)
	}
	q := strings.ToLower(strings.TrimSpace(m.routeBuf))
	out := make([]string, 0, modelSugMax)
	for _, s := range merged {
		if q != "" && !strings.Contains(strings.ToLower(s), q) {
			continue
		}
		out = append(out, s)
		if len(out) >= modelSugMax {
			break
		}
	}
	return out
}

// clampModelSug pins modelSugSel into [-1, n-1]: -1 means "free text (routeBuf)",
// 0..n-1 selects a suggestion. A stale index (the list shrank as the user typed)
// collapses back to free text so the wrap arithmetic stays in bounds.
func (m *Model) clampModelSug(n int) {
	if n <= 0 || m.modelSugSel < -1 || m.modelSugSel >= n {
		m.modelSugSel = -1
	}
}

type routeRowKind int

const (
	rowSession routeRowKind = iota
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
// routing, the defined profiles, and an "+ add profile" row. It returns a
// background command that prefetches the gateway's model catalog on first open
// (nil thereafter — the catalog is cached for the session).
func (m *Model) openRouteEditor() tea.Cmd {
	m.profileForm = nil
	m.refreshRouteList()
	m.routeSel, m.routing, m.routeEditing, m.routeBuf = 0, true, false, ""
	m.modelSugSel = -1
	if m.catalogLoaded {
		return nil
	}
	return m.fetchModelsCmd()
}

func (m *Model) refreshRouteList() {
	rows := []routeRow{{kind: rowSession, name: sessionRouteRow, value: m.model}}
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
		editingSession := m.routeList[m.routeSel].kind == rowSession
		switch msg.String() {
		case "enter":
			val := strings.TrimSpace(m.routeBuf)
			// A highlighted suggestion wins over the typed buffer, so ↑/↓+enter applies a real
			// catalog model directly.
			if editingSession {
				if sugs := m.modelSuggestions(); m.modelSugSel >= 0 && m.modelSugSel < len(sugs) {
					val = sugs[m.modelSugSel]
				}
				if val != "" {
					m.app.SetModel(m.sid, val)
					m.model = val
				}
			}
			m.refreshRouteList()
			m.routeEditing = false
			m.refresh()
			return nil, true
		case "esc":
			m.routeEditing = false
			m.refresh()
			return nil, true
		case "up", "down":
			// Session row only: circular move through the suggest box. Entering
			// navigation from free text (-1) lands on an end.
			if editingSession {
				if sugs := m.modelSuggestions(); len(sugs) > 0 {
					n := len(sugs)
					m.clampModelSug(n)
					switch {
					case m.modelSugSel < 0:
						if msg.String() == "up" {
							m.modelSugSel = n - 1
						} else {
							m.modelSugSel = 0
						}
					case msg.String() == "up":
						m.modelSugSel = (m.modelSugSel - 1 + n) % n
					default:
						m.modelSugSel = (m.modelSugSel + 1) % n
					}
					m.refresh()
				}
			}
			return nil, true
		case "tab":
			// Session row only: accept the highlighted (or first) suggestion into
			// the buffer, like shell completion, then return to free-text editing.
			if editingSession {
				if sugs := m.modelSuggestions(); len(sugs) > 0 {
					idx := m.modelSugSel
					if idx < 0 || idx >= len(sugs) {
						idx = 0
					}
					m.routeBuf = sugs[idx]
					m.modelSugSel = -1
					m.refresh()
				}
			}
			return nil, true
		case "backspace":
			if n := len(m.routeBuf); n > 0 {
				m.routeBuf = m.routeBuf[:n-1]
			}
			m.routePickIdx = -1 // back to free text
			m.modelSugSel = -1  // re-filter from the typed buffer
			m.refresh()
			return nil, true
		}
		if t := msg.Key().Text; t != "" {
			m.routeBuf += t
			m.routePickIdx = -1 // typing overrides the profile picker
			m.modelSugSel = -1  // typing re-filters and drops any highlight
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
		case rowSession:
			m.routeEditing = true
			m.routeBuf = ""
			m.routePickIdx = -1
			m.modelSugSel = -1
		case rowProfile:
			m.openProfileForm(strings.TrimPrefix(row.name, "profile:"))
		case rowAddProfile:
			m.openProfileForm("")
		}
	case "esc":
		m.routing = false
	}
	m.refresh()
	return nil, true
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
	case "esc":
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
	hint := "↑/↓ select · enter edit/open · esc close"
	if m.routeEditing {
		if m.routeList[m.routeSel].kind == rowSession {
			// A session always has a model, so an empty buffer is a no-op here
			// (unlike agent rows, where empty clears the override).
			hint = "type to filter · ↑/↓ pick · tab fill · enter apply · esc"
		} else {
			hint = "type value · ←/→ pick profile · enter apply · empty clears · esc"
		}
	}
	// The title plus its hint runs about 75 cells; a split pane or a phone-sized ssh window is
	// narrower than that, and one over-wide row in a vertically joined frame pads every other row
	// to match — the whole screen goes wider than the terminal and the shell wraps it. The hint is
	// the part that is decoration, so it goes first; the title is what says which editor this is.
	head := stylePermTitle.Render("models & routing") + "  " + styleFooter.Render(hint)
	if m.width > 0 && lipgloss.Width(head) > m.width {
		head = ansi.Truncate(stylePermTitle.Render("models & routing"), m.width, "")
	}
	// Render each row on its own, then decide how many of them there is room for. The header
	// above was bounded in width for a split pane or a phone-sized ssh window; its HEIGHT was
	// never bounded at all, and this editor lists every agent and every profile. The walk found
	// it at ten rows: eleven rows of chrome on a ten-row screen, so on an alt screen the title
	// and the selected row were both above the display. Every other surface drawn in this slot —
	// the palette, both modals, the profile form next door — windows itself against modalRoom.
	rows := make([]string, len(m.routeList))
	sepDrawn := false
	for i, r := range m.routeList {
		var rb strings.Builder
		// Set the profiles section (profile rows + add button) apart from the
		// session/agent rows with a blank line and a dim header. It rides on the row that
		// starts the section, so a window that opens mid-list carries it only when it should.
		if !sepDrawn && (r.kind == rowProfile || r.kind == rowAddProfile) {
			rb.WriteString("\n" + styleFooter.Render("backends (profiles)") + "\n")
			sepDrawn = true
		}
		if r.kind == rowAddProfile {
			btn := " + add profile "
			if i == m.routeSel {
				rb.WriteString("  " + styleBtnSel.Render(btn))
			} else {
				rb.WriteString("  " + styleBtn.Render(btn))
			}
			rows[i] = rb.String()
			continue
		}
		val := r.value
		if i == m.routeSel && m.routeEditing {
			val = m.routeBuf + "▌"
		}
		line := fmt.Sprintf("%-16s %s", r.name, val)
		if i == m.routeSel {
			rb.WriteString(stylePalSelRow.Render("› " + line))
		} else {
			rb.WriteString("  " + styleToolResult.Render(line))
		}
		// Under the session row while editing it, show the model suggest box.
		if i == m.routeSel && m.routeEditing && r.kind == rowSession {
			rb.WriteString("\n" + strings.TrimRight(m.modelSuggestBox(), "\n"))
		}
		rows[i] = rb.String()
	}

	draw := func(keep int) string {
		var t strings.Builder
		t.WriteString(head + "\n")
		start := 0
		if keep < len(rows) {
			start = min(max(0, m.routeSel-keep/2), len(rows)-keep)
		}
		for i := start; i < start+keep; i++ {
			t.WriteString(rows[i] + "\n")
		}
		if keep < len(rows) {
			// The row being edited is in the window; the count says what is outside it, so a
			// list that stops at the fourth agent does not read as a magi with four agents.
			t.WriteString(styleFooter.Render(fmt.Sprintf("  %d/%d rows (↑/↓ to reach them)",
				m.routeSel+1, len(rows))) + "\n")
		}
		return strings.TrimRight(t.String(), "\n")
	}

	room := m.modalRoom()
	for keep := len(rows); keep > 0; keep-- {
		out := draw(keep)
		if m.height <= 0 || lipgloss.Height(out) <= room {
			return out
		}
	}
	// Nothing fits: the selected row alone, which is the one being worked on.
	if len(rows) == 0 {
		return head
	}
	return strings.TrimRight(head+"\n"+rows[min(m.routeSel, len(rows)-1)], "\n")
}

// modelSuggestBox renders the session-row model suggest list: the merged,
// filtered candidates with the highlighted one marked. While the catalog is
// still loading and no profile models are available it shows a dim hint; once
// loaded with nothing matching it renders nothing (free-text entry, unchanged).
func (m *Model) modelSuggestBox() string {
	sugs := m.modelSuggestions()
	if len(sugs) == 0 {
		if !m.catalogLoaded {
			return "    " + styleFooter.Render("loading models…") + "\n"
		}
		return ""
	}
	// A gateway catalog's ids are long and path-like ("internal-gateway/anthropic/claude-…-v1:0"),
	// so they are elided in the MIDDLE: the tail is what distinguishes one revision of a model from
	// the next, and head-truncation would leave a column of rows that all read the same.
	var b strings.Builder
	for i, s := range sugs {
		if i == m.modelSugSel {
			b.WriteString("    " + stylePalSelRow.Render("› "+elideMiddle(s, m.width-6)) + "\n")
		} else {
			b.WriteString("      " + styleToolResult.Render(elideMiddle(s, m.width-6)) + "\n")
		}
	}
	return b.String()
}

// elideMiddle shortens s to w cells by cutting its MIDDLE, keeping both ends. For an identifier
// whose head is a shared prefix and whose tail is the discriminator — a model id, a path — cutting
// either end alone makes different values render identically. w <= 0 leaves s untouched (an
// unmeasured terminal is not a narrow one).
func elideMiddle(s string, w int) string {
	if w <= 0 || lipgloss.Width(s) <= w {
		return s
	}
	if w <= 3 {
		return ansi.Truncate(s, w, "")
	}
	keep := w - 1 // the ellipsis
	head := keep / 2
	return ansi.Truncate(s, head, "") + "…" + ansi.TruncateLeft(s, lipgloss.Width(s)-(keep-head), "")
}

// profileFormView renders the multi-field profile sub-editor.
//
// It fits the screen in both directions, because the values it shows are routinely longer than a
// narrow terminal — a gateway base_url, a model id — and it is drawn where the palette would be,
// in a frame that is joined vertically: one over-wide row pads every other row to match, so the
// whole screen goes wider than the terminal and the shell wraps it. Sideways it drops the keyboard
// hint first and then truncates the value, keeping the label so the row still says which field it
// is. Vertically it pages the fields around the selection, the way the resume picker does, rather
// than pushing the input box off the bottom of a short window.
func (m *Model) profileFormView() string {
	f := m.profileForm
	title := "edit profile: " + f.name
	if f.isNew {
		title = "new profile"
	}
	hint := "↑/↓ field · enter edit · esc cancel"
	if f.editing {
		hint = "type · enter ok · esc cancel"
	}
	head := stylePermTitle.Render(title) + "  " + styleFooter.Render(hint)
	if m.width > 0 && lipgloss.Width(head) > m.width {
		head = ansi.Truncate(stylePermTitle.Render(title), m.width, "")
	}

	// A field row, clipped to the terminal. The label column is fixed so the values line up; when
	// something has to go it is the value's tail, never the label.
	row := func(i int) string {
		fl := f.fields[i]
		val := fl.value
		if fl.secret && val != "" && !(f.editing && i == f.sel) {
			val = "••••"
		}
		if f.editing && i == f.sel {
			val = f.buf + "▌"
		}
		line := fmt.Sprintf("%-13s %s", fl.label, val)
		if m.width > 2 {
			line = ansi.Truncate(line, m.width-2, "…")
		}
		if i == f.sel {
			return stylePalSelRow.Render("› " + line)
		}
		return "  " + styleToolResult.Render(line)
	}
	save := func() string {
		if f.sel == len(f.fields) {
			return "  " + styleBtnSel.Render(" Save ")
		}
		return "  " + styleBtn.Render(" Save ") + styleFooter.Render("  (Tab)")
	}
	// draw renders a window of `keep` fields centred on the selection. keep == len(fields) is the
	// whole form; smaller windows carry an "n/N" line so a hidden field is never silently absent.
	draw := func(keep int) string {
		var b strings.Builder
		b.WriteString(head + "\n")
		start := 0
		if keep < len(f.fields) {
			start = min(max(0, f.sel-keep/2), len(f.fields)-keep)
		}
		for i := start; i < start+keep; i++ {
			b.WriteString(row(i) + "\n")
		}
		if keep < len(f.fields) {
			b.WriteString(styleFooter.Render(fmt.Sprintf("  %d/%d fields", f.sel+1, len(f.fields))) + "\n")
		}
		b.WriteString("\n") // spacer: set the action apart from the fields
		b.WriteString(save())
		return b.String()
	}

	room := m.modalRoom()
	for keep := len(f.fields); keep > 0; keep-- {
		out := draw(keep)
		if m.height <= 0 || lipgloss.Height(out) <= room {
			return out
		}
	}
	// Nothing fits: the selected field alone, which is the one being worked on.
	return strings.TrimRight(head+"\n"+row(min(f.sel, len(f.fields)-1)), "\n")
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
