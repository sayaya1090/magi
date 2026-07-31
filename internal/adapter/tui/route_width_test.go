package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// openRouteEditing opens /route and starts editing the session row, which is what raises the model
// suggest box under it.
func openRouteEditing(t *testing.T, s *script) {
	t.Helper()
	s.typeText("/route").enter()
	if !s.m.routing {
		t.Fatal("/route did not open the editor")
	}
	if s.m.routeList[s.m.routeSel].kind != rowSession {
		t.Fatalf("the session row is not selected first; got %v", s.m.routeList[s.m.routeSel].kind)
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !s.m.routeEditing {
		t.Fatal("enter did not start editing the session row")
	}
}

// The route editor's title plus its keyboard hint runs about 75 cells, and a gateway catalog's
// model ids are longer still. Neither was clipped. In a vertically joined frame one over-wide row
// pads every other row to match, so this is not a clipped line — it is the whole screen going wider
// than the terminal, and the shell wrapping it.
//
// This is the third surface in this package with the same shape (the find bar and the profile form
// were the others), which is why the check is on the WHOLE frame rather than on the one function.
func TestTheRouteEditorFitsItsTerminal(t *testing.T) {
	for _, w := range []int{34, 40, 60, 100} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: w, Height: 30})
		openRouteEditing(t, s)
		s.m.modelCatalog = []string{
			"internal-gateway/anthropic/claude-sonnet-4-5-20250929-v1:0",
			"openai/gpt-4o-2024-08-06-preview-long-context-128k",
			"qwen3-coder-next:latest",
		}
		s.m.catalogLoaded = true
		s.m.modelSugSel = 0
		if len(s.m.modelSuggestions()) == 0 {
			t.Fatal("no suggestions, so the box is not under test")
		}

		for i, line := range strings.Split(s.rawView(), "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
			if got := lipgloss.Width(trimmed); got > w {
				t.Errorf("width %d: row %d draws %d cells: %q", w, i, got, trimmed)
			}
		}
	}
}

// A model id is elided in the MIDDLE, not the head. The ids in a gateway catalog share a long
// prefix and differ in the tail — cutting the tail leaves a column of rows that all read the same,
// which is worse than a shortened row: the user cannot tell the entries apart at all.
func TestALongModelIdKeepsBothEnds(t *testing.T) {
	const id = "internal-gateway/anthropic/claude-sonnet-4-5-20250929-v1:0"
	got := elideMiddle(id, 30)
	if lipgloss.Width(got) > 30 {
		t.Errorf("elided to %d cells: %q", lipgloss.Width(got), got)
	}
	if !strings.HasPrefix(got, "internal") {
		t.Errorf("the head is gone: %q", got)
	}
	if !strings.HasSuffix(got, "v1:0") {
		t.Errorf("the tail is gone — the part that distinguishes one revision from the next: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("an unmarked cut reads as the whole id: %q", got)
	}
	// Short enough to fit is returned untouched, and an unmeasured terminal is not a narrow one.
	if got := elideMiddle("short", 30); got != "short" {
		t.Errorf("a fitting value was altered: %q", got)
	}
	if got := elideMiddle(id, 0); got != id {
		t.Errorf("width 0 must leave the value alone, got %q", got)
	}
}

// The editor still says which editor it is when the hint has to go — a header cut down to nothing
// leaves the user in a full-screen view with no label.
func TestTheRouteEditorKeepsItsTitleWhenTheHintGoes(t *testing.T) {
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 34, Height: 30})
	openRouteEditing(t, s)
	head := strings.Split(ansiSeq.ReplaceAllString(s.m.routeView(), ""), "\n")[0]
	if !strings.Contains(head, "models") {
		t.Errorf("the title did not survive the narrowing: %q", head)
	}
}
