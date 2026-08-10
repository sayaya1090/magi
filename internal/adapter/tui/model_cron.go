package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/cron"
	"github.com/sayaya1090/magi/internal/port"
)

// The /cron list: what this workspace does when nobody is watching.
//
// It is here because the agent can now schedule its own work, and the answer to "what did you leave
// running?" cannot be "read the config file". Every job, when it next fires, and the words it will
// be asked — on one screen, from the terminal the person is already in.
//
// # Two single-line fields, not an editor
//
// A prompt can be a paragraph, and a terminal list is a poor place to write one. The fields hold
// any length and show the end of what is typed; the hint says where to go for something longer. The
// alternative — a full text area inside a list overlay — is a lot of machinery for the case that is
// better served by the file this writes to anyway.

// cronEditField names which of the two fields is taking keys.
type cronEditField int

const (
	cronFieldName cronEditField = iota
	cronFieldSchedule
	cronFieldPrompt
)

// openCron shows the list, rebuilt from disk each time so an edit made anywhere else is visible.
func (m *Model) openCron() tea.Cmd {
	m.refreshCronList()
	m.cronSel, m.cronning, m.cronNote = 0, true, ""
	m.cronEditing, m.cronConfirm = false, false
	return nil
}

func (m *Model) refreshCronList() {
	m.cronList = m.app.ScheduledJobs(m.workdir)
	if m.cronSel >= len(m.cronList) {
		m.cronSel = max(0, len(m.cronList)-1)
	}
}

func (m *Model) selectedCron() (app.ScheduledJobInfo, bool) {
	if m.cronSel < 0 || m.cronSel >= len(m.cronList) {
		return app.ScheduledJobInfo{}, false
	}
	return m.cronList[m.cronSel], true
}

// handleCronKey drives the list. Returns false when the key is not ours.
func (m *Model) handleCronKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.cronEditing {
		return m.handleCronEditKey(msg)
	}
	if m.cronConfirm {
		return m.handleCronConfirmKey(msg)
	}
	switch msg.String() {
	case "esc", "q":
		m.cronning = false
		return nil, true
	case "up", "k":
		if m.cronSel > 0 {
			m.cronSel--
		}
		return nil, true
	case "down", "j":
		if m.cronSel < len(m.cronList)-1 {
			m.cronSel++
		}
		return nil, true
	case "n":
		m.startCronEdit(app.ScheduledJobInfo{}, true)
		return nil, true
	case "e", "enter":
		if j, ok := m.selectedCron(); ok {
			m.startCronEdit(j, false)
		}
		return nil, true
	case "d":
		// Confirmed, because this is the one action here that cannot be undone from this screen.
		if _, ok := m.selectedCron(); ok {
			m.cronConfirm = true
		}
		return nil, true
	// Both spellings of the space bar: this terminal library reports it either way, and matching
	// only one is why the box would not tick (see /subagents, which learned it first).
	case " ", "space", "x":
		return m.toggleCronRow(), true
	}
	return nil, false
}

func (m *Model) handleCronConfirmKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "y", "enter":
		m.cronConfirm = false
		j, ok := m.selectedCron()
		if !ok {
			return nil, true
		}
		m.applyCron(port.ScheduleChange{Action: "remove", Name: j.Name})
		return nil, true
	default:
		m.cronConfirm = false
		return nil, true
	}
}

// startCronEdit opens the editor over a job, or over a blank one.
func (m *Model) startCronEdit(j app.ScheduledJobInfo, fresh bool) {
	m.cronEditing, m.cronFresh = true, fresh
	m.cronName, m.cronSchedBuf, m.cronPromptBuf = j.Name, j.Schedule, j.Prompt
	m.cronNote = ""
	// A new job needs its name typed; an existing one is addressed by the name it has, and letting
	// that be edited here would be a rename — which is a delete and a create, and is not what the
	// keystroke looks like it does.
	if fresh {
		m.cronField = cronFieldName
	} else {
		m.cronField = cronFieldSchedule
	}
}

func (m *Model) cronBuf() *string {
	switch m.cronField {
	case cronFieldName:
		return &m.cronName
	case cronFieldSchedule:
		return &m.cronSchedBuf
	default:
		return &m.cronPromptBuf
	}
}

