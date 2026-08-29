package app

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/port"
)

// The push side is a one-line count pointer — it must NOT contain the entries' text,
// so relevant knowledge stays reachable without spending context every turn.
func TestExperiencePointerIsCountOnly(t *testing.T) {
	if p := experiencePointer(0, 0); p != "" {
		t.Fatalf("no matches should drop the section, got %q", p)
	}
	p := experiencePointer(2, 1)
	if !strings.Contains(p, "3 relevant") {
		t.Errorf("pointer should report the total (3), got %q", p)
	}
	if !strings.Contains(p, "recall_memory") {
		t.Errorf("pointer should route the agent to recall_memory, got %q", p)
	}
	// Singular vs plural.
	if !strings.Contains(experiencePointer(1, 0), "entry ") {
		t.Errorf("one match should read 'entry', got %q", experiencePointer(1, 0))
	}
	if !strings.Contains(experiencePointer(2, 0), "entries ") {
		t.Errorf("two matches should read 'entries', got %q", experiencePointer(2, 0))
	}
}

// The pull side (recall_memory) is where the actual entry text enters context.
func TestFormatExperienceFullRendersDetail(t *testing.T) {
	out := formatExperienceFull(
		[]port.Memory{{Text: "[project] always run gofmt before commit"}},
		[]port.Skill{{Name: "[global] deploy", Description: "ship to staging first", Body: "step1\nstep2"}},
	)
	// The shape, not just the words. A pull lands in the model's context as a list, and a skill
	// body that breaks out of its own entry reads as a new top-level item — measured: the body's
	// indent was reached by this test but asserted by nobody, so dropping it changed nothing here.
	if out != "- [project] always run gofmt before commit\n"+
		"- skill [global] deploy: ship to staging first\n"+
		"  step1\n"+
		"  step2" {
		t.Errorf("pull output has the wrong shape; got:\n%s", out)
	}
}

// Entry text is model-authored and store-round-tripped, so it arrives with whatever whitespace it
// arrived with. Trimming is what keeps one ragged entry from bending the list around it — and an
// empty body must leave no indented blank line behind, which reads as an entry that said nothing.
func TestARaggedEntryDoesNotBendTheList(t *testing.T) {
	out := formatExperienceFull(
		[]port.Memory{{Text: "  padded on both sides  "}},
		[]port.Skill{
			{Name: "a", Description: "  also padded  ", Body: "   \n\t\n"},
			{Name: "b", Description: "d", Body: "one"},
		},
	)

	if out != "- padded on both sides\n- skill a: also padded\n- skill b: d\n  one" {
		t.Errorf("ragged input must land square; got:\n%s", out)
	}
}

// Nothing recalled renders nothing — not a blank line, which would open the recall's answer with
// an empty item.
func TestAnEmptyRecallRendersNothing(t *testing.T) {
	if out := formatExperienceFull(nil, nil); out != "" {
		t.Errorf("empty recall must render empty, got %q", out)
	}
}
