package tui

import (
	"strings"
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

// A non-ASCII label survives the full event round-trip — marshaled by the app,
// unmarshaled by the TUI (ev() exercises real encoding/json) — as the exact same
// UTF-8 runes, never an ASCII-escaped \uXXXX literal. This is the core-clean proof
// for the report where "you" rendered as the literal 변냁재: if a
// plugin passes real Korean, the label renders as Korean; that escaped form can
// only originate upstream (a plugin handing us an already-escaped string), never
// from magi's own pipeline.
func TestUserLabelUnicodeRoundTrip(t *testing.T) {
	m := newTestModel(t)

	// The exact three code points the report showed escaped as 변냁재.
	const want = "변냁재"
	m.applyEvent(ev(t, event.TypeUserLabelChanged, event.UserLabelData{Label: want}))

	got := m.userLabel()
	if got != want {
		t.Fatalf("unicode label round-trip = %q, want %q (magi must not escape unicode)", got, want)
	}
	if strings.Contains(got, `\u`) {
		t.Fatalf("label contains an ASCII-escaped sequence %q; magi's pipeline must preserve UTF-8", got)
	}
	if r := []rune(got); len(r) != 3 || r[0] != 0xBCC0 || r[1] != 0xB0C1 || r[2] != 0xC7AC {
		t.Fatalf("label decoded to %d runes %U, want 3 runes U+BCC0 U+B0C1 U+C7AC — an escaped literal would be 18 ASCII runes", len(r), r)
	}
}