func (m *Model) handleCronEditKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	buf := m.cronBuf()
	switch msg.String() {
	case "esc":
		m.cronEditing, m.cronNote = false, ""
		return nil, true
	case "tab", "down":
		m.cronField = m.nextCronField(1)
		return nil, true
	case "shift+tab", "up":
		m.cronField = m.nextCronField(-1)
		return nil, true
	case "enter":
		m.cronEditing = false
		m.applyCron(port.ScheduleChange{
			Action: "set", Name: m.cronName, Schedule: m.cronSchedBuf, Prompt: m.cronPromptBuf,
		})
		return nil, true
	case "backspace":
		if r := []rune(*buf); len(r) > 0 {
			*buf = string(r[:len(r)-1])
		}
		return nil, true
	}
	// Key().Text is the character the key produced, which is how every other editor in this package
	// reads typed input. Testing the key NAME for length 1 spells the space bar "space" and drops
	// anything multi-byte — written that way here first, and the field silently refused spaces, so
	// "check the logs" went in as "checkthelogs". /subagents has the same note for the same reason.
	if t := msg.Key().Text; t != "" {
		*buf += t
	}
	return nil, true // the editor swallows everything else rather than leaking keys to the composer
}

// nextCronField moves between the fields, skipping the name on an existing job.
func (m *Model) nextCronField(d int) cronEditField {
	fields := []cronEditField{cronFieldSchedule, cronFieldPrompt}
	if m.cronFresh {
		fields = []cronEditField{cronFieldName, cronFieldSchedule, cronFieldPrompt}
	}
	for i, f := range fields {
		if f == m.cronField {
			return fields[(i+len(fields)+d)%len(fields)]
		}
	}
	return fields[0]
}

// toggleCronRow switches the selected job on or off.
func (m *Model) toggleCronRow() tea.Cmd {
	j, ok := m.selectedCron()
	if !ok {
		return nil
	}
	on := !j.Enabled
	m.applyCron(port.ScheduleChange{Action: "set", Name: j.Name, Enabled: &on})
	return nil
}

// applyCron makes one change and keeps whatever the engine said about it.
//
// The answer is shown rather than swallowed. Every refusal from EditSchedule is a sentence
// explaining what was wrong with the schedule — the same sentences the agent gets — and a screen
// that dropped them would leave a person retyping a crontab line with no idea which field was bad.
func (m *Model) applyCron(c port.ScheduleChange) {
	out, err := m.app.EditSchedule(m.workdir, c)
	if err != nil {
		m.cronNote = "could not write it: " + err.Error()
	} else {
		m.cronNote = oneLine(out, 100000)
	}
	m.refreshCronList()
	// Land on the job just touched, so a person who added one sees it selected rather than having
	// the selection sit wherever the old ordering left it.
	for i, j := range m.cronList {
		if j.Name == c.Name {
			m.cronSel = i
			break
		}
	}
}

func (m *Model) cronView() string {
	hint := "↑/↓ select · space on/off · e edit · n new · d delete · esc close"
	switch {
	case m.cronEditing:
		hint = "tab next field · enter save · esc cancel"
	case m.cronConfirm:
		hint = "y delete · any other key cancel"
	}
	head := stylePermTitle.Render("scheduled work") + "  " + styleFooter.Render(hint)
	if m.width > 0 && lipgloss.Width(head) > m.width {
		head = ansi.Truncate(stylePermTitle.Render("scheduled work"), m.width, "")
	}

	var b strings.Builder
	b.WriteString(head)
	inner := max(20, m.bodyWidth()-4)

	if m.cronEditing {
		b.WriteString("\n" + m.cronEditorView(inner))
	} else if len(m.cronList) == 0 {
		b.WriteString("\n" + clipRow(styleFooter.Render(
			"  nothing scheduled — n to add one, or ask magi to schedule something"), m.bodyWidth()))
	}

	// The list is replaced by the editor rather than drawn under it: two jobs' worth of rows behind
	// three fields is noise, and the fields are what the keys are going to.
	for i, j := range m.cronList {
		if m.cronEditing {
			break
		}
		marker := "  "
		if i == m.cronSel {
			marker = "› "
		}
		box := "[ ]"
		if j.Enabled {
			box = "[✓]"
		}
		row := marker + box + " " + styleToolName.Render(j.Name) + "  " + styleToolArgs.Render(j.Schedule)
		row += "  " + styleFooter.Render(cronWhen(j))
		if i == m.cronSel && m.cronConfirm {
			row += "  " + stylePermTitle.Render("delete? y/n")
		}
		b.WriteString("\n" + clipRow(row, m.bodyWidth()))
		if p := strings.TrimSpace(j.Prompt); p != "" {
			for _, ln := range wrapLines(oneLine(p, 100000), max(8, inner-8)) {
				b.WriteString("\n" + clipRow("      "+styleToolResult.Render(strings.TrimRight(ln, " ")), m.bodyWidth()))
			}
		}
	}
	if m.cronNote != "" {
		b.WriteString("\n" + clipRow(styleFooter.Render("  "+m.cronNote), m.bodyWidth()))
	}
	return b.String()
}

