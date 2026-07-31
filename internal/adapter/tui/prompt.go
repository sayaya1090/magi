package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/prompt"
)

// fieldState holds the live value of one prompt field.
type fieldState struct {
	buf    string // text/password/number/multiline
	optIdx int    // select
	checks []bool // multiselect
	subIdx int    // multiselect cursor
	boolV  bool   // confirm
}

// promptModel is a standalone form rendering a prompt.Spec (used for startup
// plugin prompts, e.g. SSO). It runs its own tea.Program, separate from the main
// TUI, and returns the collected answers.
type promptModel struct {
	spec     prompt.Spec
	state    []fieldState
	sel      int // selected field; len(fields) == the Submit action
	canceled bool
	width    int
	height   int
}

func newPromptModel(s prompt.Spec) promptModel {
	st := make([]fieldState, len(s.Fields))
	for i, f := range s.Fields {
		switch f.Type {
		case prompt.TypeMultiselect:
			st[i].checks = make([]bool, len(f.Options))
		case prompt.TypeSelect:
			for j, o := range f.Options {
				if o == f.Default {
					st[i].optIdx = j
				}
			}
		case prompt.TypeConfirm:
			st[i].boolV = f.Default == "true" || f.Default == "yes"
		default:
			st[i].buf = f.Default
		}
	}
	m := promptModel{spec: s, state: st, width: 60}
	m.sel = m.firstFocusable(0, 1)
	return m
}

func (m promptModel) Init() tea.Cmd { return nil }

func (m promptModel) focusable(i int) bool {
	if i == len(m.spec.Fields) {
		return true // Submit
	}
	return i >= 0 && i < len(m.spec.Fields) && m.spec.Fields[i].Type != prompt.TypeNote
}

// firstFocusable returns the next focusable index from start moving by dir.
func (m promptModel) firstFocusable(start, dir int) int {
	i := start
	for i >= 0 && i <= len(m.spec.Fields) {
		if m.focusable(i) {
			return i
		}
		i += dir
	}
	// fall back to the submit row
	return len(m.spec.Fields)
}

func (m promptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch e := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = e.Width, e.Height
	case tea.KeyPressMsg:
		return m.key(e)
	}
	return m, nil
}

func (m promptModel) key(e tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch e.String() {
	case "esc", "ctrl+c":
		m.canceled = true
		return m, tea.Quit
	case "tab":
		m.sel = len(m.spec.Fields) // jump to Submit
		return m, nil
	case "up", "ctrl+p":
		// Options are laid out vertically, so ↑ first walks within a select/multiselect;
		// only at the top edge does it leave for the previous field.
		if m.sel < len(m.spec.Fields) {
			f, st := m.spec.Fields[m.sel], &m.state[m.sel]
			switch f.Type {
			case prompt.TypeSelect:
				if st.optIdx > 0 {
					st.optIdx--
					return m, nil
				}
			case prompt.TypeMultiselect:
				if st.subIdx > 0 {
					st.subIdx--
					return m, nil
				}
			}
		}
		if n := m.firstFocusable(m.sel-1, -1); n < m.sel {
			m.sel = n
		}
		return m, nil
	case "down", "ctrl+n":
		if m.sel < len(m.spec.Fields) {
			f, st := m.spec.Fields[m.sel], &m.state[m.sel]
			switch f.Type {
			case prompt.TypeSelect:
				if st.optIdx < len(f.Options)-1 {
					st.optIdx++
					return m, nil
				}
			case prompt.TypeMultiselect:
				if st.subIdx < len(f.Options)-1 {
					st.subIdx++
					return m, nil
				}
			}
		}
		if n := m.firstFocusable(m.sel+1, 1); n > m.sel {
			m.sel = n
		}
		return m, nil
	}
	if m.sel == len(m.spec.Fields) { // on Submit
		if e.String() == "enter" {
			return m, tea.Quit
		}
		return m, nil
	}

	f := m.spec.Fields[m.sel]
	st := &m.state[m.sel]
	switch f.Type {
	case prompt.TypeSelect:
		switch e.String() {
		case "left":
			st.optIdx = wrap(st.optIdx-1, len(f.Options))
		case "right":
			st.optIdx = wrap(st.optIdx+1, len(f.Options))
		case "enter":
			// The highlighted row is already the selection (optIdx follows the cursor),
			// so Enter just confirms and advances. When the next focusable is Submit —
			// i.e. this was the last input field — submit outright rather than parking
			// on the button, so a lone select menu takes one Enter, not two.
			m.sel = m.firstFocusable(m.sel+1, 1)
			if m.sel == len(m.spec.Fields) {
				return m, tea.Quit
			}
		}
	case prompt.TypeMultiselect:
		switch e.String() {
		case "left":
			st.subIdx = wrap(st.subIdx-1, len(f.Options))
		case "right":
			st.subIdx = wrap(st.subIdx+1, len(f.Options))
		case " ", "space", "enter":
			if st.subIdx < len(st.checks) {
				st.checks[st.subIdx] = !st.checks[st.subIdx]
			}
		}
	case prompt.TypeConfirm:
		switch e.String() {
		case "left", "right", " ", "space":
			st.boolV = !st.boolV
		case "y", "Y":
			st.boolV = true
		case "n", "N":
			st.boolV = false
		}
	default: // text/password/number/multiline
		switch e.String() {
		case "backspace":
			if n := len(st.buf); n > 0 {
				st.buf = st.buf[:n-1]
			}
		case "enter":
			if f.Type == prompt.TypeMultiline {
				st.buf += "\n"
			}
		default:
			if t := e.Key().Text; t != "" {
				if f.Type == prompt.TypeNumber && !isNumeric(t) {
					break
				}
				st.buf += t
			}
		}
	}
	return m, nil
}

