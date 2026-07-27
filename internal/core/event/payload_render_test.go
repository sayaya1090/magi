package event

import (
	"reflect"
	"strings"
	"testing"
)

// Diff reports which entries are new in After (added, in after-order) and which are gone from
// Before (removed, in before-order). Membership is set-based, so a step present in both is neither
// added nor removed regardless of position.
func TestPlanRevisedDiff(t *testing.T) {
	added, removed := PlanRevisedData{
		Before: []string{"a", "b", "c"},
		After:  []string{"b", "c", "d", "e"},
	}.Diff()
	if !reflect.DeepEqual(added, []string{"d", "e"}) {
		t.Errorf("added = %v, want [d e]", added)
	}
	if !reflect.DeepEqual(removed, []string{"a"}) {
		t.Errorf("removed = %v, want [a]", removed)
	}
	// Identical plans → no diff (both named returns stay nil).
	if a, r := (PlanRevisedData{Before: []string{"x"}, After: []string{"x"}}).Diff(); a != nil || r != nil {
		t.Errorf("identical: added=%v removed=%v, want nil/nil", a, r)
	}
	// Reorder without content change → still no diff (set membership, not position).
	if a, r := (PlanRevisedData{Before: []string{"a", "b"}, After: []string{"b", "a"}}).Diff(); a != nil || r != nil {
		t.Errorf("reorder: added=%v removed=%v, want nil/nil", a, r)
	}
	// From-empty is all added; to-empty is all removed, each in source order.
	if a, _ := (PlanRevisedData{After: []string{"p", "q"}}).Diff(); !reflect.DeepEqual(a, []string{"p", "q"}) {
		t.Errorf("from empty: added=%v, want [p q]", a)
	}
	if _, r := (PlanRevisedData{Before: []string{"p", "q"}}).Diff(); !reflect.DeepEqual(r, []string{"p", "q"}) {
		t.Errorf("to empty: removed=%v, want [p q]", r)
	}
}

// A council rejection's feedback used to reach the run log only through the injected prompt, which
// renders as a 200-char note — and the advisory keep-list is prepended ABOVE the feedback there, so
// those 200 chars were spent on the advisory and the demand that held the turn open appeared nowhere.
// A run whose council refused three rounds in a row cannot be diagnosed afterward if no record says
// what it refused over.
func TestCouncilFeedbackLinesKeepTheObjectionAndStayBounded(t *testing.T) {
	lines := func(fb string) []string { return CouncilDecidedData{Feedback: fb}.FeedbackLines() }

	if got := lines("   \n\n  "); len(got) != 0 {
		t.Errorf("blank feedback must render nothing, got %q", got)
	}
	got := lines("The council did not agree:\n\n- Melchior (correctness): the output is not compared\n")
	if len(got) != 2 || got[0] != "The council did not agree:" {
		t.Fatalf("blank lines must be dropped and the rest kept, got %q", got)
	}
	if !strings.Contains(got[1], "Melchior") {
		t.Errorf("every member's objection must survive, got %q", got[1])
	}

	// One verbose member must not be able to bury the transcript: lines are capped and truncated,
	// and the cut is announced rather than silent.
	var many []string
	for i := 0; i < 40; i++ {
		many = append(many, strings.Repeat("x", 500))
	}
	out := lines(strings.Join(many, "\n"))
	if len(out) != 13 {
		t.Fatalf("expected 12 lines plus the truncation notice, got %d", len(out))
	}
	if !strings.Contains(out[12], "feedback continues") {
		t.Errorf("the cut must be announced, got %q", out[12])
	}
	for _, ln := range out[:12] {
		if len([]rune(ln)) > 201 {
			t.Errorf("a single line must be truncated, got %d runes", len([]rune(ln)))
		}
	}

	// A multibyte objection must never be cut mid-rune: a surface that prints the truncated line
	// would show a replacement glyph exactly where the demand was.
	one := lines(strings.Repeat("실", 400))
	if len(one) != 1 {
		t.Fatalf("expected one line, got %d", len(one))
	}
	if strings.ContainsRune(one[0], '�') {
		t.Errorf("truncation split a multibyte rune: %q", one[0])
	}
}
