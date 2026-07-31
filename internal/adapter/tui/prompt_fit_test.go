package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/prompt"
)

// loginSpec is the shape this form actually gets in production: a plugin sign-in with a gateway
// URL, a token, and a tenant list whose ids are longer than a narrow terminal is wide.
func loginSpec() prompt.Spec {
	return prompt.Spec{Title: "sign in to the gateway", Fields: []prompt.Field{
		{Name: "endpoint", Label: "endpoint", Type: prompt.TypeText,
			Default: "https://gateway.internal.example.com/v1/openai/chat/completions"},
		{Name: "token", Label: "token", Type: prompt.TypePassword, Default: strings.Repeat("k", 48)},
		{Name: "tenant", Label: "tenant", Type: prompt.TypeSelect, Options: []string{
			"internal-gateway/anthropic/claude-sonnet-4-5-20250929-v1:0", "short",
		}},
	}}
}

func sized(s prompt.Spec, w, h int) promptModel {
	m, _ := newPromptModel(s).Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(promptModel)
}

// The form is a full-screen surface, so a row wider than the terminal is not one clipped line: the
// shell wraps it and every row below shifts down, which carried Submit off the bottom of a login
// screen. This is the fourth surface in this package with that shape (the find bar, the profile
// form and the route editor were the others), which is why the check is on the whole frame.
func TestThePromptFormFitsItsTerminal(t *testing.T) {
	applyTheme(true)
	for _, w := range []int{34, 40, 60, 100} {
		m := sized(loginSpec(), w, 30)
		for i, line := range strings.Split(m.View().Content, "\n") {
			trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
			if got := lipgloss.Width(trimmed); got > w {
				t.Errorf("width %d: row %d draws %d cells: %q", w, i, got, trimmed)
			}
		}
	}
}

// When a value has to be cut it is the value's tail that goes, never the label — a form whose
// labels are cut is one the user cannot answer. And the cut is marked: an unmarked one reads as
// the whole endpoint, which is how someone signs in against the wrong host.
func TestThePromptFormKeepsLabelsWhenValuesAreCut(t *testing.T) {
	applyTheme(true)
	out := ansiSeq.ReplaceAllString(sized(loginSpec(), 40, 30).View().Content, "")
	for _, label := range []string{"endpoint", "token", "tenant"} {
		if !strings.Contains(out, label) {
			t.Errorf("the %q label did not survive the narrowing:\n%s", label, out)
		}
	}
	if strings.Contains(out, "completions") {
		t.Errorf("a 63-cell value was drawn whole into 40 cells:\n%s", out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("an unmarked cut reads as the whole value:\n%s", out)
	}
}

// Submit is what ends the form, so it stays on screen at every size — including the narrow one
// where its keyboard hint has to go. Losing the hint costs a reminder; losing the button leaves
// the user in a full-screen form with no visible way out.
func TestThePromptFormAlwaysShowsSubmit(t *testing.T) {
	applyTheme(true)
	for _, w := range []int{20, 34, 100} {
		out := ansiSeq.ReplaceAllString(sized(loginSpec(), w, 30).View().Content, "")
		if !strings.Contains(out, "Submit") {
			t.Errorf("width %d has no Submit:\n%s", w, out)
		}
	}
	narrow := ansiSeq.ReplaceAllString(sized(loginSpec(), 34, 30).View().Content, "")
	if strings.Contains(narrow, "Tab submit") {
		t.Errorf("the hint was kept at 34 cells, where it does not fit:\n%s", narrow)
	}
	if !strings.Contains(ansiSeq.ReplaceAllString(sized(loginSpec(), 100, 30).View().Content, ""), "Tab submit") {
		t.Error("the hint is gone at 100 cells, where it fits")
	}
}

// Taller than the screen is the same failure on the other axis. The form pages the fields around
// the selection rather than running off the bottom, and says how many there are — a field that is
// simply not drawn is indistinguishable from a field the form never had.
func TestATallPromptFormPagesRatherThanOverflowing(t *testing.T) {
	applyTheme(true)
	var fs []prompt.Field
	for i := 0; i < 12; i++ {
		fs = append(fs, prompt.Field{Name: fmt.Sprintf("f%d", i), Label: fmt.Sprintf("field%d", i), Type: prompt.TypeText})
	}
	const h = 20
	m := sized(prompt.Spec{Title: "many", Fields: fs}, 80, h)
	m.sel = 9 // deep in the list, so the window has to move

	out := m.View().Content
	if got := lipgloss.Height(out); got > h {
		t.Errorf("a %d-line screen was handed %d lines:\n%s", h, got, out)
	}
	plain := ansiSeq.ReplaceAllString(out, "")
	if !strings.Contains(plain, "/12 fields") {
		t.Errorf("fields were dropped with nothing saying how many there are:\n%s", plain)
	}
	// The field being answered is the one that must be on screen.
	if !strings.Contains(plain, "field9") {
		t.Errorf("the selected field is not drawn:\n%s", plain)
	}
	if !strings.Contains(plain, "Submit") {
		t.Errorf("Submit fell off the bottom:\n%s", plain)
	}
}

// An unmeasured terminal (no WindowSizeMsg yet) is not a zero-sized one: the form lays out at its
// default width and is not shrunk to nothing.
func TestAnUnmeasuredPromptFormStillDraws(t *testing.T) {
	applyTheme(true)
	out := ansiSeq.ReplaceAllString(newPromptModel(loginSpec()).View().Content, "")
	for _, want := range []string{"endpoint", "token", "Submit"} {
		if !strings.Contains(out, want) {
			t.Errorf("an unsized form is missing %q:\n%s", want, out)
		}
	}
}
