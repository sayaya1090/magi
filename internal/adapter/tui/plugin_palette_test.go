package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/port"
)

// squash strips the ANSI, the box glyphs and every run of whitespace, so a row can be looked for
// without caring where the frame wrapped it.
func squash(s string) string {
	s = ansiSeq.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		switch r {
		case '│', '╭', '╮', '╰', '╯', '─', ' ', '\t', '\n':
			return -1
		}
		return r
	}, s)
	return s
}

// pluginCmd is a plugin-contributed slash command with whatever name and description its author
// wrote. Everything else the palette draws is authored in this repo; these two strings are not.
type pluginCmd struct{ name, desc string }

func (c pluginCmd) Name() string           { return c.name }
func (c pluginCmd) Description() string    { return c.desc }
func (c pluginCmd) Execute([]string) error { return nil }

type cmdSourceWith struct{ cmds []port.PluginCommand }

func (s *cmdSourceWith) PluginCommands() []port.PluginCommand           { return s.cmds }
func (s *cmdSourceWith) DispatchCommand(string, []string) (bool, error) { return false, nil }
func (s *cmdSourceWith) TakeUIEffects() []string                        { return nil }

// The palette's rows come from slashCommands, which this repo writes — except the plugin ones,
// which come from a Lua file somebody else wrote. Nothing between that file and the row sanitizes
// it, and the palette is a surface whose row accounting has already been wrong twice.
//
// So: a name and a description shaped the ways a plugin author's really are — long, wide, wrapped
// over a line, and carrying an escape sequence — and the frame still has to fit its terminal.
func TestAPluginCommandCannotBreakThePalette(t *testing.T) {
	applyTheme(true)
	for _, c := range []pluginCmd{
		{"login", "sign in to the internal gateway"},
		{"averyveryverylongpluginnamethatnobodywouldpickbutnothingstopsthem", "short"},
		{"long", strings.Repeat("a description that runs on and on and on ", 6)},
		{"multi", "first line of the description\nsecond line nobody expected"},
		{"wide", "설명이 전부 한글로 되어 있어서 한 글자가 두 칸을 차지합니다 " + strings.Repeat("가나다라마 ", 8)},
		{"ansi", "\x1b[31mred\x1b[0m and \x1b[1mbold\x1b[0m in a plugin's own description"},
		{"tabbed", "a\tdescription\twith\ttabs"},
	} {
		for _, size := range []struct{ w, h int }{{34, 30}, {60, 24}, {100, 40}, {80, 14}, {30, 10}} {
			s := newScript(t)
			s.m.cmds = &cmdSourceWith{cmds: []port.PluginCommand{c}}
			s.send(tea.WindowSizeMsg{Width: size.w, Height: size.h})
			s.assistantText("some work happened")
			// Type toward the command, the way a user reaching for it does. A bare "/" lists the
			// built-ins first and appends plugin rows last, so on a small screen the plugin's row
			// is legitimately behind the "… N more" marker — cut, but marked, which is the
			// palette working. What has to hold is that once its row IS on screen, its author's
			// strings cannot push the frame past the terminal.
			s.typeText("/" + c.name)
			// The plugin's own row has to be among them, and the palette has to be drawing it.
			// Asserting only that SOME match exists would pass over a palette full of built-ins
			// that never rendered the hostile string at all.
			found := false
			for _, mt := range s.m.paletteMatches() {
				if mt.name == "/"+c.name {
					found = true
				}
			}
			if !found {
				t.Fatalf("%q: the plugin command is not among the matches, so it is not under test", c.name)
			}
			lines := strings.Split(s.rawView(), "\n")
			// Searched with the frame's own wrapping taken out: a long name is drawn across
			// three lines inside the box, which is the box doing its job, not the row missing.
			if !strings.Contains(squash(s.rawView()), squash(c.name)) {
				t.Fatalf("%q at %dx%d: the plugin row was never drawn:\n%s",
					c.name, size.w, size.h, ansiSeq.ReplaceAllString(s.rawView(), ""))
			}
			if len(lines) > size.h {
				t.Errorf("%q at %dx%d: the frame is %d rows", c.name, size.w, size.h, len(lines))
			}
			for i, l := range lines {
				trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(l, ""), " ")
				if got := lipgloss.Width(trimmed); got > size.w {
					t.Errorf("%q at %dx%d: row %d draws %d cells: %q",
						c.name, size.w, size.h, i, got, trimmed)
				}
			}
		}
	}
}

// A plugin command is offered only when what has been typed is a prefix of it — the same rule the
// built-ins follow, so a plugin cannot squat the palette on every keystroke.
func TestAPluginCommandMatchesLikeABuiltIn(t *testing.T) {
	applyTheme(true)
	s := newScript(t)
	s.m.cmds = &cmdSourceWith{cmds: []port.PluginCommand{pluginCmd{name: "login", desc: "sign in"}}}
	s.send(tea.WindowSizeMsg{Width: 80, Height: 30})

	has := func(v string) bool {
		for _, c := range s.m.pluginCmdMatches(v) {
			if c.name == "/login" {
				return true
			}
		}
		return false
	}
	for _, v := range []string{"/", "/l", "/log", "/login"} {
		if !has(v) {
			t.Errorf("%q does not offer /login", v)
		}
	}
	for _, v := range []string{"/x", "/logout", "/lo gin"} {
		if has(v) {
			t.Errorf("%q offers /login", v)
		}
	}
	// No source wired is the ordinary case (no plugin host) and must not panic or match.
	s.m.cmds = nil
	if got := s.m.pluginCmdMatches("/"); got != nil {
		t.Errorf("a model with no command source returned %v", got)
	}
}