func (m promptModel) answers() map[string]any {
	out := map[string]any{}
	for i, f := range m.spec.Fields {
		if f.Type == prompt.TypeNote || f.Name == "" {
			continue
		}
		st := m.state[i]
		switch f.Type {
		case prompt.TypeSelect:
			if st.optIdx < len(f.Options) {
				out[f.Name] = f.Options[st.optIdx]
			}
		case prompt.TypeMultiselect:
			var picked []string
			for j, on := range st.checks {
				if on {
					picked = append(picked, f.Options[j])
				}
			}
			out[f.Name] = picked
		case prompt.TypeConfirm:
			out[f.Name] = st.boolV
		default:
			out[f.Name] = st.buf
		}
	}
	return out
}

// promptWidth is the layout width used before the terminal has reported one.
const promptWidth = 60

// fieldRows renders one field as the lines it occupies, each clipped to w. The label column is
// fixed so the values line up; when something has to go it is the value's tail, never the label —
// a form whose labels are cut is one the user cannot answer. Option lists render vertically, one
// option per line, so long option labels stay readable and Submit falls below them.
func (m promptModel) fieldRows(i, w int) []string {
	f, st, sel := m.spec.Fields[i], m.state[i], i == m.sel
	clip := func(s string) string {
		if w > 0 && ansi.StringWidth(s) > w {
			return ansi.Truncate(s, w, "…")
		}
		return s
	}

	if f.Type == prompt.TypeSelect || f.Type == prompt.TypeMultiselect {
		var out []string
		if f.Label != "" {
			out = append(out, clip("  "+styleFooter.Render(f.Label)))
		}
		for j, o := range f.Options {
			var marker string
			var cur bool
			if f.Type == prompt.TypeSelect {
				marker = "○"
				if j == st.optIdx { // for select the highlighted row is the selection
					marker = "●"
				}
				cur = sel && j == st.optIdx
			} else {
				marker = "[ ]"
				if st.checks[j] {
					marker = "[x]"
				}
				cur = sel && j == st.subIdx
			}
			row := marker + " " + o
			if cur {
				out = append(out, clip(stylePalName.Render("  › ")+stylePalSelRow.Render(" "+row+" ")))
			} else {
				out = append(out, clip("    "+styleToolResult.Render(row)))
			}
		}
		return out
	}

	var val string
	switch f.Type {
	case prompt.TypeNote:
		return []string{clip(styleFooter.Render("  " + f.Label))}
	case prompt.TypePassword:
		val = strings.Repeat("•", len([]rune(st.buf)))
		if sel {
			val += "▌"
		}
	case prompt.TypeConfirm:
		yes, no := "yes", "no"
		if st.boolV {
			yes = stylePalSelRow.Render(" yes ")
		} else {
			no = stylePalSelRow.Render(" no ")
		}
		val = yes + "  " + no
	default:
		val = st.buf
		if sel {
			val += "▌"
		}
	}
	label := f.Label
	if label == "" {
		label = f.Name
	}
	line := fmt.Sprintf("%-14s %s", label, val)
	if sel {
		return []string{clip(stylePalName.Render("› ") + line)}
	}
	return []string{clip("  " + line)}
}

