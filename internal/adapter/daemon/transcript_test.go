package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The door was designed around a method the engine already had. This pins that: if *app.App ever
// stops satisfying Transcriber, this fails to COMPILE, which is the loudest this can be said —
// and it is the reason the door needed no change in internal/app to land.
var _ Transcriber = (*app.App)(nil)

// transcriptEngine is an Engine that also reads out a conversation, the way App does: the persisted
// events after the cursor first, then whatever arrives live, down one channel.
type transcriptEngine struct {
	*fakeEngine
	mu sync.Mutex
	// log is what has been written. pending is what is "already live" at the moment somebody
	// subscribes — seeded into the live side before the first read, so a stream that put a live
	// event before the backlog would be caught without the test having to time anything.
	log     []event.Event
	pending []event.Event
	from    []int64 // the cursor each subscribe was actually made with
	closed  int     // how many times a subscriber's cancel was called
	newErr  error
}

func (e *transcriptEngine) Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error) {
	e.mu.Lock()
	e.from = append(e.from, fromSeq)
	var past []event.Event
	for _, ev := range e.log {
		if fromSeq > 0 && ev.Seq <= fromSeq {
			continue
		}
		past = append(past, ev)
	}
	live := make(chan event.Event, len(e.pending)+8)
	for _, ev := range e.pending {
		live <- ev
	}
	e.mu.Unlock()

	out := make(chan event.Event)
	go func() {
		defer close(out)
		for _, ev := range past {
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
		for {
			select {
			case ev := <-live:
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, func() {
		e.mu.Lock()
		e.closed++
		e.mu.Unlock()
	}, nil
}

func (e *transcriptEngine) NewSince(_ context.Context, _ session.SessionID, seq int64) (int64, bool, error) {
	if e.newErr != nil {
		return seq, false, e.newErr
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var last int64
	for _, ev := range e.log {
		if ev.Seq > last {
			last = ev.Seq
		}
	}
	if last <= seq {
		return seq, false, nil
	}
	return last, true, nil
}

func (e *transcriptEngine) cursors() []int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int64(nil), e.from...)
}

func (e *transcriptEngine) unsubscribed() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.closed
}

func ev(seq int64, text string) event.Event {
	return event.Event{
		Seq: seq, SessionID: "s1", Type: event.TypePartAppended,
		Data: []byte(`{"text":` + `"` + text + `"}`),
	}
}

// read runs a transcript and returns the first n events, the reset note if there was one, and
// whatever the stream ended with. It stops reading at n so a live stream does not hang the test.
func read(t *testing.T, c *Client, sid string, since int64, n int) (got []event.Event, note string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- c.Transcript(sid, since, func(why string) { note = why }, func(e event.Event) bool {
			got = append(got, e)
			return len(got) < n
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("transcript: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the transcript never delivered what was asked for")
	}
	return got, note
}

func hasCap(caps []string, want string) bool {
	for _, c := range caps {
		if c == want {
			return true
		}
	}
	return false
}

func seqs(evs []event.Event) []int64 {
	out := make([]int64, len(evs))
	for i, e := range evs {
		out[i] = e.Seq
	}
	return out
}

func sameSeqs(got []event.Event, want ...int64) bool {
	g := seqs(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

// The backlog comes first and the live events follow it, in one stream and in order. A live event is
// already waiting when the subscription opens, so a door that forwarded live before replay would be
// caught here rather than in whichever client noticed its transcript beginning in the middle.
func TestTranscriptReplaysBeforeItFollows(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{},
		log:     []event.Event{ev(1, "one"), ev(2, "two")},
		pending: []event.Event{ev(3, "three")}}
	c := start(t, eng)
	got, note := read(t, c, "s1", 0, 3)
	if !sameSeqs(got, 1, 2, 3) {
		t.Errorf("the stream arrived as %v, want the backlog 1,2 then the live 3", seqs(got))
	}
	if note != "" {
		t.Errorf("a cursor of 0 was second-guessed: %q", note)
	}
	if got[0].Type != event.TypePartAppended || string(got[0].Data) == "" {
		t.Errorf("the event crossed hollowed out: %+v", got[0])
	}
}

// Absent, zero and negative all mean everything — the same thing the store means by them. The
// console opens at -1 and resets to -1 when the conversation changes, and a door that read -1 as
// "nothing before that" would have made a reset show an empty session.
func TestTranscriptWithNoUsableCursorSendsEverything(t *testing.T) {
	for _, since := range []int64{0, -1, -99} {
		eng := &transcriptEngine{fakeEngine: &fakeEngine{}, log: []event.Event{ev(1, "one"), ev(2, "two")}}
		c := start(t, eng)
		got, note := read(t, c, "s1", since, 2)
		if !sameSeqs(got, 1, 2) {
			t.Errorf("since=%d gave %v, want the whole conversation", since, seqs(got))
		}
		if note != "" {
			t.Errorf("since=%d was announced as a refusal: %q", since, note)
		}
	}
}

// An absent `since` field on the wire is a zero, and a zero is everything. Sent as a raw request so
// the field really is missing rather than merely zero on this side.
func TestTranscriptWithTheFieldAbsentSendsEverything(t *testing.T) {
	var r Request
	if err := json.Unmarshal([]byte(`{"method":"transcript","session":"s1"}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.Since != 0 {
		t.Fatalf("an absent since decoded to %d, not 0", r.Since)
	}
	if got, _ := answerable(context.Background(), &transcriptEngine{fakeEngine: &fakeEngine{}}, "s1", r.Since); got != 0 {
		t.Errorf("an absent cursor was turned into %d", got)
	}
}

// A cursor the log CAN account for is honoured exactly: what comes after it, and nothing said about
// it. This is the ordinary reconnect, and it must stay quiet.
func TestTranscriptHonoursACursorInsideTheLog(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{},
		log: []event.Event{ev(1, "one"), ev(2, "two"), ev(3, "three")}}
	c := start(t, eng)
	got, note := read(t, c, "s1", 2, 1)
	if !sameSeqs(got, 3) {
		t.Errorf("since=2 gave %v, want only 3", seqs(got))
	}
	if note != "" {
		t.Errorf("a good cursor was refused: %q", note)
	}
	if cur := eng.cursors(); len(cur) != 1 || cur[0] != 2 {
		t.Errorf("the engine was subscribed at %v, want [2]", cur)
	}
}

// The case the door is careful about: a client reconnecting after a daemon restart still holds a
// number counted in a DIFFERENT conversation. Past the end of this one's log, it can name nothing —
// honouring it would send an empty replay and then live events, which on the client's screen is a
// conversation that started in the middle. So it is refused OUT LOUD and the whole thing is sent.
func TestTranscriptRefusesACursorPastTheEndAndSaysSo(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}, log: []event.Event{ev(1, "one"), ev(2, "two")}}
	c := start(t, eng)
	got, note := read(t, c, "s1", 40, 2)
	if !sameSeqs(got, 1, 2) {
		t.Errorf("a refused cursor gave %v, want the conversation from the start", seqs(got))
	}
	if note == "" {
		t.Fatal("the cursor was silently replaced — a client appending to what it had now shows the " +
			"start of the session stitched onto the end of it")
	}
	if !strings.Contains(note, "40") {
		t.Errorf("the note does not name the cursor it refused: %q", note)
	}
	if cur := eng.cursors(); len(cur) != 1 || cur[0] != 0 {
		t.Errorf("the engine was subscribed at %v, want [0]", cur)
	}
}

// A session with nothing in it is the same case: every positive cursor is past its end.
func TestTranscriptRefusesACursorIntoAnEmptyLog(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}, pending: []event.Event{ev(1, "first")}}
	c := start(t, eng)
	got, note := read(t, c, "s1", 7, 1)
	if note == "" {
		t.Error("a cursor into a log with no events was honoured in silence")
	}
	if !sameSeqs(got, 1) {
		t.Errorf("got %v, want the live event that followed", seqs(got))
	}
}

// If the cursor cannot be CHECKED, it is honoured rather than thrown away. Subscribe is about to
// read the same log and will refuse in words if it is unreadable; resending a whole conversation
// because a stat failed is a cost with no signal in it.
func TestAnUncheckableCursorIsLeftAlone(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}, newErr: context.DeadlineExceeded,
		log: []event.Event{ev(1, "one")}}
	since, note := answerable(context.Background(), eng, "s1", 40)
	if since != 40 || note != "" {
		t.Errorf("an uncheckable cursor was rewritten to %d (%q)", since, note)
	}
}

// An engine that cannot read out a transcript says so, in a sentence, and the connection stays
// usable — the refusal happens before anything is given over to a stream. A client that hung here
// would look exactly like a companion thinking.
func TestATranscriptlessDaemonRefusesInWords(t *testing.T) {
	c := start(t, &fakeEngine{})
	done := make(chan error, 1)
	go func() {
		done <- c.Transcript("s1", 0, nil, func(event.Event) bool { return true })
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a daemon with no transcript reported success")
		}
		var refused Refused
		if !errors.As(err, &refused) {
			t.Fatalf("the refusal did not arrive as one: %T %v", err, err)
		}
		if !strings.Contains(refused.Why, "transcript") {
			t.Errorf("the refusal does not say what was refused: %q", refused.Why)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon neither answered nor refused — a client cannot tell this from a slow turn")
	}
	// Still an ordinary exchange: the connection was never given over to a stream, so it still
	// answers. (What it answers is beside the point — this fake describes nothing; that the reply
	// comes back at all is what a stream would have made impossible.)
	var answered Refused
	if _, err := c.exchange(Request{Method: "about"}); err != nil && !errors.As(err, &answered) {
		t.Errorf("the refusal broke the connection: %v", err)
	}
}

// The peer hanging up ends the stream on the DAEMON's side too. Without a reader for the hang-up
// nothing would ever fail — a stream with nothing happening in it writes nothing — and the
// subscription would sit there until the daemon stopped.
func TestTranscriptEndsWhenThePeerHangsUp(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}, log: []event.Event{ev(1, "one")}}
	c := start(t, eng)
	first := make(chan struct{})
	go func() {
		var once sync.Once
		_ = c.Transcript("s1", 0, nil, func(event.Event) bool {
			once.Do(func() { close(first) })
			return true // keep listening: only the hang-up may end this
		})
	}()
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("the backlog never arrived")
	}
	c.Close()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if eng.unsubscribed() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the daemon held the subscription after the peer went away")
}

// A client has to be able to ASK, rather than call the method and read a sentence back: prose cannot
// tell "this build predates the door" from "this engine will not do it", and those are a client that
// should fall back and a client that should give up.
func TestTheHandshakeAdvertisesTheTranscript(t *testing.T) {
	if !hasCap(capsOf(&transcriptEngine{fakeEngine: &fakeEngine{}}), "transcript") {
		t.Error("a daemon that reads out transcripts did not say so")
	}
	if hasCap(capsOf(struct{ Engine }{}), "transcript") {
		t.Error("a daemon that cannot read one out advertised that it can")
	}
	if !strings.Contains(acceptedMethods(), "transcript") {
		t.Errorf("transcript missing from the accepted methods: %s", acceptedMethods())
	}
}
