package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/config"
	"github.com/sayaya1090/magi/internal/core/meeting"
)

// talker is a companion that takes part in a meeting without a model behind it: it says what it
// was told to say, and remembers what it was asked.
type talker struct {
	*recordingEngine
	say  string
	pass bool
	// slow holds the turn open, which is how a meeting in progress is made observable from a test
	// without racing it, and started says the turn has BEGUN — sent before the block, because the
	// two facts are needed at different moments and only one of them can be read off the record.
	slow    chan struct{}
	started chan struct{}

	mu     sync.Mutex
	asked  []string // the transcripts it was given, in order
	closes int
}

func (t *talker) MeetingTurn(ctx context.Context, topic, transcript string, closing bool) (
	string, bool, error) {
	if t.started != nil {
		select {
		case t.started <- struct{}{}:
		default:
		}
	}
	if t.slow != nil {
		select {
		case <-t.slow:
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	t.mu.Lock()
	t.asked = append(t.asked, transcript)
	if closing {
		t.closes++
	}
	t.mu.Unlock()
	if closing {
		return "I will " + t.say, false, nil
	}
	return t.say, t.pass, nil
}

func (t *talker) heard() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.asked...)
}

// room stands up two companions that can talk, and returns the console holding them.
func room(t *testing.T) (*fleetFixture, *talker, *talker, url.Values) {
	t.Helper()
	f := newFleetFixture(t)
	design := &talker{recordingEngine: &recordingEngine{}, say: "the empty state needs a shape"}
	api := &talker{recordingEngine: &recordingEngine{}, say: "billing cannot answer that quickly"}
	dwd, awd := shortTempDir(t), shortTempDir(t)
	ds := f.liveDaemonAs(t, dwd, "d", design, daemon.Identity{Name: "design", Role: "the screens"})
	as := f.liveDaemonAs(t, awd, "a", api, daemon.Identity{Name: "api", Role: "billing"})
	f.session("d", dwd, "idle", 0, true)
	f.session("a", awd, "idle", 0, true)
	return f, design, api, url.Values{"who": {ds, as}}
}

func convene(t *testing.T, f *fleetFixture, form url.Values) meetView {
	t.Helper()
	w := post(t, f.srv, f.srv.meet, "/meet", form)
	if w.Code != http.StatusOK {
		t.Fatalf("convening replied %d: %s", w.Code, w.Body.String())
	}
	var v meetView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return v
}

// read is the screen refreshing: it must not move the meeting on.
func read(t *testing.T, f *fleetFixture, id string) meetView {
	t.Helper()
	w := httptest.NewRecorder()
	f.srv.meet(w, httptest.NewRequest(http.MethodGet, "/meet?id="+url.QueryEscape(id), nil))
	if w.Code != http.StatusOK {
		t.Fatalf("reading the meeting replied %d: %s", w.Code, w.Body.String())
	}
	var v meetView
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return v
}

func until(t *testing.T, why string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", why)
}