// View lays the form out inside the terminal it was given. This is a full-screen surface, so a row
// wider than the terminal is not one clipped line — the shell wraps it and every row below shifts,
// which on a login form can carry Submit off the bottom. Both axes are therefore bounded: rows are
// clipped to the width, and when the form is taller than the screen the banner goes first and then
// the fields are paged around the selection, with an "n/N" line so a hidden field is never
// silently absent.
func (m promptModel) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.Content = m.body()
	return v
}

func (m promptModel) body() string {
	w := m.width
	if w <= 0 {
		w = promptWidth
	}
	blocks := make([][]string, len(m.spec.Fields))
	for i := range m.spec.Fields {
		blocks[i] = m.fieldRows(i, w)
	}

	// The hint rides along only while it fits: on a narrow terminal the button is what the user
	// needs to see, and a wrapped hint pushes the button off its own line.
	submit := "  " + styleBtnSel.Render(" Submit ")
	if m.sel != len(m.spec.Fields) {
		submit = "  " + styleBtn.Render(" Submit ") + styleFooter.Render("  ↑/↓ move · Tab submit · Esc cancel")
		if ansi.StringWidth(submit) > w {
			submit = "  " + styleBtn.Render(" Submit ")
		}
	}

	// Identity banner: the same wordmark as the startup splash, so a plugin login screen reads as
	// "the startup page with a login form attached", not a bare form. It is the first thing dropped
	// when the screen is tight — it names the program, the form is what is being asked.
	logo := logoBlock()
	fits := lipgloss.Width(logo) <= w

	draw := func(banner bool, keep int) string {
		var b strings.Builder
		if banner {
			b.WriteString(lipgloss.PlaceHorizontal(max(1, w), lipgloss.Center, logo) + "\n\n")
		}
		if m.spec.Title != "" {
			b.WriteString(ansi.Truncate(stylePermTitle.Render(m.spec.Title), w, "…") + "\n\n")
		}
		start := 0
		if keep < len(blocks) {
			start = min(max(0, min(m.sel, len(blocks)-1)-keep/2), len(blocks)-keep)
		}
		for i := start; i < start+keep; i++ {
			for _, row := range blocks[i] {
				b.WriteString(row + "\n")
			}
		}
		if keep < len(blocks) {
			b.WriteString(styleFooter.Render(fmt.Sprintf("  %d/%d fields", min(m.sel, len(blocks)-1)+1, len(blocks))) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(submit)
		return b.String()
	}

	for _, banner := range []bool{fits, false} {
		for keep := len(blocks); keep >= 0; keep-- {
			out := draw(banner, keep)
			if m.height <= 0 || lipgloss.Height(out) <= m.height {
				return out
			}
			if keep == 0 {
				break
			}
		}
	}
	// Nothing fits: the selected field alone, which is the one being answered.
	if len(blocks) == 0 {
		return submit
	}
	return strings.Join(blocks[min(m.sel, len(blocks)-1)], "\n") + "\n" + submit
}

// RunPrompt renders the spec as a standalone form and returns the answers. It
// errors when there is no interactive terminal (the caller falls back).
func RunPrompt(s prompt.Spec) (map[string]any, error) {
	if !isInteractive() {
		return nil, errors.New("no interactive terminal for prompt")
	}
	applyTheme(true)
	res, err := tea.NewProgram(newPromptModel(s)).Run()
	if err != nil {
		return nil, err
	}
	fm, _ := res.(promptModel)
	if fm.canceled {
		return nil, errors.New("prompt canceled")
	}
	return fm.answers(), nil
}

func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func wrap(i, n int) int {
	if n == 0 {
		return 0
	}
	if i < 0 {
		return n - 1
	}
	if i >= n {
		return 0
	}
	return i
}

func isNumeric(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' {
			return false
		}
	}
	return true
}
