package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A verdict's grounds are the one part of it a reader can CHECK rather than weigh: magi looked the
// fragment up in the material the member was shown. They were recorded on the fact and rendered
// nowhere, which is the same half-measure the member's "keep" was in before it got a path — the
// part of a verdict nobody could read.
func TestTheVerdictDetailShowsWhatTheVoteStoodOn(t *testing.T) {
	applyTheme(true)
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	// A session with no blocks shows the startup splash instead of the viewport, so the detail
	// would be behind it — in production this panel is opened from a transcript that has content.
	s.assistantText("some work happened")
	s.m.councilDetail = &event.CouncilVerdictData{
		Round: 1, Member: "Balthasar", Lens: "verification", Decision: "done",
		Rationale: "the suite ran clean",
		Cite:      "3 passed in 0.42s",
	}
	s.m.refresh()
	got := ansiSeq.ReplaceAllString(s.rawView(), "")
	if !strings.Contains(got, "grounds") {
		t.Errorf("the detail view has no grounds section:\n%s", got)
	}
	if !strings.Contains(got, "3 passed in 0.42s") {
		t.Errorf("the fragment the vote rests on is not shown:\n%s", got)
	}
}

// The two ways there are no grounds are different, and both have to be legible. NO-EVIDENCE is a
// member saying plainly that it judged the report's substance; an empty field is a member that did
// not answer. Rendering either as blank hides the verdict a reader should look at twice.
func TestNoGroundsIsSaidInWordsNotLeftBlank(t *testing.T) {
	for _, c := range []struct{ cite, want string }{
		{"", "none given"},
		{"NO-EVIDENCE", "none observed"},
		{"no-evidence", "none observed"},
		{"exit 0", `"exit 0"`},
	} {
		if got := citeLabel(c.cite); !strings.Contains(got, c.want) {
			t.Errorf("citeLabel(%q) = %q, want it to contain %q", c.cite, got, c.want)
		}
	}
}

// The transcript row stays ONE line per round. Surfacing a missing citation there was tried and
// reverted: the compact row is the design — members on one line, detail behind a click — and a
// model that does not fill `cite` would have added a line per member to every round. The grounds
// belong in the detail view, which is where a reader goes to weigh a vote.
func TestTheVerdictRowStaysOneLineWhateverTheGrounds(t *testing.T) {
	applyTheme(true)
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.emit(event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Casper"}, Rule: "majority", Task: "t"})
	s.emit(event.TypeCouncilVerdict, event.CouncilVerdictData{Round: 1, Member: "Melchior", Decision: "done", Cite: ""})
	s.emit(event.TypeCouncilVerdict, event.CouncilVerdictData{Round: 1, Member: "Casper", Decision: "done", Cite: "exit 0"})
	s.emit(event.TypeCouncilDecided, event.CouncilDecidedData{Round: 1, Decision: "done"})

	for _, b := range s.m.blocks {
		if b.kind != blockCouncilVerdict {
			continue
		}
		if got := strings.Count(s.m.renderBlock(b), "\n"); got != 0 {
			t.Errorf("the verdict row grew to %d lines:\n%s", got+1, ansiSeq.ReplaceAllString(s.m.renderBlock(b), ""))
		}
	}
}
