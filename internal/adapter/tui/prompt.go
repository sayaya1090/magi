package tui

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

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
		m.width = e.Width
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
			// so Enter just confirms and advances — landing on Submit when last.
			m.sel = m.firstFocusable(m.sel+1, 1)
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

func (m promptModel) View() tea.View {
	var b strings.Builder
	// Identity banner: the same wordmark as the startup splash, so a plugin login
	// screen reads as "the startup page with a login form attached", not a bare form.
	b.WriteString(lipgloss.PlaceHorizontal(max(1, m.width), lipgloss.Center, logoBlock()) + "\n\n")
	if m.spec.Title != "" {
		b.WriteString(stylePermTitle.Render(m.spec.Title) + "\n\n")
	}
	for i, f := range m.spec.Fields {
		st := m.state[i]
		sel := i == m.sel
		// Option lists render vertically — one option per line — so long labels stay
		// readable and the Submit button falls naturally below them.
		if f.Type == prompt.TypeSelect || f.Type == prompt.TypeMultiselect {
			if f.Label != "" {
				b.WriteString("  " + styleFooter.Render(f.Label) + "\n")
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
					b.WriteString(stylePalName.Render("  › ") + stylePalSelRow.Render(" "+row+" ") + "\n")
				} else {
					b.WriteString("    " + styleToolResult.Render(row) + "\n")
				}
			}
			continue
		}

		var val string
		switch f.Type {
		case prompt.TypeNote:
			b.WriteString(styleFooter.Render("  "+f.Label) + "\n")
			continue
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
			b.WriteString(stylePalName.Render("› ") + line + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n")
	if m.sel == len(m.spec.Fields) {
		b.WriteString("  " + styleBtnSel.Render(" Submit "))
	} else {
		b.WriteString("  " + styleBtn.Render(" Submit ") + styleFooter.Render("  ↑/↓ move · Tab submit · Esc cancel"))
	}
	var v tea.View
	v.AltScreen = true
	v.Content = b.String()
	return v
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
