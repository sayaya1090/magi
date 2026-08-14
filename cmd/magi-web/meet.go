package main

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/meeting"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/core/text"
)

// Companions talking to each other, run from here.
//
// # Why the console holds the floor
//
// Somebody has to say who speaks next, and it cannot be one of the participants: a companion that
// also decided the order would be chairing a discussion it is arguing in. The console is the one
// party in the room with nothing to say, which is what makes it the right one to keep the order.
//
// # Why it runs on its own
//
// Driven by a goroutine here rather than by the page. A meeting is several minutes of model turns;
// a browser tab closed halfway would otherwise leave three companions having half-discussed
// something with nobody left to collect the result. The page polls, and may be closed.
//
// # Why it is only in memory
//
// It lives as long as this console does, and no longer. The record that outlives the meeting is
// the one each participant keeps in its own session log, written by the participants themselves.
// A second copy here would be a second answer to "what was said", and the two would disagree the
// first time a console was restarted mid-meeting.
type meetings struct {
	mu   sync.Mutex
	open map[string]*meetingRun
}

// meetingRun is one meeting, plus what is needed to keep it going.
type meetingRun struct {
	mu      sync.Mutex
	id      string
	m       *meeting.Meeting
	sockets map[string]string // speaker name -> socket
	tasks   []meeting.Task
	// held is set while the person has the floor: nobody else is asked to speak until they let it
	// go, which is the hush a room gives somebody who has started talking.
	held bool
	// heldAt is when they took it. A hold is a keystroke, and the thing that ends it is sending —
	// so a tab closed mid-sentence, or a stray press in the box, left the room silent for as long
	// as the console stayed up. Watched live: the floor was taken, the sentence never arrived, and
	// three companions waited on somebody who had walked away.
	heldAt time.Time
	// trouble is the last thing that went wrong, kept to be shown rather than swallowed: a
	// participant whose daemon has gone is a fact about the meeting, not a reason to end it.
	trouble string
	// collecting is set while the closing round is being asked, so the screen can say why the
	// tasks are appearing one at a time instead of looking stuck.
	collecting bool
	stop       context.CancelFunc
}

// meetView is what the screen reads: everything about one meeting, in one answer.
type meetView struct {
	ID     string `json:"id"`
	Topic  string `json:"topic"`
	Round  int    `json:"round"`
	Max    int    `json:"max"`
	Holder string `json:"holder,omitempty"`
	Held   bool   `json:"held,omitempty"`
	Closed bool   `json:"closed,omitempty"`
	// Spent marks a meeting the backstop ended rather than the room: a discussion that converged
	// and one that ran out of laps are different outcomes, and the screen says which.
	Spent      bool          `json:"spent,omitempty"`
	Collecting bool          `json:"collecting,omitempty"`
	Trouble    string        `json:"trouble,omitempty"`
	Speakers   []meetSpeaker `json:"speakers"`
	Said       []meetLine    `json:"said"`
	Tasks      []meetTask    `json:"tasks,omitempty"`
}

// meetTask is one participant's conclusion on the wire.
//
// The core type would serialise as Who/What — its fields carry no tags, and they should not: a
// domain type that knows how a browser spells things is a domain type with a transport in it. The
// mapping lives here, where the rest of this screen's wire shape is written.
type meetTask struct {
	Who  string `json:"who"`
	What string `json:"what,omitempty"`
}

type meetSpeaker struct {
	Name   string `json:"name"`
	Socket string `json:"socket,omitempty"`
	Person bool   `json:"person,omitempty"`
	// Passes is how many times in a row this one has read the room and had nothing to add. At two
	// it stops being asked until somebody names it — which the screen says, rather than leaving a
	// reader to wonder why one companion has gone quiet.
	Passes int `json:"passes,omitempty"`
	// Next marks whoever is being asked right now, so the roster shows where the token is instead
	// of making somebody read to the end of the transcript to work out whose turn it is.
	Next bool `json:"next,omitempty"`
}

type meetLine struct {
	Who   string `json:"who"`
	Text  string `json:"text,omitempty"`
	Pass  bool   `json:"pass,omitempty"`
	Round int    `json:"round"`
	At    string `json:"at"`
}

