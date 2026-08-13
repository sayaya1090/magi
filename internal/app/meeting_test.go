package app

import (
	"os"
	"strings"
	"testing"
)

// A pass is an answer, and it has to be told from a contribution that mentions passing.
//
// One keyword at the START, because that is what a model can be relied on to produce — and a reply
// that merely contains the word ("I would not pass on this") is somebody speaking. Getting this
// backwards would drop a contribution and record a silence, which is the one error in a meeting
// record nobody can spot afterwards.
func TestAPassIsToldFromSomebodySpeakingAboutPassing(t *testing.T) {
	cases := []struct {
		said   string
		pass   bool
		reason string
	}{
		{"PASS", true, ""},
		{"PASS: nothing in my workspace touches billing", true, "nothing in my workspace touches billing"},
		{"pass — the schema is design's call", true, "the schema is design's call"},
		{"", true, ""},
		{"I would not pass on this: the retry is wrong", false, ""},
		{"Passing the token would be premature; we have not read the logs", false, ""},
		{"The endpoint already returns 409 here.", false, ""},
	}
	for _, c := range cases {
		u := readUtterance("api", c.said)
		if u.Pass != c.pass {
			t.Errorf("%q read as pass=%v", c.said, u.Pass)
			continue
		}
		if c.pass && c.reason != "" && u.Text != c.reason {
			t.Errorf("%q left the reason as %q", c.said, u.Text)
		}
		if !c.pass && u.Text != strings.TrimSpace(c.said) {
			t.Errorf("%q was altered to %q", c.said, u.Text)
		}
	}
}

// What a participant is given: the question, everything said, and the two shapes an answer takes.
//
// The transcript goes in whole. A meeting where the fourth speaker reads a précis of the first
// three has thrown away its one advantage — that each speaker read what came before.
func TestAParticipantIsGivenTheWholeDiscussion(t *testing.T) {
	said := "design: the empty state needs a token name\napi passed: not mine\n"
	p := meetingPrompt("ops", "what to do about the empty state", said, false)
	if !strings.Contains(p, said) {
		t.Errorf("the transcript was not passed through:\n%s", p)
	}
	if !strings.Contains(p, "PASS") {
		t.Error("nothing told the participant that passing is an answer")
	}
	// And what a pass DOES, which is the half a model cannot infer.
	//
	// The meeting ends when the room has nothing left to add, so passing is how a participant votes
	// to end it. Told only that passing is "allowed", a model treats it as abstaining from a
	// discussion that will continue regardless — so it finds something to say every time it is
	// asked, and the round ceiling becomes what stops the room. A number nobody chose on purpose
	// then decides when a question has been answered.
	if !strings.Contains(p, "ENDS WHEN NOBODY HAS ANYTHING LEFT TO ADD") {
		t.Errorf("the participant is not told that passing is what ends the meeting:\n%s", p)
	}
	// And the closing question is a different question: what will YOU do.
	c := meetingPrompt("ops", "what to do about the empty state", said, true)
	if !strings.Contains(c, "what YOU will do") {
		t.Errorf("the closing prompt does not ask for work:\n%s", c)
	}
	// The first speaker is told they are first rather than shown an empty heading.
	if f := meetingPrompt("design", "x", "", false); !strings.Contains(f, "You are first") {
		t.Errorf("the first speaker was given an empty transcript:\n%s", f)
	}
}

// A participant may look and may not touch, and that is the allowlist rather than the prompt.
//
// "Please do not change anything" is advice, and this tree has watched advice evaporate under
// pressure. What makes a meeting safe to hold while three companions have work in progress is that
// the sessions it opens cannot write.
func TestAMeetingTurnIsSpawnedReadOnly(t *testing.T) {
	src, err := os.ReadFile("meeting.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	i := strings.Index(body, "func (a *App) MeetingTurn")
	if i < 0 {
		t.Fatal("MeetingTurn is gone")
	}
	spec := body[i:]
	if j := strings.Index(spec, "\n}"); j > 0 {
		spec = spec[:j]
	}
	if !strings.Contains(spec, `Tools:    []string{"read", "glob", "grep", "list"}`) {
		t.Error("the meeting turn is not spawned with the four tools that only look")
	}
	for _, forbidden := range []string{`"bash"`, `"write"`, `"edit"`, `"multiedit"`} {
		if strings.Contains(spec, forbidden) {
			t.Errorf("a meeting turn may reach %s", forbidden)
		}
	}
}
