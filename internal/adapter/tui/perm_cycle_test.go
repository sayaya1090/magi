package tui

import "testing"

// ctrl-p cycles ask → auto → allow → deny → ask, updating the app's live
// permission mode each step (an unknown current mode falls back to "ask").
func TestCyclePermission(t *testing.T) {
	m := newTestModel(t)
	m.app.SetPermission("ask")
	for _, want := range []string{"auto", "allow", "deny", "ask"} {
		m.cyclePermission()
		if got := m.app.Permission(); got != want {
			t.Fatalf("cycle → %q, want %q", got, want)
		}
	}
	m.app.SetPermission("bogus")
	m.cyclePermission()
	if got := m.app.Permission(); got != "ask" {
		t.Errorf("unknown mode should cycle to the first entry (ask), got %q", got)
	}
}

// parseTokenCount accepts bare numbers, k/m suffixes, and the "unlimited" words;
// junk and negatives are rejected rather than silently zeroed.
func TestParseTokenCount(t *testing.T) {
	for _, tc := range []struct {
		in string
		n  int
		ok bool
	}{
		{"32000", 32000, true},
		{"32k", 32000, true},
		{" 2M ", 2_000_000, true},
		{"unlimited", 0, true},
		{"off", 0, true},
		{"0", 0, true},
		{"-5", 0, false},
		{"12kb", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	} {
		n, ok := parseTokenCount(tc.in)
		if n != tc.n || ok != tc.ok {
			t.Errorf("parseTokenCount(%q) = (%d,%v), want (%d,%v)", tc.in, n, ok, tc.n, tc.ok)
		}
	}
}