// meetRounds is the backstop, not a plan: what ends a meeting is the room running out of things
// to say. See meeting.MaxRounds. The console picks it rather than asking, because a convener
// choosing a number is a convener guessing, before the discussion, how long the discussion needs
// to be — and every lap costs one model turn per participant, so the guess is expensive in both
// directions. A caller may still name one; the screen does not.
const meetRounds = 5

// heldFor is how long the room waits for somebody who took the floor and has not said anything.
const heldFor = 90 * time.Second

// meetSeats is the most companions one meeting will ask, and meetSaid the most text one question
// or one person's contribution may carry into everybody else's prompt.
const (
	meetSeats = 8
	meetSaid  = 4000
)

// meet answers with one meeting, or with the ones this console is holding. POST convenes.
func (s *server) meet(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.meetStart(w, r)
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeJSON(w, "meetings", s.meets.list(func(run *meetingRun) bool { return s.mayWatch(r, run) }))
		return
	}
	run := s.meets.get(id)
	if run == nil || !s.mayWatch(r, run) {
		// The same answer either way. A caller who learns that a meeting exists but is none of
		// their business has learned the thing the scope was keeping from them — which on this
		// screen is a list of somebody else's companions and everything they said.
		http.Error(w, "no meeting by that name here — it ended with the console that held it",
			http.StatusNotFound)
		return
	}
	writeJSON(w, "meeting", run.view())
}

