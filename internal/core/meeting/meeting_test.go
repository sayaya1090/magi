package meeting

import (
	"strings"
	"testing"
)

// opened is a meeting whose participants have all finished getting ready.
//
// Every test in this file is about what happens IN the room — who speaks, who is skipped, when it
// ends — and none is about the door. Written once here rather than as a line in each, so the
// preparation gate reads as a precondition instead of being smuggled into seven setups.
func opened(m *Meeting) *Meeting {
	for i := range m.Speakers {
		m.Speakers[i].Ready = true
	}
	m.Open()
	return m
}

func names(m *Meeting, n int) []string {
	var out []string
	for i := 0; i < n; i++ {
		s, ok := m.Next()
		if !ok {
			out = append(out, "—")
			break
		}
		out = append(out, s.Name)
		m.Say(Utterance{Who: s.Name, Text: "something about " + s.Name})
	}
	return out
}

// Everybody speaks once before anybody speaks twice.
//
// The baseline of a meeting is hearing from each of them: a first round that skipped anybody would
// be a meeting whose shape depended on who happened to be asked first.
func TestTheFirstRoundAsksEverybody(t *testing.T) {
	m := opened(New("the empty state", []Speaker{
		{Name: "design", Socket: "/s/d"}, {Name: "api", Socket: "/s/a"}, {Name: "ops", Socket: "/s/o"},
		{Name: "you"}, // the person: never asked, speaks when they want to
	}, 3))
	got := strings.Join(names(m, 3), " ")
	if got != "design api ops" {
		t.Fatalf("the first round went %q", got)
	}
	if m.Round != 1 {
		t.Errorf("three of three spoke and it is round %d", m.Round)
	}
	// The person is not in the order at all.
	for _, u := range m.Said {
		if u.Who == "you" {
			t.Error("the person was asked to speak")
		}
	}
}

// A speaker that reads the room and has nothing to add says so, and that is not the same as not
// being asked. Two in a row and it is left out — the largest waste in a meeting is a turn spent on
// a companion that has twice said this is not its business.
func TestPassingTwiceLeavesYouOutUntilSomebodyNamesYou(t *testing.T) {
	m := opened(New("the billing endpoint", []Speaker{
		{Name: "design", Socket: "/s/d"}, {Name: "api", Socket: "/s/a"},
	}, 9))
	// Round one: design speaks, api passes.
	s, _ := m.Next()
	m.Say(Utterance{Who: s.Name, Text: "the screen needs the error text"})
	s, _ = m.Next()
	if s.Name != "api" {
		t.Fatalf("second to speak was %s", s.Name)
	}
	m.Say(Utterance{Who: "api", Pass: true})
	// Round two: api passes again.
	s, _ = m.Next()
	m.Say(Utterance{Who: s.Name, Text: "and a code"})
	s, _ = m.Next()
	if s.Name != "api" {
		t.Fatalf("api was skipped after ONE pass; it was %s", s.Name)
	}
	m.Say(Utterance{Who: "api", Pass: true, Text: "nothing in my workspace touches this"})
	// Round three: api is not asked at all. design speaks, and the next question goes to design
	// again in round four rather than to the companion that has twice said this is not its business.
	s, ok := m.Next()
	if !ok || s.Name != "design" {
		t.Fatalf("round three began with %v (%v)", s.Name, ok)
	}
	m.Say(Utterance{Who: "design", Text: "then I will write it as it is"})
	if s, ok = m.Next(); !ok || s.Name != "design" {
		t.Fatalf("after two passes api was asked again: %v (%v)", s.Name, ok)
	}
	m.Say(Utterance{Who: "design", Text: "@api can you confirm the shape?"})
	// …until it is named, which puts it back at the FRONT: being called on means being answered
	// next, not eventually.
	s, ok = m.Next()
	if !ok || s.Name != "api" {
		t.Fatalf("naming api did not bring it back: %v (%v)", s.Name, ok)
	}
	// And answering spends the naming, or one "@api?" would keep it in every round afterwards.
	m.Say(Utterance{Who: "api", Text: "it is {code, message}"})
	if m.Named["api"] {
		t.Error("being named outlived the answer to it")
	}
	// Speaking also clears the two passes behind it.
	for _, sp := range m.Speakers {
		if sp.Name == "api" && sp.Passes != 0 {
			t.Errorf("api spoke and still carries %d passes", sp.Passes)
		}
	}
}

// A room where everybody passed has stopped moving, and that ends it — a better reason than a
// count, and it ends without another lap of asking the same silent room.
func TestAWholeRoundOfPassesEndsIt(t *testing.T) {
	m := opened(New("the rollout", []Speaker{
		{Name: "api", Socket: "/s/a"}, {Name: "ops", Socket: "/s/o"},
	}, 9))
	for i := 0; i < 2; i++ {
		s, ok := m.Next()
		if !ok {
			t.Fatalf("it ended after %d", i)
		}
		m.Say(Utterance{Who: s.Name, Pass: true})
	}
	if _, ok := m.Next(); ok {
		t.Error("everybody passed and it asked for more")
	}
	if !m.Closed {
		t.Error("it stopped asking and did not close")
	}
	// And it ended because the room ran out of things to say, not because a count stopped it. The
	// two are different answers to "are these conclusions the whole answer", and a reader who
	// cannot tell them apart is reading a summary of an argument that may have been cut in half.
	if m.Spent {
		t.Error("a meeting that converged is recorded as one that ran out of rounds")
	}
	if m.Round >= m.MaxRounds {
		t.Errorf("it used all %d rounds, so this proves nothing about ending early", m.MaxRounds)
	}
}

