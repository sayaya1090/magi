package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Sweep four: the screens a click takes you INTO, and the tool bodies. A detail view replaces the
// transcript entirely, so if it renders wrong the user has nothing else to look at — and the way in
// is a mouse click, which no unit test performs.

// Clicking a member's verdict opens their reasoning, with the evidence the round was judged on.
// The evidence is the part that only started carrying the tool output today; a detail view that
// shows the claim but not what backed it is how a false "done" reads as reasonable.
func TestClickingAVerdictOpensItWithItsEvidence(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "finish it")
	s.emit(event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority",
		Task: "finish it", Report: "the tests pass",
		Actions: "- bash `go test ./...` → ok: PASS", Changes: "### x.go\n+func F() {}",
	})
	for _, v := range []event.CouncilVerdictData{
		{Round: 1, Member: "Melchior", Lens: "correctness", Decision: "done", Rationale: "the run is clean"},
		{Round: 1, Member: "Balthasar", Lens: "verification", Decision: "continue", Rationale: "no external signal was delivered"},
	} {
		s.emit(event.TypeCouncilVerdict, v)
	}
	_ = s.view()

	// Find the verdict row on screen and click its first member.
	row := -1
	for i, b := range s.m.blocks {
		if b.kind == blockCouncilVerdict {
			row = i
		}
	}
	if row < 0 || row >= len(s.m.blockLineStart) {
		t.Fatal("no verdict row rendered, so there is nothing to click")
	}
	y := s.m.blockLineStart[row] - s.m.vp.YOffset() + headerRows
	s.send(tea.MouseClickMsg{Button: tea.MouseLeft, X: 4, Y: y})
	s.send(tea.MouseReleaseMsg{Button: tea.MouseLeft, X: 4, Y: y})

	if s.m.councilDetail == nil {
		t.Fatal("clicking a verdict must open it")
	}
	s.renders("a verdict's detail", s.m.councilDetail.Member)
	plain := s.view()
	for _, want := range []string{"go test ./...", "the tests pass"} {
		if !strings.Contains(plain, want) {
			t.Errorf("the detail view dropped the evidence %q:\n%s", want, plain)
		}
	}
	// esc returns to the transcript, not to a blank screen.
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.m.councilDetail != nil {
		t.Error("esc must leave the detail view")
	}
	s.renders("back in the transcript", "finish it")
}

// Transcript search is the only way to find anything in a long session — the alt-screen hides it
// from the terminal's own find. A query that matches nothing must say so rather than appearing to
// do nothing.
func TestSearchFindsAndSurvivesNoMatches(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "read everything")
	for i, txt := range []string{"the first landmark", "filler", "the second landmark", "filler"} {
		_ = i
		s.assistantText(txt)
	}
	s.emit(event.TypeTurnFinished, event.TurnFinishedData{})

	s.send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if !s.m.searching {
		t.Fatal("ctrl+f did not open search, so this asserts nothing")
	}
	s.typeText("landmark")
	if len(s.m.searchHits) < 2 {
		t.Errorf("two lines contain the query, %d hits found", len(s.m.searchHits))
	}
	s.renders("search with hits")

	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.send(tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	s.typeText("zzzznotpresent")
	if len(s.m.searchHits) != 0 {
		t.Errorf("nothing contains that query, %d hits reported", len(s.m.searchHits))
	}
	s.renders("search with no hits")
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.m.searching {
		t.Error("esc must close search")
	}
}

// A tool result with no recorded call. It happens on replay and on paths that never emitted the
// call, and the fold has a fallback for exactly that — a result that vanishes because its call is
// missing is output the user paid for and never sees.
func TestAResultWithNoRecordedCallStillShows(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "do it")
	s.emit(event.TypePartAppended, event.PartAppendedData{
		MessageID: "m_a", Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: "", Content: mustJSON("orphaned output the user still paid for")}},
	})
	s.renders("an orphaned tool result", "orphaned output")
}

// webfetch/websearch bodies go through the plain-text renderer, which is the one body path with no
// structure to lean on. Empty, huge, and control-character content are the three shapes that break
// a naive line walker.
func TestPlainTextToolBodiesHandleTheAwkwardShapes(t *testing.T) {
	for _, c := range []struct{ what, body string }{
		{"empty", ""},
		{"one long unbroken line", strings.Repeat("x", 5000)},
		{"control characters", "a\x00b\x1bc\rd\te"},
		{"many lines", strings.Repeat("a line\n", 500)},
	} {
		s := newScript(t)
		s.steer("r1", "fetch it")
		s.toolCall("webfetch", "c1")
		s.toolResult("c1", c.body)
		if got := len(s.m.textBody(c.body)); got < 0 {
			t.Fatalf("%s: negative line count", c.what)
		}
		s.renders("a webfetch body: " + c.what)
	}
}
