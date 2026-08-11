package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/report"
)

// A question put to a person arrives with the grounds it was meant to be decided on.
//
// The tool refuses a report with a section missing, the event carries it, and the console draws
// it — and the terminal, where the person usually is, showed the question with the working thrown
// away. A decision put to somebody who has been away is exactly where the grounds are worth the
// most, and it was the one place they did not arrive.
func TestAQuestionArrivesWithItsGrounds(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.quest = &questReq{
		callID:   "c1",
		question: "Which branch should this land on?",
		options:  []string{"main", "engine-ui-split"},
		report: []report.Filled{
			{Key: "tried", Text: "ran the suite on both; engine-ui-split is three commits ahead"},
			{Key: "stakes", Text: "landing on main means a revert if the split lands first"},
			{Key: "lean", Text: "engine-ui-split, because the change touches files only it has"},
		},
	}
	out := m.questView()
	if !strings.Contains(out, "Which branch") {
		t.Fatalf("the question is missing:\n%s", out)
	}
	for _, want := range []string{"tried", "three commits ahead", "stakes", "lean"} {
		if !strings.Contains(out, want) {
			t.Errorf("the grounds are missing %q:\n%s", want, out)
		}
	}
	// Grounds first, question second. Read the other way round a person answers and justifies
	// afterwards, which is the failure the report exists to prevent.
	if strings.Index(out, "tried") > strings.Index(out, "Which branch") {
		t.Errorf("the question comes before what it should be decided on:\n%s", out)
	}
}

// A companion that wrote no report gets the prompt it always had — no empty headings.
func TestAQuestionWithNoGroundsIsTheOldPrompt(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.quest = &questReq{callID: "c1", question: "keep going?", options: []string{"yes", "no"}}
	out := m.questView()
	if !strings.Contains(out, "keep going?") {
		t.Fatalf("the question is missing:\n%s", out)
	}
	// A blank section is dropped rather than drawn: the contract refuses an empty one, so a gap
	// here is an older companion and not a lazy answer.
	m.quest.report = []report.Filled{{Key: "tried", Text: "   "}}
	if got := m.groundsBlock(m.quest.report); got != "" {
		t.Errorf("a blank section was drawn as a heading over nothing: %q", got)
	}
}

// On a terminal too short for the whole box, the question and the options survive.
//
// An unanswerable prompt is worse than an unexplained one, and the report is still in the
// transcript and on the console.
func TestAShortTerminalKeepsTheQuestionAnswerable(t *testing.T) {
	m := newTestModel(t)
	// Tall enough for the windowed list and not for the full box with a long report on top of it.
	// (Below about fifteen rows the modal cannot show an option at all, which is older than this
	// and is what the last-resort truncation is for.)
	m.width, m.height = 100, 20
	long := strings.Repeat("what it ran, at length. ", 40)
	m.quest = &questReq{
		callID: "c1", question: "which one?", options: []string{"a", "b", "c"},
		report: []report.Filled{{Key: "tried", Text: long}},
	}
	out := m.questView()
	if !strings.Contains(out, "which one?") {
		t.Errorf("the question was shed before the grounds:\n%s", out)
	}
	if !strings.Contains(out, "1. a") && !strings.Contains(out, "2. b") && !strings.Contains(out, "3. c") {
		t.Errorf("no option survived, so the prompt cannot be answered:\n%s", out)
	}
	// And the grounds are what gave way.
	if strings.Contains(out, "at length") {
		t.Errorf("the grounds were kept at the cost of the prompt fitting:\n%s", out)
	}
}

// The grounds survive the crossing from the event to the screen.
//
// Building the request by hand tests the drawing and not the wiring, and the wiring is where this
// was broken: the event carried a report and the model kept the question, the options and the call
// id. A test that skips the event would have passed against the defect.
func TestTheGroundsSurviveTheEventThatCarriesThem(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.applyEvent(ev(t, event.TypeQuestionRequested, event.QuestionRequestedData{
		CallID:   "c1",
		Question: "Which branch should this land on?",
		Options:  []string{"main", "engine-ui-split"},
		Report: []report.Filled{
			{Key: "tried", Text: "ran the suite on both; engine-ui-split is three commits ahead"},
		},
	}))
	if m.quest == nil {
		t.Fatal("the question never reached the model")
	}
	if len(m.quest.report) != 1 {
		t.Fatalf("the report was dropped between the event and the model: %+v", m.quest)
	}
	if !strings.Contains(m.questView(), "three commits ahead") {
		t.Error("what crossed is not what is drawn")
	}
}

// A question says which of how many, when more are coming.
//
// A tool may ask several and each one blocks. Answering the first of three and being handed the
// next without warning is a different situation from answering the only question there is —
// "is this the whole decision" is part of the decision.
func TestAQuestionSaysWhichOfHowMany(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.applyEvent(ev(t, event.TypeQuestionRequested, event.QuestionRequestedData{
		CallID: "c1#2", Question: "and the scope?", Options: []string{"small", "big"},
		Index: 2, Total: 3,
	}))
	if got := m.questView(); !strings.Contains(got, "2 of 3") {
		t.Errorf("the modal does not place the question in its run:\n%s", got)
	}

	// Silent at one, which is nearly every question: "1 of 1" answers something nobody asked and
	// teaches the eye to skip the spot where the real one appears.
	m.applyEvent(ev(t, event.TypeQuestionRequested, event.QuestionRequestedData{
		CallID: "c2", Question: "go ahead?", Options: []string{"yes", "no"}, Index: 1, Total: 1,
	}))
	if got := m.questView(); strings.Contains(got, "1 of 1") {
		t.Errorf("a lone question is numbered anyway:\n%s", got)
	}
}
