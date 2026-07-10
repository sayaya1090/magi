package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func paletteNames(m *Model, input string) []string {
	m.ta.SetValue(input)
	ms := m.paletteMatches()
	names := make([]string, len(ms))
	for i, c := range ms {
		names[i] = c.name
	}
	return names
}

func has(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// Typing an alias prefix surfaces the alias with the canonical's description;
// the bare "/" list stays clean (canonicals only, no aliases).
func TestPaletteAliasMatching(t *testing.T) {
	mm := newTestModel(t)
	m := &mm

	if names := paletteNames(m, "/m"); !has(names, "/model") {
		t.Errorf("/m should surface /model, got %v", names)
	}
	if names := paletteNames(m, "/a"); !has(names, "/agents") {
		t.Errorf("/a should surface /agents, got %v", names)
	}
	if names := paletteNames(m, "/e"); !has(names, "/exit") {
		t.Errorf("/e should surface /exit, got %v", names)
	}
	if names := paletteNames(m, "/r"); !has(names, "/route") {
		t.Errorf("/r should surface /route, got %v", names)
	}
	// Bare slash: canonicals only — no alias clutter.
	if names := paletteNames(m, "/"); has(names, "/model") || has(names, "/exit") {
		t.Errorf("bare / should not list aliases, got %v", names)
	}
}

// The alias carries the canonical's description (so /model reads like /route).
func TestPaletteAliasDescription(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.ta.SetValue("/model")
	ms := m.paletteMatches()
	var got string
	for _, c := range ms {
		if c.name == "/model" {
			got = c.desc
		}
	}
	if got == "" {
		t.Fatalf("/model not matched")
	}
	// Same description text as the canonical /route entry.
	var want string
	for _, c := range slashCommands {
		if c.name == "/route" {
			want = c.desc
		}
	}
	if got != want {
		t.Errorf("/model desc = %q, want canonical %q", got, want)
	}
}

// Palette up/down wrap around (circular) instead of clamping at the ends, so a
// user can reach the last entry by pressing up once from the top.
func TestPaletteCircularNav(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.ta.SetValue("/") // bare slash lists all canonicals
	n := len(m.paletteMatches())
	if n < 2 {
		t.Fatalf("need at least 2 canonical commands to test wrap, got %d", n)
	}

	up := tea.KeyPressMsg{Code: tea.KeyUp}
	down := tea.KeyPressMsg{Code: tea.KeyDown}

	// From the top, up wraps to the bottom.
	m.palSel = 0
	if _, h := m.handleKey(up); !h {
		t.Fatal("up should be handled while the palette is open")
	}
	if m.palSel != n-1 {
		t.Errorf("up from top: palSel = %d, want %d (wrap to bottom)", m.palSel, n-1)
	}

	// From the bottom, down wraps to the top.
	m.palSel = n - 1
	m.handleKey(down)
	if m.palSel != 0 {
		t.Errorf("down from bottom: palSel = %d, want 0 (wrap to top)", m.palSel)
	}

	// Interior steps move by one in the expected direction.
	m.palSel = 1
	m.handleKey(up)
	if m.palSel != 0 {
		t.Errorf("up from 1: palSel = %d, want 0", m.palSel)
	}
	m.palSel = 0
	m.handleKey(down)
	if m.palSel != 1 {
		t.Errorf("down from 0: palSel = %d, want 1", m.palSel)
	}

	// A full cycle of downs returns to the start (n steps == identity).
	m.palSel = 0
	for i := 0; i < n; i++ {
		m.handleKey(down)
	}
	if m.palSel != 0 {
		t.Errorf("after %d downs palSel = %d, want 0 (full cycle)", n, m.palSel)
	}
}

// With a single match, wrapping keeps the selection pinned on the only entry
// (the modulo must not divide-by-zero or drift off index 0).
func TestPaletteCircularNavSingleMatch(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	// Find a prefix that yields exactly one match, so up/down have nowhere to go.
	var single string
	for _, c := range slashCommands {
		if len(paletteNames(m, c.name)) == 1 {
			single = c.name
			break
		}
	}
	if single == "" {
		t.Skip("no single-match command prefix available")
	}
	m.ta.SetValue(single)
	if got := len(m.paletteMatches()); got != 1 {
		t.Fatalf("precondition: %q should have 1 match, got %d", single, got)
	}
	m.palSel = 0
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.palSel != 0 {
		t.Errorf("up on single match: palSel = %d, want 0", m.palSel)
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.palSel != 0 {
		t.Errorf("down on single match: palSel = %d, want 0", m.palSel)
	}
}

// A stale out-of-range palSel (e.g. left over after the match list shrank as the
// user typed) is clamped before use, so wrapping arithmetic stays in bounds.
func TestPaletteCircularNavClampsStaleSel(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	m.ta.SetValue("/")
	n := len(m.paletteMatches())
	if n < 2 {
		t.Fatalf("need >= 2 matches, got %d", n)
	}
	// palSel far past the end: clampSel pins it to n-1, so up lands on n-2.
	m.palSel = n + 100
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.palSel != n-2 {
		t.Errorf("up from stale sel: palSel = %d, want %d", m.palSel, n-2)
	}
	// Negative stale sel: clampSel pins to 0, so down lands on 1.
	m.palSel = -50
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.palSel != 1 {
		t.Errorf("down from negative stale sel: palSel = %d, want 1", m.palSel)
	}
}
