package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// suggestHintStyle draws the suggestion faintly, so it reads as an offer under the composer rather
// than as text that is there.
var suggestHintStyle = lipgloss.NewStyle().Faint(true)

// suggestHint is the one dim line shown under the composer: the taking key and the suggestion,
// flattened to a single line and clipped to the width.
func (m *Model) suggestHint(width int) string {
	if m.suggestion == "" {
		return ""
	}
	s := strings.Join(strings.Fields(strings.ReplaceAll(m.suggestion, "\n", " ")), " ")
	if r := []rune(s); width > 4 && len(r) > width-2 {
		s = string(r[:width-3]) + "…"
	}
	return suggestHintStyle.Render("⇥ " + s)
}

// Composer suggestion in the TUI — the same ghost text the web composer shows, on the composer
// profile, learned from this person's past prompts (see app.SuggestPrompt). The terminal has no way
// to draw greyed inline text inside the textarea, so it shows as a dim hint line under the composer
// and Tab takes it, the same shape the web composer settled on.

// suggestTickMsg fires after a pause in typing to launch a suggestion for token tok. A newer
// keystroke bumps the token, so a tick for an old one is ignored.
type suggestTickMsg struct{ tok int }

// suggestReadyMsg carries a completed suggestion back to Update.
type suggestReadyMsg struct {
	tok    int
	prefix string
	text   string
}

const suggestDebounce = 400 * time.Millisecond

// onComposerChanged runs after a key reaches the textarea. If the text changed it drops any standing
// suggestion (it was about older text) and, when there is something worth completing, schedules a
// debounced fetch. A leading "/" is a command for the palette, not an instruction, so it is left
// alone. Returns nil (no command) when there is nothing to schedule.
func (m *Model) onComposerChanged() tea.Cmd {
	v := m.ta.Value()
	if v == m.suggestBase {
		// A caret move or a modifier: the text did not change, but the caret may have. A standing
		// suggestion appends at the end, so once the caret has moved it would land wrong — drop it
		// rather than offer a mis-placed insert. Do not refetch (the text is the same).
		m.suggestion = ""
		return nil
	}
	m.suggestBase = v
	m.suggestion = ""
	if t := strings.TrimSpace(v); t == "" || strings.HasPrefix(t, "/") {
		return nil
	}
	m.suggestTok++
	tok := m.suggestTok
	return tea.Tick(suggestDebounce, func(time.Time) tea.Msg { return suggestTickMsg{tok} })
}

// fetchSuggest runs the model call off the UI goroutine and returns its text as a message.
func (m *Model) fetchSuggest(prefix string, tok int) tea.Cmd {
	sid := m.sid
	return func() tea.Msg {
		out, err := m.app.SuggestPrompt(m.ctx, sid, prefix)
		if err != nil {
			return nil
		}
		return suggestReadyMsg{tok: tok, prefix: prefix, text: strings.TrimSpace(out)}
	}
}

// acceptSuggestion appends the standing suggestion to the composer and clears it. Returns false when
// there is none, so Tab falls through to whatever else it does (history completion, pane focus).
func (m *Model) acceptSuggestion() bool {
	if m.suggestion == "" {
		return false
	}
	m.ta.SetValue(m.ta.Value() + m.suggestion)
	m.suggestBase = m.ta.Value()
	m.suggestion = ""
	m.syncTaViewport()
	return true
}
