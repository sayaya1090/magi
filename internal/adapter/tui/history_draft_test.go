package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Browsing history REPLACES the input box, so a half-written prompt is destroyed the moment ↑ is
// pressed unless it is stashed. Coming back down used to clear the box outright — the input gone,
// silently, to a key that reads as navigation. Every readline-family prompt restores it.
//
// A multi-line draft was already protected (recallHistory refuses while LineCount > 1), which is
// what makes the one-line case an omission rather than a choice: you do not guard one and discard
// the other.
func TestSteppingThroughHistoryAndBackRestoresTheDraft(t *testing.T) {
	s := newScript(t)
	s.m.history = []string{"first prompt", "second prompt"}
	s.m.histIdx = len(s.m.history)
	s.typeText("a draft I am still writing")

	s.send(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := s.m.ta.Value(); got != "second prompt" {
		t.Fatalf("↑ did not recall the newest entry: %q", got)
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := s.m.ta.Value(); got != "first prompt" {
		t.Fatalf("a second ↑ did not walk older: %q", got)
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := s.m.ta.Value(); got != "a draft I am still writing" {
		t.Errorf("coming back down lost the draft: %q", got)
	}
}

// Nothing typed means nothing to restore — coming back down leaves an empty box, not the last
// recalled entry.
func TestComingBackWithNoDraftLeavesAnEmptyBox(t *testing.T) {
	s := newScript(t)
	s.m.history = []string{"only entry"}
	s.m.histIdx = len(s.m.history)

	s.send(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := s.m.ta.Value(); got != "only entry" {
		t.Fatalf("↑ did not recall: %q", got)
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := s.m.ta.Value(); got != "" {
		t.Errorf("with no draft the box should be empty, got %q", got)
	}
}

// Submitting retires the stash: the draft belonged to a prompt that has now been sent, and
// resurrecting it on a later ↓ would put a stale line under the user's cursor.
func TestASubmittedPromptRetiresTheStashedDraft(t *testing.T) {
	s := newScript(t)
	s.m.history = []string{"older"}
	s.m.histIdx = len(s.m.history)
	s.typeText("draft")
	s.send(tea.KeyPressMsg{Code: tea.KeyUp}) // stashes "draft"
	s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.m.ta.Value() != "draft" {
		t.Fatal("setup: the draft should be back")
	}
	s.enter() // submit it

	s.send(tea.KeyPressMsg{Code: tea.KeyUp})
	s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	if got := s.m.ta.Value(); got != "" {
		t.Errorf("a submitted draft came back on a later ↓: %q", got)
	}
}

// Walking up past the oldest entry stops there rather than wrapping or emptying — a wrap would
// make ↑ feel like it lost the history.
func TestWalkingPastTheOldestEntryStops(t *testing.T) {
	s := newScript(t)
	s.m.history = []string{"oldest", "newest"}
	s.m.histIdx = len(s.m.history)
	for i := 0; i < 5; i++ {
		s.send(tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if got := s.m.ta.Value(); got != "oldest" {
		t.Errorf("walking past the top gave %q", got)
	}
}
