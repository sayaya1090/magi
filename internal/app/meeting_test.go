package app

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/meeting"
	"github.com/sayaya1090/magi/internal/core/session"
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
	p := meetingPrompt("ops", "what to do about the empty state", said, "", false)
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
	c := meetingPrompt("ops", "what to do about the empty state", said, "", true)
	if !strings.Contains(c, "what YOU will do") {
		t.Errorf("the closing prompt does not ask for work:\n%s", c)
	}
	// The first speaker is told they are first rather than shown an empty heading.
	if f := meetingPrompt("design", "x", "", "", false); !strings.Contains(f, "You are first") {
		t.Errorf("the first speaker was given an empty transcript:\n%s", f)
	}
}

// A participant may look and may not touch, and that is the allowlist rather than the prompt.
//
// "Please do not change anything" is advice, and this tree has watched advice evaporate under
// pressure. What makes a meeting safe to hold while three companions have work in progress is that
// the sessions it opens cannot write.
//
// Asked of EVERY place in this file that hands a participant a tool list, rather than of one named
// function. The earlier version of this test named MeetingTurn — which by then nothing called, the
// daemon having moved to MeetingPrepare plus MeetingSayIn — so the safety property everybody read
// off this test was being proven against a function that never ran.
func TestEveryMeetingToolListIsTheFourThatLook(t *testing.T) {
	src, err := os.ReadFile("meeting.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	sites := fieldValues(body, "Tools")
	if len(sites) < 2 {
		t.Fatalf("meeting.go hands out a tool list %d time(s); the participant is prepared and then "+
			"spoken with, so there are at least two", len(sites))
	}
	for _, got := range sites {
		if got != "meetingLook" {
			t.Errorf("a participant is given %s rather than meetingLook", got)
		}
	}
	for _, forbidden := range []string{`"bash"`, `"write"`, `"edit"`, `"multiedit"`, `"apply_patch"`} {
		if strings.Contains(body, forbidden) {
			t.Errorf("meeting.go names %s", forbidden)
		}
	}
}

// A model writes markdown, and the word inside it still means what it says.
//
// The first live meeting came back "**PASS** – my workspace only contains billing" ten times over.
// The comparison was against the bare word, so every one of those was recorded as a contribution:
// no pass count moved, no round ever came back all-passes, and a discussion in which nothing was
// said ran to the ceiling — ten model turns spent saying nothing, which is the failure the
// convergence rule exists to prevent.
func TestAPassStillReadsAsOneWhenTheModelDressesItUp(t *testing.T) {
	for _, said := range []string{
		"**PASS** – my workspace only contains the billing API",
		"*PASS* — not mine",
		"`PASS`: nothing to add",
		"__PASS__ · the question does not touch what I work on",
		"PASS",
		"pass - already said by design",
	} {
		u := readUtterance("api", said)
		if !u.Pass {
			t.Errorf("%q was recorded as a contribution", said)
			continue
		}
		// And the reason comes out as a reason, with the dressing and the dash gone.
		if strings.HasPrefix(u.Text, "-") || strings.HasPrefix(u.Text, "–") ||
			strings.HasPrefix(u.Text, "—") || strings.HasPrefix(u.Text, "*") {
			t.Errorf("%q left punctuation at the front of the reason: %q", said, u.Text)
		}
	}
	// A sentence that merely contains the word is still a contribution.
	if u := readUtterance("api", "I would not pass on this one"); u.Pass {
		t.Error("a sentence about passing was read as a pass")
	}
}

// Preparing and speaking are the same session, and it is still the read-only one.
//
// Every contribution used to be its own child: three companions over five rounds put fifteen
// children on the strip, each starting cold. Read from the source, like the check above, because
// what is being asserted is which door the code goes through — a behaviour test would need a model
// to answer and would pass just as happily with fifteen spawns behind it.
func TestAParticipantSpeaksInTheSessionItPreparedIn(t *testing.T) {
	src, err := os.ReadFile("meeting.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	fn := func(name string) string {
		i := strings.Index(body, "func (a *App) "+name)
		if i < 0 {
			t.Fatalf("%s is gone", name)
		}
		out := body[i:]
		if j := strings.Index(out, "\n}"); j > 0 {
			out = out[:j]
		}
		return out
	}
	prep := fn("MeetingPrepare")
	if !strings.Contains(prep, "spawnChild") {
		t.Error("preparation does not make the child session the meeting reuses")
	}
	if !strings.Contains(prep, "Tools:    meetingLook") {
		t.Error("the participant is prepared with something other than the four tools that look")
	}
	say := fn("MeetingSayIn")
	if strings.Contains(say, "spawnChild") {
		t.Error("a turn still spawns a child, so the meeting is back to one session per turn")
	}
	if !strings.Contains(say, "appendPromptText") || !strings.Contains(say, "runLoop") {
		t.Error("a turn is not a prompt in the session that is already open")
	}
	for _, forbidden := range []string{`"bash"`, `"write"`, `"edit"`, `"multiedit"`} {
		if strings.Contains(prep+say, forbidden) {
			t.Errorf("a meeting participant may reach %s", forbidden)
		}
	}
}

// A readiness note is prose or it is nothing.
//
// A live run came back with `{"path": ".", "pattern": "fleet"}` as one companion's note — the
// model's last tool call echoed as its answer — and the roster showed it to the reader. Silence is
// not an error here: the note is a courtesy and the readiness is the fact.
func TestAReadinessNoteIsNeverAToolCall(t *testing.T) {
	for _, junk := range []string{`{"path": ".", "pattern": "fleet"}`, "  [\n  {\"a\":1}\n]  ", ""} {
		if got := readyNote(junk); got != "" {
			t.Errorf("readyNote(%q) kept %q", junk, got)
		}
	}
	said := "The client assumes three tries; nothing here enforces it."
	if got := readyNote("  " + said + "\n"); got != said {
		t.Errorf("a real note came back as %q", got)
	}
}

// The list itself, pinned where it now lives.
//
// The three spawn sites used to write it out one each, so a test could only ask each site whether
// it still held the right literal — and a fourth tool added to one of them was a change to one
// site's copy, which every comment in meeting.go would still have described as read-only. Now
// there is one list, so this asks the one question that matters: what is in it.
func TestTheMeetingAllowlistIsFourToolsThatOnlyLook(t *testing.T) {
	want := []string{"read", "glob", "grep", "list"}
	if len(meetingLook) != len(want) {
		t.Fatalf("the meeting allowlist is %v", meetingLook)
	}
	for i, w := range want {
		if meetingLook[i] != w {
			t.Errorf("the meeting allowlist is %v", meetingLook)
			break
		}
		_ = i
	}
	// The ones a meeting must never reach, named so the failure says which arrived.
	for _, forbidden := range []string{"bash", "write", "edit", "multiedit", "apply_patch"} {
		for _, got := range meetingLook {
			if got == forbidden {
				t.Errorf("a meeting participant can reach %q", forbidden)
			}
		}
	}
}

// The system prompt had two copies: one turn called meetingSystem and another pasted the same
// sentences inline. Editing the helper would have changed the preparation turn and left every
// SPEAKING turn on the old text, with nothing failing.
func TestBothMeetingTurnsShareOneSystemPrompt(t *testing.T) {
	src, err := os.ReadFile("meeting.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if n := strings.Count(body, "You are \" + who + \", taking part in a meeting"); n != 1 {
		t.Errorf("the meeting system prompt is written out %d times; it belongs in meetingSystem alone", n)
	}
	sites := fieldValues(body, "System")
	if len(sites) < 2 {
		t.Fatalf("meeting.go sets a system prompt %d time(s); preparing and speaking are both "+
			"turns and both need one", len(sites))
	}
	// Two helpers now, and the difference is the point: a participant argues, a secretary writes
	// down what was argued. What must not come back is a THIRD — an inline paste of either, which
	// is the shape this test was written for.
	for _, got := range sites {
		if got != "meetingSystem(who)" && got != "minutesSystem(who)" {
			t.Errorf("a turn takes its system prompt from %s rather than one of the two helpers", got)
		}
	}
	if n := strings.Count(body, "keeping the minutes of a meeting"); n != 1 {
		t.Errorf("the minutes system prompt is written out %d times; it belongs in minutesSystem alone", n)
	}
	// And the secretary is not the participant. If these ever became one string the minutes turn
	// would inherit "read your own files when you need to check something", which is the one thing
	// a session with no tools cannot do — and it would start reporting what it could not have read.
	if minutesSystem("x") == meetingSystem("x") {
		t.Error("the secretary and the participant share a system prompt; they are different jobs")
	}
}

// fieldValues is what each `name:` field in src is assigned, whatever gofmt aligned it to. Written
// out rather than matched as a literal because the two sites are formatted differently — one in a
// struct of its own and one on a shared line — and a test that pinned the spacing would go quiet
// the first time a field was added above it.
func fieldValues(src, name string) []string {
	re := regexp.MustCompile(`\b` + name + `:\s*([^,\n]+)`)
	var out []string
	for _, m := range re.FindAllStringSubmatch(src, -1) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

// The preparation prompt must not tell a participant that nobody hears this turn.
//
// It used to, two paragraphs above "for the person who called the meeting" — the prompt
// contradicted itself, and a model that believed the first half wrote its private reasoning into
// the answer. Measured in a live meeting: the note that reached the screen opened with the model's
// plan for its own answer and ended with the instruction echoed back at itself ("Two or three
// lines in Korean.") immediately before the Korean began, with no separator.
//
// The rule is not "never mention the audience" — it is "say who it actually is". So this asserts
// both halves: the false claim is gone, and the true one is present.
func TestThePreparationPromptDoesNotClaimNobodyIsListening(t *testing.T) {
	p := preparePrompt("alpha", "which store for the queue", "README.md", nil)
	if strings.Contains(p, "nobody will hear this turn") {
		t.Error("the prompt tells the participant nobody hears this, then shows what it answers " +
			"to the person who called the meeting — a model that believes it thinks out loud")
	}
	if !strings.Contains(p, "read by the person who called the meeting") {
		t.Error("the prompt must name the reader: an audience left unsaid is guessed at")
	}
	// And the narrower truth it replaced the lie with: no round, no answer.
	if !strings.Contains(p, "not a round") {
		t.Error("what IS true — the room does not answer this turn — is what keeps the " +
			"participant from opening the discussion here")
	}
}

// A participant getting ready is told who else is in the room and what each of them is for.
//
// Without it the last line of that prompt asks for "what you know that the others do not" while
// declining to say who the others are, and every participant answers about its own workspace and
// nothing else — the meeting's shape gets decided during the first round instead of before it.
func TestThePrepareBriefNamesTheRoomAndWhatEachOneIsFor(t *testing.T) {
	room := []meeting.Seat{
		{Name: "alpha", Role: "the wire", Does: "socket, roster"},
		{Name: "beta", Role: "storage", Does: "sqlite, migrations (+3)"},
		{Name: "gamma"},
		{Name: "kim", Person: true},
	}
	p := preparePrompt("alpha", "which store for the queue", "README.md", room)

	if !strings.Contains(p, "You are alpha.") {
		t.Error("the participant is not told its own name — it was passed one and the prompt dropped it")
	}
	for _, want := range []string{"beta", "storage", "sqlite, migrations (+3)", "gamma"} {
		if !strings.Contains(p, want) {
			t.Errorf("the roster does not carry %q:\n%s", want, p)
		}
	}
	// A companion that published neither a role nor abilities is still named. Nothing is invented
	// for it: a made-up description is worse than none, because the reader plans around it.
	if strings.Contains(p, "gamma (") || strings.Contains(p, "gamma —") {
		t.Errorf("something was invented for a companion that published nothing:\n%s", p)
	}
	if !strings.Contains(p, "kim — the person who called this meeting") {
		t.Errorf("the person in the room is not named as one:\n%s", p)
	}
	// And it is not told about itself. That is what this whole turn is for.
	if strings.Contains(p, "alpha (the wire)") {
		t.Errorf("the participant is listed among the others it is being asked to differ from:\n%s", p)
	}
}

// An empty roster leaves no heading behind. A section title with nothing under it reads as "nobody
// else is here", which is a different meeting from "nobody told me".
func TestAnEmptyRoomLeavesNoHeading(t *testing.T) {
	if p := preparePrompt("alpha", "q", "", nil); strings.Contains(p, "WHO ELSE IS IN THE ROOM") {
		t.Errorf("an empty roster still drew its heading:\n%s", p)
	}
}

// The minutes are written in a session of their own.
//
// This is the whole of design C and the one part of it that cannot be seen from the outside: both
// arrangements produce a revised document, so if the write-up quietly moved into the speaking
// session nothing would fail — and every participant would then be reading its own edit history
// while deciding what to say.
func TestTheMinutesAreWrittenSomewhereElse(t *testing.T) {
	llm := &fakeLLM{}
	a, room := meetingApp(t, llm)
	ctx := context.Background()
	note, err := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(),
		Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	const doc = "## Decided\n- (nothing yet)"
	if _, err := a.MeetingSayIn(ctx, room, "api", "the topic", "", doc, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.MeetingWriteUp(ctx, note, "api", "the topic", doc, "api said a thing"); err != nil {
		t.Fatal(err)
	}
	saw := func(sid session.SessionID, what string) bool {
		evs, err := a.store.Read(ctx, sid, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range evs {
			if strings.Contains(string(e.Data), what) {
				return true
			}
		}
		return false
	}
	if !saw(room, "THE MINUTES SO FAR") {
		t.Error("the speaker was not shown the minutes — it will agree again to what is already agreed")
	}
	if saw(room, "Give back the whole minutes") {
		t.Error("the write-up ran in the SPEAKING session: its drafts now sit in the context the " +
			"participant decides in, which is what two sessions exist to prevent")
	}
	if !saw(note, "Give back the whole minutes") {
		t.Error("the write-up did not reach the minutes session")
	}
}

// The first speaker is handed a form; everybody after is handed the document.
//
// Given as a filled-in skeleton rather than described in prose because a description produces a
// different shape from each model, and the second speaker then has to guess where its own edit
// belongs. And once there IS a document, handing the form again would invite a rewrite from
// scratch — which is the same failure as summarising: the record shrinks instead of growing.
func TestTheFirstSpeakerGetsAFormAndTheRestGetTheDocument(t *testing.T) {
	first := minutesPrompt("api", "which store", "", "api said a thing")
	for _, want := range []string{"## Decided", "## Still open", "## Action items", "## Questions for the room"} {
		if !strings.Contains(first, want) {
			t.Errorf("the form is missing %q — the sections are what everybody after navigates by:\n%s", want, first)
		}
	}
	later := minutesPrompt("design", "which store", "## Decided\n- use the queue", "design agreed")
	if strings.Contains(later, "THE MINUTES ARE EMPTY") {
		t.Errorf("a document already exists and the form was handed out again:\n%s", later)
	}
	if !strings.Contains(later, "- use the queue") {
		t.Errorf("the document did not reach the writer:\n%s", later)
	}
	// The two rules the record depends on. Without the first it shrinks every round; without the
	// second somebody wakes up owning work they never accepted.
	if !strings.Contains(later, "UNCHANGED") {
		t.Error("nothing tells the writer to carry unchanged lines through, so the minutes will shrink")
	}
	if !strings.Contains(later, "did not accept it") {
		t.Error("nothing stops the writer assigning work to a name that never took it on")
	}
	// The OTHER half of the same rule, and the one that was missing. Only the prohibition was
	// here, and a live run lost a commitment to exactly that gap: a speaker said it would add a
	// header to api.md and the minutes came back byte-identical to the document it was handed.
	// A model told only what it must not write, and asked to carry the rest through unchanged,
	// carries the whole thing through unchanged.
	if !strings.Contains(later, "MUST gain a line") {
		t.Error("nothing obliges the writer to record work the speaker just took on — the " +
			"prohibition alone loses commitments")
	}
	// And it names the speaker, so "took work on" is about this turn rather than about anybody.
	if !strings.Contains(later, "If design took work on") {
		t.Errorf("the obligation does not say WHOSE turn this is:\n%s", later)
	}
}