// The rounds are bounded, because each lap is a model turn per participant and a meeting that
// cannot end is one that spends until somebody notices.
func TestTheRoundsRunOut(t *testing.T) {
	m := opened(New("what to do about the flake", []Speaker{{Name: "api", Socket: "/s/a"}}, 2))
	for i := 0; i < 2; i++ {
		s, ok := m.Next()
		if !ok {
			t.Fatalf("it ended after %d rounds", i)
		}
		m.Say(Utterance{Who: s.Name, Text: "still thinking"})
	}
	if _, ok := m.Next(); ok {
		t.Error("a two-round meeting went to a third")
	}
	// Marked as what it is: the backstop, not the room. Nobody passed here — the participant had
	// something to say every time it was asked.
	if !m.Spent {
		t.Error("the ceiling ended it and the meeting does not say so")
	}
}

// The default is a backstop and not a plan, so it has to be loose enough that an ordinary
// disagreement ends by being settled rather than by being cut off.
//
// Three was the default, and three is a statement, an objection and one reply — which is the shape
// of a discussion that has just got interesting. The number is not a preference: it is the point at
// which the console stops a room that will not stop itself.
func TestTheDefaultCeilingIsNotWhatEndsAnOrdinaryMeeting(t *testing.T) {
	m := opened(New("what to do", []Speaker{{Name: "api", Socket: "/s/a"}}, 0))
	if m.MaxRounds < 5 {
		t.Errorf("the default ceiling is %d rounds", m.MaxRounds)
	}
}

// The person takes the floor when they want it and gives it back, and nothing about that is a turn.
func TestThePersonTakesTheFloorAndGivesItBack(t *testing.T) {
	m := opened(New("naming", []Speaker{{Name: "design", Socket: "/s/d"}, {Name: "you"}}, 3))
	m.Take("you")
	if m.Holder != "you" {
		t.Fatalf("the floor is with %q", m.Holder)
	}
	m.Say(Utterance{Who: "you", Text: "call it a companion, not an agent"})
	if m.Holder != "" {
		t.Errorf("saying it left the floor with %q", m.Holder)
	}
	// And what the person said is in what the next speaker reads.
	if !strings.Contains(m.Transcript(), "not an agent") {
		t.Errorf("the transcript is %q", m.Transcript())
	}
	s, ok := m.Next()
	if !ok || s.Name != "design" {
		t.Errorf("after the person, the floor went to %v (%v)", s.Name, ok)
	}
}

// A pass is in the record, with its reason when there was one: silence from somebody who read the
// discussion is information, and a reader of the transcript has to be able to tell it from absence.
func TestTheTranscriptTellsSilenceFromAbsence(t *testing.T) {
	m := opened(New("the schema", []Speaker{{Name: "api", Socket: "/s/a"}, {Name: "ops", Socket: "/s/o"}}, 2))
	m.Say(Utterance{Who: "api", Text: "add a column"})
	m.Say(Utterance{Who: "ops", Pass: true, Text: "no deploy impact"})
	got := m.Transcript()
	if !strings.Contains(got, "api: add a column") {
		t.Errorf("what was said is missing: %q", got)
	}
	if !strings.Contains(got, "ops passed: no deploy impact") {
		t.Errorf("a pass with a reason reads as %q", got)
	}
}

// Nobody speaks until everybody has read their own workspace.
//
// A meeting where the participants arrive cold spends its first two rounds looking things up out
// loud, and a reader watching the screen cannot tell that from a slow model. The room waits — and
// a participant that could NOT get ready does not hold it, because a room that never opens is
// worse than one that opens a voice short and says so.
func TestTheRoomWaitsUntilEverybodyIsReady(t *testing.T) {
	m := New("who owns the retry budget", []Speaker{
		{Name: "api", Socket: "/s/a"}, {Name: "ops", Socket: "/s/o"}, {Name: "you"},
	}, 3)
	if _, ok := m.Next(); ok {
		t.Fatal("somebody was asked to speak before the room opened")
	}
	if m.Open() {
		t.Fatal("the room opened with nobody ready")
	}
	m.Prepared("api", "the client assumes three tries", "")
	if m.Open() {
		t.Fatal("the room opened with one of two ready")
	}
	if _, ok := m.Next(); ok {
		t.Fatal("a prepared participant was asked while the other was still reading")
	}
	// The one that could not get ready is not a reason to wait for ever.
	m.Prepared("ops", "", "no daemon at /s/o")
	if !m.Open() {
		t.Fatal("the room did not open once everybody had answered one way or the other")
	}
	who, ok := m.Next()
	if !ok || who.Name != "api" {
		t.Fatalf("the first turn went to %q (%v)", who.Name, ok)
	}
	// What each of them brought is on the record, which is what the screen draws.
	if m.Speakers[0].Brief == "" || !m.Speakers[0].Ready {
		t.Errorf("the prepared participant reads as %+v", m.Speakers[0])
	}
	if m.Speakers[1].Ready || m.Speakers[1].Trouble == "" {
		t.Errorf("the one that failed reads as %+v", m.Speakers[1])
	}
}
