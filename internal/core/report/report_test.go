package report

import (
	"reflect"
	"strings"
	"testing"
)

// A skill is prose with a list in the middle of it, so the parser has to know where the list stops.
// Both ends are load-bearing: a block that swallowed the rest of the document would turn every
// bullet in the skill into a section the agent must fill, and a block that ended at the first blank
// line would drop sections somebody spaced out for readability.
func TestOnlySectionsUnderTheSectionsHeadingCount(t *testing.T) {
	got := Parse(strings.Join([]string{
		"# decision-report",
		"Write these when you ask a person to decide. Take your time over stakes.",
		"",
		"## Sections",
		"- tried: what you ran",
		"",
		"- stakes: what the wrong pick costs",
		"",
		"## Notes",
		"- style: keep it short",
	}, "\n"))
	want := Contract{
		{"tried", "what you ran"},
		{"stakes", "what the wrong pick costs"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// Prose after the list ends it. Without that, a sentence explaining the sections would be followed
// by whatever bullets came next — an example, a caveat — and the agent would be refused for not
// filling in a section the skill was only describing.
func TestAListThatEndedInProseDoesNotResume(t *testing.T) {
	got := Parse(strings.Join([]string{
		"## sections",
		"- tried: what you ran",
		"For example, if the build failed, say which target.",
		"- example: this is a sample, not a section",
	}, "\n"))
	want := Contract{{"tried", "what you ran"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// The key is what the model fills, so the two spellings of one section have to land on one key —
// and a second declaration of it must not become a second section the agent is refused for missing.
func TestOneSectionIsOneKeyHoweverItIsSpelled(t *testing.T) {
	got := Parse(strings.Join([]string{
		"## sections",
		"-   Tried  :   what you ran   ",
		"- TRIED: said twice",
		"- a line with no colon",
		"- : a prompt with no key",
		"- lean: which way you would go",
	}, "\n"))
	want := Contract{
		{"tried", "what you ran"},
		{"lean", "which way you would go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// nil, not an empty contract: the caller reads "this skill declares nothing" and falls back to
// Default. An empty contract would mean a report needs no sections at all, which is the opposite of
// what a half-written skill is saying.
func TestASkillWithNoSectionsBlockDeclaresNothing(t *testing.T) {
	for _, body := range []string{
		"",
		"# decision-report\nJust write something useful.\n- tried: not under a heading that opened the block",
		"## section\n- tried: near miss on the heading",
	} {
		if got := Parse(body); got != nil {
			t.Fatalf("Parse(%q) = %#v, want nil so the caller falls back to Default", body, got)
		}
	}
}

// The order is the report: a contract that asks what you tried before what you would pick is telling
// the reader how to arrive at the decision. Sections the contract did not ask for keep their place
// at the end, in an order that does not move between runs.
func TestTheContractsOrderSurvivesAndExtrasFollowIt(t *testing.T) {
	c := Contract{{"tried", "p"}, {"stakes", "p"}, {"lean", "p"}}
	got := c.Fill(map[string]string{
		"lean":   "  the second one  ",
		"stakes": "a day either way",
		"tried":  "ran the suite",
		"zeal":   " extra ",
		"aside":  "also extra",
	})
	want := []Filled{
		{"tried", "ran the suite"},
		{"stakes", "a day either way"},
		{"lean", "the second one"},
		{"aside", "also extra"},
		{"zeal", "extra"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// A section holding a space is a section the model skipped while satisfying the letter of the check.
// It is missing, it is not rendered, and it does not become an extra section either.
func TestASectionFilledWithSpaceIsNotFilled(t *testing.T) {
	c := Contract{{"tried", "p"}, {"stakes", "p"}, {"lean", "p"}}
	values := map[string]string{"tried": "ran the suite", "stakes": "   ", "spare": " \t "}

	if got, want := c.Missing(values), []string{"stakes", "lean"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Missing = %#v, want %#v (contract order)", got, want)
	}
	if got := c.Unknown(values); len(got) != 0 {
		t.Fatalf("Unknown = %#v, want nothing: a blank extra is not something the report says", got)
	}
	if got, want := c.Fill(values), []Filled{{"tried", "ran the suite"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Fill = %#v, want %#v", got, want)
	}
	if got := c.Missing(map[string]string{"tried": "a", "stakes": "b", "lean": "c", "spare": "d"}); len(got) != 0 {
		t.Fatalf("Missing = %#v, want nothing: an answered report is answered whatever else it carries", got)
	}
}

// A report that says more than it was asked to is doing its job, so the extras come back sorted
// rather than dropped for not being on a list.
func TestWhatWasNotAskedForIsKeptInAStableOrder(t *testing.T) {
	c := Contract{{"tried", "p"}}
	got := c.Unknown(map[string]string{"tried": "asked for", "zeal": "z", "aside": "a", "middle": "m"})
	if want := []string{"aside", "middle", "zeal"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// The spec is what a rejection quotes back, so one refused call has to be enough to learn the shape:
// every section named, each on its own line, with the sentence saying what belongs there.
func TestTheSpecNamesEverySectionAndWhatItIsFor(t *testing.T) {
	got := Contract{{"tried", "what you ran"}, {"lean", ""}}.Spec()
	if want := "\n  tried — what you ran\n  lean"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	for _, s := range Default {
		if !strings.Contains(Default.Spec(), s.Key) || !strings.Contains(Default.Spec(), s.Prompt) {
			t.Fatalf("Default.Spec() = %q, missing %q or its prompt", Default.Spec(), s.Key)
		}
	}
}