// mayWatch reports whether this person may see a meeting at all.
//
// Every companion in the room, not any of them: what a meeting hands back is one transcript with
// all of them talking in it, and a scope that admits a reader because ONE participant is theirs
// hands them the other three's words as well. There is no way to show half a discussion.
//
// The gate at the front cannot do this — the meeting is named in the query and its participants
// are only known here — which is the same third shape the scope check has everywhere else: a
// route whose ANSWER carries the subject. See onlySeen.
func (s *server) mayWatch(r *http.Request, run *meetingRun) bool {
	if run == nil {
		return false
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	for _, sp := range run.m.Speakers {
		if sp.Person() {
			continue
		}
		if !s.seen(r, sp.Name, "") {
			return false
		}
	}
	return true
}

// meetStart convenes one: a question, and who is being asked.
func (s *server) meetStart(w http.ResponseWriter, r *http.Request) {
	// Clipped, like everything else this console passes on. The topic is put in front of every
	// participant on every turn and written into each of their session logs, so a pasted file is
	// not a question — it is the same file paid for once per speaker per round.
	topic := text.Clip(strings.TrimSpace(r.FormValue("topic")), meetSaid)
	if topic == "" {
		http.Error(w, "a meeting needs a question to be about", http.StatusBadRequest)
		return
	}
	// Participants are named by socket, and each is resolved against what this console publishes
	// and what this person may see. The gate at the front cannot do it: the subject is in the body,
	// so the check there passes with no companion named — the same shape /dispatch deals with.
	var speakers []meeting.Speaker
	sockets := map[string]string{}
	for _, sock := range r.Form["who"] {
		sock = strings.TrimSpace(sock)
		if sock == "" {
			continue
		}
		name := s.companionName(r, sock)
		if name == "" {
			http.Error(w, "this console has no companion you may convene at "+sock,
				http.StatusBadRequest)
			return
		}
		if _, twice := sockets[name]; twice {
			continue
		}
		speakers = append(speakers, meeting.Speaker{Name: name, Socket: sock})
		sockets[name] = sock
	}
	// And a ceiling on the room. Every participant is a model turn per round, so a request naming
	// forty companions is forty turns a lap — and a discussion nobody can read is not the thing
	// this feature is for either.
	if len(speakers) > meetSeats {
		http.Error(w, "a meeting of more than "+strconv.Itoa(meetSeats)+" companions is a broadcast; "+
			"pick the ones whose answer you need", http.StatusBadRequest)
		return
	}
	if len(speakers) < 2 {
		// One companion and a person is not a meeting, it is a conversation — and the companion's
		// own page does that better than anything here could.
		http.Error(w, "a meeting needs at least two companions; with one it is a conversation, "+
			"and its own page does that better", http.StatusBadRequest)
		return
	}
	// The person is in the room, last in the roster and never asked: they speak when they want to.
	speakers = append(speakers, meeting.Speaker{Name: s.personHere(r)})
	rounds := meetRounds
	if n, err := strconv.Atoi(strings.TrimSpace(r.FormValue("rounds"))); err == nil && n > 0 && n <= 9 {
		rounds = n
	}
	// The same question, to the same room, twice. A press that seems to do nothing — a slow answer,
	// a reader who went back to the list and pressed again — should land on the meeting they
	// already started rather than starting a second one beside it. Answering with the existing one
	// makes the second press mean what the person meant by it.
	if going := s.meets.same(topic, sockets); going != nil {
		writeJSON(w, "meeting", going.view())
		return
	}
	run, err := s.meets.start(topic, speakers, sockets, rounds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	// Background, not the request's context: the meeting outlives the POST that convened it, and
	// tying it to the browser would end it the moment somebody navigated away.
	go run.drive(context.Background(), s)
	writeJSON(w, "meeting", run.view())
}

// meetSay is the person speaking, or taking the floor before they have.
//
// Taking it is what quietens the room: nobody else is asked while somebody is typing. Saying
// something gives it back, so whoever goes next reads what was just said rather than answering the
// moment before it.
func (s *server) meetSay(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	run := s.meets.get(strings.TrimSpace(r.FormValue("id")))
	if run == nil || !s.mayWatch(r, run) {
		http.Error(w, "no meeting by that name here", http.StatusNotFound)
		return
	}
	said := text.Clip(strings.TrimSpace(r.FormValue("text")), meetSaid)
	run.mu.Lock()
	defer run.mu.Unlock()
	me := run.personName(s.personHere(r))
	// Calling on somebody by pressing their name. It is the same act as writing "@ops" into a
	// sentence — the rules already have it — and on a screen where the roster is a row of chips it
	// is the obvious thing to do to one. A companion that had passed twice and stopped being asked
	// comes back this way, which is the only way back.
	if call := strings.TrimSpace(r.FormValue("call")); call != "" {
		if call == me {
			// Pressing your own chip is taking the floor, which is what the box below does when you
			// start typing in it. One act, two ways to reach it.
			run.held = true
		}
		run.m.Take(call)
		writeJSON(w, "meeting", run.viewLocked())
		return
	}
	if said == "" {
		// A keystroke, not a sentence: the floor, without words yet.
		run.held = r.FormValue("hold") != ""
		run.heldAt = time.Now()
		if run.held {
			run.m.Take(me)
		} else {
			run.m.Yield()
		}
		writeJSON(w, "meeting", run.viewLocked())
		return
	}
	run.m.Say(meeting.Utterance{Who: me, Text: said})
	run.held = false
	writeJSON(w, "meeting", run.viewLocked())
}

// meetClose ends the discussion, which is the convener saying it is time to write down who does
// what. The closing round runs on the driver, so this answers straight away.
func (s *server) meetClose(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	run := s.meets.get(strings.TrimSpace(r.FormValue("id")))
	if run == nil || !s.mayWatch(r, run) {
		http.Error(w, "no meeting by that name here", http.StatusNotFound)
		return
	}
	run.mu.Lock()
	run.m.Close()
	run.held = false
	run.mu.Unlock()
	writeJSON(w, "meeting", run.view())
}

// meetHand sends one of the meeting's conclusions to the companion whose conclusion it is.
//
// A person presses this, not the meeting. Three workspaces starting to change the moment a
// discussion ends is exactly what the read-only rule in the meeting package is there to prevent:
// deciding and doing are separate, and this is the seam between them.
func (s *server) meetHand(w http.ResponseWriter, r *http.Request) {
	if postOnly(w, r) {
		return
	}
	run := s.meets.get(strings.TrimSpace(r.FormValue("id")))
	if run == nil || !s.mayWatch(r, run) {
		http.Error(w, "no meeting by that name here", http.StatusNotFound)
		return
	}
	who := strings.TrimSpace(r.FormValue("who"))
	run.mu.Lock()
	var what string
	for _, t := range run.tasks {
		if t.Who == who {
			what = t.What
		}
	}
	socket, topic := run.sockets[who], run.m.Topic
	run.mu.Unlock()
	if what == "" || socket == "" {
		http.Error(w, "there is nothing for "+who+" to do in that meeting", http.StatusBadRequest)
		return
	}
	// Asked again now the subject is known: the socket came out of the meeting, but whether THIS
	// person may give that companion work is a question about this request.
	if s.companionName(r, socket) == "" {
		http.Error(w, "you may not give work to "+who, http.StatusForbidden)
		return
	}
	in, err := daemon.Find(s.cfgDir, socket)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	cl, err := s.clientFor(socket)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	// Steer rather than Submit, like every other way work reaches a companion from here: whether a
	// turn is already running is the engine's to know.
	//
	// The meeting is named in the text because a person reading that session next week needs to
	// know where the work came from, and the meeting's transcript is not in their log.
	err = cl.Steer(r.Context(), command.SubmitPrompt{
		SessionID: session.SessionID(in.Session),
		Parts: []session.Part{{Kind: session.PartText,
			Text: "From the meeting about " + topic + ":\n\n" + what}},
	})
	if err != nil {
		http.Error(w, err.Error(), daemonSaysNo(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// drive is the meeting happening: ask whoever is next, record what they said, repeat.
//
// It stops for a person who is typing, and for nothing else. A participant that cannot be reached
// is recorded as a pass with the reason — a companion whose machine went down should not end the
// meeting the other three are having.
func (run *meetingRun) drive(ctx context.Context, s *server) {
	ctx, cancel := context.WithCancel(ctx)
	run.mu.Lock()
	run.stop = cancel
	run.mu.Unlock()
	defer cancel()
	for {
		if ctx.Err() != nil {
			return
		}
		run.mu.Lock()
		// A hold that nobody is using any more. Long enough that somebody composing a sentence is
		// never cut off, short enough that a closed tab does not end the meeting by accident.
		if run.held && !run.heldAt.IsZero() && time.Since(run.heldAt) > heldFor {
			run.held = false
			run.m.Yield()
		}
		if run.held {
			run.mu.Unlock()
			// Somebody is typing. Half a second is short enough that the room does not feel frozen
			// and long enough that a person mid-sentence is not raced by a model turn.
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		next, ok := run.m.Next()
		topic, transcript := run.m.Topic, run.m.Transcript()
		sock := run.sockets[next.Name]
		if ok {
			// The floor is TAKEN for the length of the turn, not handed over at the end of it.
			// Watched live, the roster never lit anybody: Holder was set by speaking and cleared by
			// the same call, so the one moment worth showing — a companion composing, for the
			// minute it takes — was the moment the screen showed nothing at all.
			run.m.Take(next.Name)
		}
		run.mu.Unlock()
		if !ok {
			run.collect(ctx, s, topic)
			return
		}
		said, passed, err := s.speakTo(ctx, sock, run.id, topic, transcript, false)
		run.mu.Lock()
		if err != nil {
			run.trouble = next.Name + ": " + err.Error()
			run.m.Say(meeting.Utterance{Who: next.Name, Pass: true, Text: "could not be reached"})
		} else {
			run.m.Say(meeting.Utterance{Who: next.Name, Text: said, Pass: passed})
		}
		// Recording an utterance moves the floor on, which is right for the speaker and wrong for
		// somebody who started typing while that speaker was still composing: their hold would be
		// silently dropped by a sentence that was already in flight when they took it. The room is
		// still hushed — the screen has to keep saying so.
		if run.held {
			run.m.Take(run.personName(""))
		}
		run.mu.Unlock()
	}
}

// collect asks each companion what it will do about what was just discussed.
func (run *meetingRun) collect(ctx context.Context, s *server, topic string) {
	run.mu.Lock()
	run.collecting = true
	transcript := run.m.Transcript()
	who := append([]meeting.Speaker(nil), run.m.Speakers...)
	run.mu.Unlock()
	for _, sp := range who {
		if sp.Person() {
			continue
		}
		said, passed, err := s.speakTo(ctx, run.sockets[sp.Name], run.id, topic, transcript, true)
		run.mu.Lock()
		switch {
		case err != nil:
			run.trouble = sp.Name + ": " + err.Error()
			run.tasks = append(run.tasks, meeting.Task{Who: sp.Name})
		case passed || strings.TrimSpace(said) == "":
			// Nothing to do is an outcome, and it is recorded as one. A participant left off the
			// list entirely would read as one nobody got round to asking.
			run.tasks = append(run.tasks, meeting.Task{Who: sp.Name})
		default:
			run.tasks = append(run.tasks, meeting.Task{Who: sp.Name, What: strings.TrimSpace(said)})
		}
		run.mu.Unlock()
	}
	run.mu.Lock()
	run.collecting = false
	run.mu.Unlock()
}

// speakTo asks one companion for one contribution.
//
// Its own connection, dialled and dropped: a meeting turn is minutes of model time and the pooled
// client serialises everything sent to that daemon, so holding it would stall every dashboard poll
// for that companion behind a sentence somebody is composing.
func (s *server) speakTo(ctx context.Context, socket, id, topic, transcript string, closing bool) (
	string, bool, error) {
	if strings.TrimSpace(socket) == "" {
		return "", false, errNoDoor
	}
	cl, err := daemon.Dial(socket)
	if err != nil {
		return "", false, err
	}
	defer cl.Close()
	type answer struct {
		said string
		pass bool
		err  error
	}
	done := make(chan answer, 1)
	go func() {
		said, pass, err := cl.Meet(id, topic, transcript, closing)
		done <- answer{said, pass, err}
	}()
	// Bounded, a little above the daemon's own limit on a contribution. The companion stops itself
	// after five minutes; this is for the case where it never gets that far — a wedged process
	// that accepts a connection and answers nothing would otherwise hold the floor for the life of
	// the console, with the meeting frozen behind it and no way to end it.
	late := time.NewTimer(6 * time.Minute)
	defer late.Stop()
	select {
	case a := <-done:
		return a.said, a.pass, a.err
	case <-late.C:
		return "", false, errText("no answer in six minutes")
	case <-ctx.Done():
		return "", false, ctx.Err()
	}
}

// companionName is what this console calls the companion at a socket — and "" when it publishes
// none there, or when this person may not see it. Both are the same answer on purpose: a caller
// who learns that a socket exists but is out of their scope has learned the thing the scope hides.
func (s *server) companionName(r *http.Request, socket string) string {
	for _, a := range s.published(r) {
		if a.Socket != socket || a.Elsewhere {
			continue
		}
		if !s.seen(r, a.Name, a.Peer) {
			return ""
		}
		return a.Name
	}
	return ""
}

// personHere is what to call the reader in a transcript: the name a gateway gave, or "you" on the
// one-operator console, which is the truthful thing to write when nothing here knows their name.
func (s *server) personHere(r *http.Request) string {
	if who := strings.TrimSpace(s.whoFrom(r)); who != "" {
		return who
	}
	return "you"
}

// meetsAtOnce is how many meetings one console will hold.
//
// Each one is a goroutine and, far more expensively, a model turn per participant per round: a
// loop of POSTs would put every companion on the machine into a dozen simultaneous discussions
// and leave the goroutines behind afterwards. The permission is already `prompt` — this is not
// about somebody who should not be here, it is about a mistake or a stuck script costing the
// whole fleet's backend. Finished meetings do not count against it and are dropped oldest-first
// once there are more than a screenful.
const meetsAtOnce = 6

// start makes a meeting, or says why it will not.
func (m *meetings) start(topic string, speakers []meeting.Speaker, sockets map[string]string,
	rounds int) (*meetingRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.open == nil {
		// Made on first use rather than at startup: the zero value has to be a working console,
		// because a server built without going through the constructor is what every test here is.
		m.open = map[string]*meetingRun{}
	}
	m.forgetOldLocked()
	going := 0
	for _, r := range m.open {
		if !r.finished() {
			going++
		}
	}
	if going >= meetsAtOnce {
		return nil, errText("this console is already holding " + strconv.Itoa(going) +
			" meetings; wrap one up before starting another")
	}
	run := &meetingRun{
		id:      meetID(time.Now(), len(m.open)),
		m:       meeting.New(topic, speakers, rounds),
		sockets: sockets,
	}
	m.open[run.id] = run
	return run, nil
}

// meetsKept is how many finished meetings stay readable. They are the record of what was decided
// until somebody acts on it — and each one holds a whole transcript, so they cannot all stay.
const meetsKept = 12

// forgetOldLocked drops the oldest finished meetings once there are more than that.
func (m *meetings) forgetOldLocked() {
	done := make([]string, 0, len(m.open))
	for id, r := range m.open {
		if r.finished() {
			done = append(done, id)
		}
	}
	if len(done) <= meetsKept {
		return
	}
	sort.Strings(done) // the id begins with the time it started, so this is oldest first
	for _, id := range done[:len(done)-meetsKept] {
		delete(m.open, id)
	}
}

// finished reports whether a meeting has run its course: closed, and the conclusions collected.
func (run *meetingRun) finished() bool {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.m.Closed && !run.collecting
}

// meetID names a meeting by when it started. The counter is the tiebreak for two convened in the
// same second, which is what a person clicking twice does.
func meetID(at time.Time, n int) string {
	return "m" + at.UTC().Format("20060102-150405") + "-" + strconv.Itoa(n)
}

// same is a meeting already going on the same question with the same companions, or nil.
func (m *meetings) same(topic string, sockets map[string]string) *meetingRun {
	m.mu.Lock()
	runs := make([]*meetingRun, 0, len(m.open))
	for _, r := range m.open {
		runs = append(runs, r)
	}
	m.mu.Unlock()
	for _, r := range runs {
		if r.finished() {
			continue // a question asked again after it was answered is a new meeting
		}
		r.mu.Lock()
		match := r.m.Topic == topic && len(r.sockets) == len(sockets)
		for name, sock := range r.sockets {
			if match && sockets[name] != sock {
				match = false
			}
		}
		r.mu.Unlock()
		if match {
			return r
		}
	}
	return nil
}

func (m *meetings) get(id string) *meetingRun {
	if id == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.open[id]
}

// list is the meetings this person may see. The filter is passed in rather than applied by the
// caller afterwards, so there is no moment when a full list exists to be handed to the wrong
// place — the same reason onlySeen copies rather than filtering in place.
func (m *meetings) list(may func(*meetingRun) bool) []meetView {
	m.mu.Lock()
	runs := make([]*meetingRun, 0, len(m.open))
	for _, r := range m.open {
		runs = append(runs, r)
	}
	m.mu.Unlock()
	out := make([]meetView, 0, len(runs))
	for _, r := range runs {
		if may != nil && !may(r) {
			continue
		}
		out = append(out, r.view())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (run *meetingRun) view() meetView {
	run.mu.Lock()
	defer run.mu.Unlock()
	return run.viewLocked()
}

func (run *meetingRun) viewLocked() meetView {
	out := meetView{
		ID: run.id, Topic: run.m.Topic, Round: run.m.Round, Max: run.m.MaxRounds,
		Holder: run.m.Holder, Held: run.held, Closed: run.m.Closed, Spent: run.m.Spent,
		Collecting: run.collecting,
		Trouble:    run.trouble, Speakers: []meetSpeaker{}, Said: []meetLine{},
	}
	for _, t := range run.tasks {
		out.Tasks = append(out.Tasks, meetTask{Who: t.Who, What: t.What})
	}
	for _, sp := range run.m.Speakers {
		out.Speakers = append(out.Speakers, meetSpeaker{
			Name: sp.Name, Socket: sp.Socket, Person: sp.Person(), Passes: sp.Passes,
			Next: sp.Name == run.upNext(),
		})
	}
	for _, u := range run.m.Said {
		out.Said = append(out.Said, meetLine{
			Who: u.Who, Text: u.Text, Pass: u.Pass, Round: u.Round,
			At: u.At.UTC().Format(time.RFC3339),
		})
	}
	return out
}

// upNext is who the driver will ask next, worked out from the rules rather than from a field
// somebody remembered to set — two answers to "whose turn is it" would disagree, and the one on
// the screen would be the wrong one.
//
// Asked of a COPY. Next advances the round when the current one is over, and a screen refresh must
// not move the meeting on: reading a room does not make anybody speak.
func (run *meetingRun) upNext() string {
	if run.m.Closed || run.held {
		return ""
	}
	peek := *run.m
	peek.Said = append([]meeting.Utterance(nil), run.m.Said...)
	peek.Speakers = append([]meeting.Speaker(nil), run.m.Speakers...)
	peek.Named = map[string]bool{}
	for k, v := range run.m.Named {
		peek.Named[k] = v
	}
	if sp, ok := peek.Next(); ok {
		return sp.Name
	}
	return ""
}

// personName is the roster's name for the reader, so a gateway that spells them differently from
// the way the meeting was convened does not add a second person to the room.
func (run *meetingRun) personName(me string) string {
	for _, sp := range run.m.Speakers {
		if sp.Person() {
			return sp.Name
		}
	}
	return me
}

// errNoDoor is a participant with no socket to dial. It should not happen, and if it does it is
// said plainly rather than reported as a model that had nothing to say.
var errNoDoor = errText("that participant has no door on this machine")

type errText string

func (e errText) Error() string { return string(e) }