// The whole of it: two companions discuss, each reads what the other said, and each leaves with
// something to do that a person can then hand out.
//
// End to end over real sockets, because the join is the thing worth checking — a meeting is the
// console asking one daemon at a time and passing what came back to the next one, and a stub in
// place of the wire would prove none of that.
func TestAMeetingIsEachCompanionReadingWhatTheLastOneSaid(t *testing.T) {
	f, design, api, who := room(t)
	who.Set("topic", "how long may the fleet table take to load")
	who.Set("rounds", "1")
	v := convene(t, f, who)
	if len(v.Speakers) != 3 {
		t.Fatalf("the room is %+v", v.Speakers) // two companions and the person
	}

	until(t, "the meeting to finish", func() bool { return len(read(t, f, v.ID).Tasks) == 2 })
	got := read(t, f, v.ID)

	// The second speaker was given what the first one said. This is the property that makes it a
	// discussion instead of two answers to the same question.
	heard := api.heard()
	if len(heard) == 0 || !strings.Contains(heard[0], "the empty state needs a shape") {
		t.Errorf("the second speaker did not read the first one:\n%q", heard)
	}
	if len(design.heard()) == 0 || strings.TrimSpace(design.heard()[0]) != "" {
		t.Errorf("the first speaker was given a transcript that did not exist: %q", design.heard())
	}
	// Both said something, attributed, in the round they said it in.
	if len(got.Said) != 2 || got.Said[0].Who != "design" || got.Said[1].Who != "api" {
		t.Fatalf("the transcript is %+v", got.Said)
	}
	for _, line := range got.Said {
		if line.Round != 1 {
			t.Errorf("%s spoke in round %d of a one-round meeting", line.Who, line.Round)
		}
	}
	// And each left with something to do.
	if len(got.Tasks) != 2 || got.Tasks[0].What == "" {
		t.Fatalf("the conclusions are %+v", got.Tasks)
	}

	// Handing one out is a separate act, and it arrives as work in that companion's own session
	// with the meeting named — a person reading that log later needs to know where it came from.
	w := post(t, f.srv, f.srv.meetHand, "/meet-hand", url.Values{"id": {v.ID}, "who": {"design"}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("handing the task out replied %d: %s", w.Code, w.Body.String())
	}
	sent := design.seen()
	if len(sent) != 1 || !strings.Contains(sent[0], "fleet table") {
		t.Fatalf("what reached the companion was %v", sent)
	}
	if api.seen() != nil && len(api.seen()) != 0 {
		t.Errorf("the other companion was given work nobody handed it: %v", api.seen())
	}
}

// Nothing changes in a workspace because a meeting happened.
//
// The whole reason participants speak from read-only sessions is that a discussion is not a
// decision to act. What crosses into a workspace is one thing — a task a person pressed a button
// to send — and this is the check that the meeting itself sends nothing.
func TestAMeetingByItselfGivesNobodyWork(t *testing.T) {
	f, design, api, who := room(t)
	who.Set("topic", "should we cache the fleet table")
	who.Set("rounds", "1")
	v := convene(t, f, who)
	until(t, "the meeting to finish", func() bool { return len(read(t, f, v.ID).Tasks) == 2 })

	for name, eng := range map[string]*talker{"design": design, "api": api} {
		if got := eng.seen(); len(got) != 0 {
			t.Errorf("%s was given work by the meeting itself: %v", name, got)
		}
	}
}

// Somebody typing quietens the room.
//
// Holding the floor is the one thing that stops the driver, and it has to stop it BEFORE the next
// turn is asked for — a hush that arrives after the model has already been sent the transcript is
// a hush that did nothing.
func TestNobodyIsAskedToSpeakWhileSomebodyIsTyping(t *testing.T) {
	f, design, api, who := room(t)
	design.slow, design.started = make(chan struct{}), make(chan struct{}, 1)
	who.Set("topic", "who owns the retry budget")
	v := convene(t, f, who)

	// Wait until the first speaker actually HAS the floor before taking it away.
	//
	// Taking it first is not a sharper version of this test, it is a different one: the hush works,
	// so a driver that had not yet asked anybody would never ask, nothing would be said, and the
	// wait below would time out on correct behaviour. It did — on CI, where the POST won a race it
	// wins on nobody's laptop.
	until(t, "the first speaker to be asked", func() bool {
		select {
		case <-design.started:
			return true
		default:
			return false
		}
	})

	// Take the floor while that first speaker is still composing.
	w := post(t, f.srv, f.srv.meetSay, "/meet-say", url.Values{"id": {v.ID}, "hold": {"1"}})
	if w.Code != http.StatusOK {
		t.Fatalf("taking the floor replied %d: %s", w.Code, w.Body.String())
	}
	close(design.slow) // the first speaker finishes; the second must not be asked
	until(t, "the first contribution", func() bool { return len(read(t, f, v.ID).Said) == 1 })

	// Long enough that a driver which ignored the hold would have asked by now: the fake answers
	// immediately, so the only thing between it and the transcript is the hold.
	time.Sleep(300 * time.Millisecond)
	if got := api.heard(); len(got) != 0 {
		t.Fatalf("a companion was asked to speak over somebody who was typing: %v", got)
	}
	if v := read(t, f, v.ID); !v.Held || v.Holder == "" {
		t.Errorf("the screen does not show the floor as taken: %+v", v)
	}

	// Saying it gives the floor back, and the next speaker reads what was typed.
	post(t, f.srv, f.srv.meetSay, "/meet-say", url.Values{"id": {v.ID},
		"text": {"the budget is mine, but only above 200ms"}})
	until(t, "the next speaker", func() bool { return len(api.heard()) > 0 })
	if got := api.heard(); !strings.Contains(got[0], "above 200ms") {
		t.Errorf("what the person said did not reach the next speaker:\n%q", got[0])
	}
}

// Refreshing the screen must not move the meeting on.
//
// "Who is next" is worked out from the rules, and the rules advance the round when the current one
// is over. Asking them on the live meeting is the shape of bug where a page that polls every second
// burns a three-round meeting in three seconds — with nobody having spoken.
func TestReadingAMeetingDoesNotAdvanceIt(t *testing.T) {
	f, design, _, who := room(t)
	design.slow = make(chan struct{})
	defer close(design.slow)
	who.Set("topic", "what breaks if we ship on friday")
	v := convene(t, f, who)

	first := read(t, f, v.ID)
	for i := 0; i < 20; i++ {
		read(t, f, v.ID)
	}
	after := read(t, f, v.ID)
	if after.Round != first.Round {
		t.Errorf("twenty refreshes moved the meeting from round %d to round %d",
			first.Round, after.Round)
	}
	if after.Closed {
		t.Error("refreshing the screen ended the meeting")
	}
	// And it says whose turn it is, which is the reason the question is asked at all.
	var next string
	for _, sp := range after.Speakers {
		if sp.Next {
			next = sp.Name
		}
	}
	if next != "design" {
		t.Errorf("the screen points at %q as the next speaker", next)
	}
}

// The same thing at the round boundary, which is where it actually bites.
//
// Mid-round the rules answer "who is next" without changing anything, so a screen reading the live
// meeting looks harmless. It is the moment everybody has spoken that costs: there, asking who is
// next is what STARTS the next round — so a page polling once a second would spend a three-round
// meeting in three seconds, with nobody having said a word in rounds two or three.
func TestRefreshingAtTheEndOfARoundDoesNotStartTheNextOne(t *testing.T) {
	run := &meetingRun{m: meeting.New("what to do", []meeting.Speaker{
		{Name: "design", Socket: "/s/d"}, {Name: "api", Socket: "/s/a"}, {Name: "you"},
	}, 3)}
	run.m.Say(meeting.Utterance{Who: "design", Text: "one"})
	run.m.Say(meeting.Utterance{Who: "api", Text: "two"})

	for i := 0; i < 20; i++ {
		run.view()
	}
	if run.m.Round != 1 {
		t.Errorf("twenty refreshes at the end of round 1 left the meeting in round %d", run.m.Round)
	}
	if run.m.Closed {
		t.Error("refreshing the screen ran the meeting out of rounds")
	}
	// The driver asking, once, is what moves it on — the difference between reading and driving.
	if _, ok := run.m.Next(); !ok || run.m.Round != 2 {
		t.Errorf("the driver could not start round 2: round %d, closed %v", run.m.Round, run.m.Closed)
	}
}

// A meeting is convened with companions this console publishes, and nothing else.
//
// The participants arrive in the BODY, so the gate at the front has no companion to check — the
// same shape /dispatch has. A socket that is not in the fleet is a path somebody typed, and this
// process must not dial a path somebody typed.
func TestAMeetingCannotBeConvenedWithASocketNobodyPublished(t *testing.T) {
	f, _, _, who := room(t)
	real1 := who["who"][0]

	for _, bad := range []string{"/tmp/somewhere-else.sock", "../../etc/passwd"} {
		w := post(t, f.srv, f.srv.meet, "/meet", url.Values{
			"topic": {"anything"}, "who": {real1, bad}})
		if w.Code != http.StatusBadRequest {
			t.Errorf("a meeting with %q replied %d: %s", bad, w.Code, w.Body.String())
		}
	}
	// And one companion is not a meeting: the console says so rather than starting a room of one.
	w := post(t, f.srv, f.srv.meet, "/meet", url.Values{"topic": {"anything"}, "who": {real1}})
	if w.Code != http.StatusBadRequest {
		t.Errorf("a meeting of one replied %d: %s", w.Code, w.Body.String())
	}
}

// A person may only convene the companions they can already see.
//
// Scope is per person, and a route whose subject comes from the body passes the front-door check
// under no name at all. Without a second check here, a viewer narrowed to one companion could put
// somebody else's in a room and spend their model turns.
func TestAMeetingRespectsWhoseCompanionsThoseAre(t *testing.T) {
	f, _, _, who := room(t)
	f.srv.userHeader = "X-Forwarded-User"
	// Through the real auth file, so the built-in roles are the ones a console actually has: a
	// policy assembled by hand in a test can grant a role that does not exist and prove nothing.
	if err := os.WriteFile(filepath.Join(f.cfgDir, config.AuthFile), []byte(`
[people."kim"]
role = "operator"
companions = ["design"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadAuth(f.cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	f.srv.policy = p
	r := httptest.NewRequest(http.MethodPost, "/meet",
		strings.NewReader(url.Values{"topic": {"anything"}, "who": who["who"]}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Forwarded-User", "kim")
	w := httptest.NewRecorder()
	f.srv.meet(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a meeting with somebody else's companion replied %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "api") {
		t.Errorf("the refusal names the companion it is hiding: %s", w.Body.String())
	}
}

// A participant that cannot be reached is recorded, and the others carry on.
//
// The alternative — the driver stopping on the first error — makes one crashed daemon end a
// discussion the rest of the room was having, and leaves it looking as if nobody had anything
// more to say.
func TestACompanionThatCannotBeReachedDoesNotEndTheMeeting(t *testing.T) {
	f, _, api, who := room(t)
	gone := f.daemonAt(shortTempDir(t), "gone", false) // a socket file with nobody behind it
	f.session("gone", shortTempDir(t), "idle", 0, true)
	who.Add("who", gone)
	who.Set("topic", "what to do about the dead one")
	who.Set("rounds", "1")
	v := convene(t, f, who)

	until(t, "the meeting to finish", func() bool { return len(read(t, f, v.ID).Tasks) == 3 })
	got := read(t, f, v.ID)
	// Whatever the console calls the companion behind that socket — the point is that it names it.
	dead := got.Speakers[2].Name
	if got.Trouble == "" || !strings.Contains(got.Trouble, dead) {
		t.Errorf("the screen does not say which participant could not be reached: %q", got.Trouble)
	}
	if len(api.heard()) == 0 {
		t.Error("the rest of the room stopped when one participant could not be reached")
	}
	var silent bool
	for _, line := range got.Said {
		if line.Who == dead && line.Pass {
			silent = true
		}
	}
	if !silent {
		t.Errorf("the unreachable participant left no trace in the transcript: %+v", got.Said)
	}
}

// The floor rules live in core, and this console asks them rather than keeping its own copy.
//
// A second implementation of "who speaks next" is the thing that would make the screen and the
// driver disagree — and the screen is the one a person would believe.
func TestTheConsoleKeepsNoSecondCopyOfTheOrder(t *testing.T) {
	b, err := os.ReadFile("meet.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, own := range []string{"Passes >= 2", "Passes++", "Round++"} {
		if strings.Contains(src, own) {
			t.Errorf("meet.go decides %q for itself instead of asking the meeting package", own)
		}
	}
	if !strings.Contains(src, "peek.Next()") {
		t.Error("the screen no longer asks the rules who is next")
	}
}

// A meeting is not readable by somebody who may not see the companions in it.
//
// The list and the room both answer with a whole transcript — every participant's words, their
// names and the sockets they answer on. A scope that stops somebody from opening a companion and
// then hands them everything that companion said in a meeting is not a scope; it is a longer way
// round. Both are refused as "no such meeting", because a caller who learns one exists has already
// learned the thing being kept from them.
func TestAMeetingIsOnlyReadableByPeopleWhoMaySeeTheRoom(t *testing.T) {
	f, _, _, who := room(t)
	convene(t, f, url.Values{"topic": {"who owns the retry budget"}, "who": who["who"]})

	// Narrowed to one of the two companions in that meeting.
	f.srv.userHeader = "X-Forwarded-User"
	if err := os.WriteFile(filepath.Join(f.cfgDir, config.AuthFile), []byte(`
[people."kim"]
role = "operator"
companions = ["design"]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := config.LoadAuth(f.cfgDir)
	if err != nil {
		t.Fatal(err)
	}
	f.srv.policy = p

	as := func(method, path string, body url.Values) *httptest.ResponseRecorder {
		var r *http.Request
		if method == http.MethodPost {
			r = httptest.NewRequest(method, path, strings.NewReader(body.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.Header.Set("X-Forwarded-User", "kim")
		w := httptest.NewRecorder()
		switch {
		case strings.HasPrefix(path, "/meet-say"):
			f.srv.meetSay(w, r)
		case strings.HasPrefix(path, "/meet-close"):
			f.srv.meetClose(w, r)
		default:
			f.srv.meet(w, r)
		}
		return w
	}

	// The list says nothing about it.
	w := as(http.MethodGet, "/meet", nil)
	if strings.Contains(w.Body.String(), "retry budget") || strings.Contains(w.Body.String(), "api") {
		t.Errorf("the list hands over a meeting about somebody else's companion: %s", w.Body.String())
	}
	// And the room cannot be opened, spoken into, or ended.
	id := f.srv.meets.list(nil)[0].ID
	if w := as(http.MethodGet, "/meet?id="+url.QueryEscape(id), nil); w.Code != http.StatusNotFound {
		t.Errorf("reading it replied %d: %s", w.Code, w.Body.String())
	}
	for _, path := range []string{"/meet-say", "/meet-close"} {
		w := as(http.MethodPost, path, url.Values{"id": {id}, "text": {"hello"}})
		if w.Code != http.StatusNotFound {
			t.Errorf("%s replied %d: %s", path, w.Code, w.Body.String())
		}
	}
	// The operator with no narrowing still sees it, or this test would pass with the whole screen
	// broken.
	if got := f.srv.meets.list(func(run *meetingRun) bool { return true }); len(got) != 1 {
		t.Fatalf("the meeting is not there at all: %v", got)
	}
}

// One console will not hold an unbounded number of meetings.
//
// Each is a goroutine and, expensively, a model turn per participant per round — so a loop of
// posts is not a nuisance, it is every companion on the machine dragged into a dozen discussions
// at once with the backend paying for all of them. The permission is already `prompt`; this is the
// mistake and the stuck script, which cost the same as malice.
func TestAConsoleWillNotHoldUnlimitedMeetings(t *testing.T) {
	f, design, _, who := room(t)
	design.slow = make(chan struct{}) // nothing finishes, so they all stay open
	defer close(design.slow)

	for i := 0; i < meetsAtOnce; i++ {
		w := post(t, f.srv, f.srv.meet, "/meet",
			url.Values{"topic": {"one"}, "who": who["who"]})
		if w.Code != http.StatusOK {
			t.Fatalf("meeting %d was refused early: %d %s", i+1, w.Code, w.Body.String())
		}
	}
	w := post(t, f.srv, f.srv.meet, "/meet", url.Values{"topic": {"one too many"}, "who": who["who"]})
	if w.Code != http.StatusConflict {
		t.Fatalf("the %dth meeting replied %d: %s", meetsAtOnce+1, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "wrap one up") {
		t.Errorf("the refusal does not say what to do about it: %s", w.Body.String())
	}
}

// What a meeting carries into everybody's prompt is bounded, and so is the room.
//
// The topic is put in front of every participant on every turn and written into each of their
// session logs; a person's contribution is the same. Unclipped, a pasted file is not a question —
// it is that file paid for once per speaker per round, in every workspace. The seat count is the
// same arithmetic from the other side.
func TestAMeetingCannotCarryUnboundedTextOrUnboundedSeats(t *testing.T) {
	f, design, _, who := room(t)
	design.slow = make(chan struct{})
	defer close(design.slow)

	// Fixed numbers on both sides, not the constants being tested: an assertion written against
	// meetSaid passes however large somebody makes meetSaid, which is the change it exists to
	// catch. Twenty thousand in, eight thousand tolerated — whatever the cap is set to, it belongs
	// well under that.
	const sent, tolerated = 20000, 8000
	huge := strings.Repeat("x", sent)
	v := convene(t, f, url.Values{"topic": {huge}, "who": who["who"]})
	if len(v.Topic) > tolerated {
		t.Errorf("the topic went through at %d characters", len(v.Topic))
	}
	w := post(t, f.srv, f.srv.meetSay, "/meet-say", url.Values{"id": {v.ID}, "text": {huge}})
	if w.Code != http.StatusOK {
		t.Fatalf("saying it replied %d: %s", w.Code, w.Body.String())
	}
	said := read(t, f, v.ID).Said
	if len(said) != 1 || len(said[0].Text) > tolerated {
		t.Errorf("what the person said went in at %d characters", len(said[0].Text))
	}

	// And a room bigger than anybody reads is refused rather than dialled.
	// Distinct companions, because duplicates collapse into one seat on the way in. Twelve, for
	// the same reason the numbers above are fixed: the cap belongs below it, wherever it is set.
	const tooMany = 12
	seats := url.Values{"topic": {"everything"}}
	for i := 0; i < tooMany; i++ {
		wd := shortTempDir(t)
		sock := f.liveDaemonAs(t, wd, fmt.Sprintf("s%d", i), &talker{recordingEngine: &recordingEngine{}},
			daemon.Identity{Name: fmt.Sprintf("c%d", i)})
		f.session(fmt.Sprintf("s%d", i), wd, "idle", 0, true)
		seats.Add("who", sock)
	}
	if w := post(t, f.srv, f.srv.meet, "/meet", seats); w.Code != http.StatusBadRequest {
		t.Errorf("a meeting of %d replied %d: %s", tooMany, w.Code, w.Body.String())
	}
}