// cronWhen says when a job next runs, or why it never will.
//
// A job that can never run is the one a listing MUST mark: it is switched on, it looks ordinary,
// and nothing else will ever mention it again.
func cronWhen(j app.ScheduledJobInfo) string {
	switch {
	case j.Problem != "":
		return "never runs — " + j.Problem
	case !j.Enabled:
		return "off"
	case j.Next.IsZero():
		return "never runs"
	default:
		return "next " + j.Next.Format("Mon 15:04") + relativeWhen(j.Next)
	}
}

// relativeWhen adds "(in 7h)" to an absolute time. Both, because "Tue 03:00" answers "is that the
// time I meant" and "in 7h" answers "is that soon", and a person checking a job they just wrote is
// asking the second one.
func relativeWhen(t time.Time) string {
	d := time.Until(t)
	switch {
	case d < 0:
		return ""
	case d < time.Minute:
		return " (in under a minute)"
	case d < time.Hour:
		return fmt.Sprintf(" (in %dm)", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf(" (in %dh)", int(d.Hours()))
	default:
		return fmt.Sprintf(" (in %dd)", int(d.Hours()/24))
	}
}

func (m *Model) cronEditorView(inner int) string {
	field := func(f cronEditField, label, val, placeholder string) string {
		shown := val
		if shown == "" && m.cronField != f {
			shown = styleFooter.Render(placeholder)
		}
		// The end of a long value, with the cursor on it: a single line cannot show a paragraph, and
		// the end is where the typing is.
		if w := max(8, inner-len(label)-4); len([]rune(shown)) > w {
			shown = "…" + string([]rune(shown)[len([]rune(shown))-w+1:])
		}
		if m.cronField == f {
			shown = styleToolArgs.Render(shown) + "▌"
		}
		return "  " + styleToolName.Render(label) + " " + shown
	}
	var rows []string
	if m.cronFresh {
		rows = append(rows, field(cronFieldName, "name    ", m.cronName, "nightly-audit"))
	} else {
		rows = append(rows, "  "+styleToolName.Render("name    ")+" "+m.cronName)
	}
	rows = append(rows,
		field(cronFieldSchedule, "when    ", m.cronSchedBuf, "0 3 * * *   or   @daily"),
		field(cronFieldPrompt, "ask     ", m.cronPromptBuf, "what to ask when it runs"),
	)
	// What the schedule being typed actually means, while it is being typed. A crontab line is not
	// readable at a glance, and the moment to find out that "0 3 * *" has four fields is now.
	rows = append(rows, "  "+styleFooter.Render(explainSchedule(m.cronSchedBuf)))
	for i := range rows {
		rows[i] = clipRow(rows[i], m.bodyWidth())
	}
	return strings.Join(rows, "\n")
}

// explainSchedule turns the line being typed into when it would next fire, or into the reason it
// will not parse.
func explainSchedule(spec string) string {
	if strings.TrimSpace(spec) == "" {
		return "five fields (minute hour day month weekday), or @daily @hourly @weekly @monthly"
	}
	sch, err := cron.Parse(spec)
	if err != nil {
		return err.Error()
	}
	next := sch.Next(time.Now())
	if next.IsZero() {
		return "parses, but never comes round — nothing would ever run"
	}
	return "next " + next.Format("Mon 2 Jan 15:04") + relativeWhen(next)
}
