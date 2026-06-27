package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Arrow-up/down recall walks the input history.
func TestRecallHistory(t *testing.T) {
	m := newTestModel(t)
	m.history = []string{"first", "second", "third"}
	m.histIdx = len(m.history)

	if _, h := m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp}); !h {
		t.Fatal("up should be handled")
	}
	if m.ta.Value() != "third" {
		t.Fatalf("up → %q, want third", m.ta.Value())
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.ta.Value() != "second" {
		t.Fatalf("up up → %q, want second", m.ta.Value())
	}
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.ta.Value() != "third" {
		t.Fatalf("down → %q, want third", m.ta.Value())
	}
}

// Up does not recall while a turn is running.
func TestRecallBlockedWhileRunning(t *testing.T) {
	m := newTestModel(t)
	m.history = []string{"x"}
	m.histIdx = 1
	m.running = true
	if m.recallHistory(-1) {
		t.Fatal("recall should be blocked while running")
	}
}

// Tab completes the typed prefix from history (most recent match).
func TestHistoryComplete(t *testing.T) {
	m := newTestModel(t)
	m.history = []string{"git status", "git push origin", "ls -la"}

	if got := m.historyComplete("git "); got != "git push origin" {
		t.Fatalf("complete(%q) = %q, want git push origin", "git ", got)
	}
	if got := m.historyComplete("ls"); got != "ls -la" {
		t.Fatalf("complete(ls) = %q", got)
	}
	if got := m.historyComplete("zzz"); got != "" {
		t.Fatalf("no match should be empty, got %q", got)
	}
}

// Tab on a non-empty, non-slash input completes from history via handleKey.
func TestTabCompletesFromHistory(t *testing.T) {
	m := newTestModel(t)
	m.history = []string{"deploy to staging"}
	m.ta.SetValue("deploy")
	if _, h := m.handleKey(tea.KeyPressMsg{Code: tea.KeyTab}); !h {
		t.Fatal("tab should be handled")
	}
	if m.ta.Value() != "deploy to staging" {
		t.Fatalf("tab completion → %q", m.ta.Value())
	}
}
