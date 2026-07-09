package tui

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// userLabel() falls back to "you" until a plugin sets a label; a
// user.label.changed event then swaps it in (and invalidates the render cache so
// existing transcript blocks re-render with the new name).
func TestUserLabelEventUpdatesLabel(t *testing.T) {
	m := newTestModel(t)
	if got := m.userLabel(); got != "you" {
		t.Fatalf("default user label = %q, want you", got)
	}

	m.applyEvent(ev(t, event.TypeUserLabelChanged, event.UserLabelData{Label: "Alice"}))
	if got := m.userLabel(); got != "Alice" {
		t.Fatalf("user label after event = %q, want Alice", got)
	}

	// An empty payload is ignored (no clobber back to blank/"you").
	m.applyEvent(ev(t, event.TypeUserLabelChanged, event.UserLabelData{Label: ""}))
	if got := m.userLabel(); got != "Alice" {
		t.Fatalf("empty user.label.changed must not clobber, got %q", got)
	}
}
