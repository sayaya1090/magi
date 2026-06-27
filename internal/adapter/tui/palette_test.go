package tui

import "testing"

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
