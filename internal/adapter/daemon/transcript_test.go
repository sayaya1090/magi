package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
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

// **The stream says where the replay ends, and History stops there.**
//
// ⚠ This stream is a live tail: its own note says the peer hanging up is the only thing that ends a
// quiet one. So a reader wanting the conversation ONCE had nothing to stop at — measured 2026-09-12,
// the bridge's first rows door read a finished session for 202s and was killed. The marker is a frame
// with no event, which is what this stream already uses to talk about itself.
func TestHistoryStopsWhereTheReplayEnds(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{},
		log:     []event.Event{ev(1, "one"), ev(2, "two"), ev(3, "three")},
		pending: []event.Event{ev(4, "live, and must NOT be waited for")}}
	c := start(t, eng)

	done := make(chan struct{})
	var got []event.Event
	var err error
	go func() {
		got, err = c.History("s1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("History 가 안 끝난다 — 끝을 대는 표가 없으면 이 읽기는 대화가 끝난 세션에서도 영원하다")
	}
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if !sameSeqs(got, 1, 2, 3) {
		t.Errorf("기록을 %v 로 받았다 — 로그가 든 셋이어야 하고, 생중계는 기다리지 않아야 한다", seqs(got))
	}
}

// An empty log is ALREADY caught up, and that is the case most in need of being told: nothing will
// arrive to prompt it. A born-lazy current session looks exactly like this.
func TestHistoryOfAnEmptyLogEndsAtOnce(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}}
	c := start(t, eng)
	done := make(chan struct{})
	var got []event.Event
	var err error
	go func() {
		got, err = c.History("s1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("빈 로그에서 History 가 안 끝난다 — 아무것도 안 올 자리라 표가 유일한 소식이다")
	}
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("빈 로그에서 사건 %d 개를 받았다", len(got))
	}
}

// The marker comes AFTER the event that reached the end, never before it.
//
// A reader that stops at the marker would otherwise lose the newest line of the conversation it just
// asked for — and the loss is invisible, because every row it did get is correct.
func TestTheReplayMarkerFollowsTheLastEventItCovers(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}, log: []event.Event{ev(1, "one"), ev(2, "two")}}
	c := start(t, eng)
	got, err := c.History("s1")
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if !sameSeqs(got, 1, 2) {
		t.Fatalf("기록이 %v — 표가 마지막 사건보다 먼저 나오면 그 줄이 조용히 사라진다", seqs(got))
	}
}

// **A stream that ends without the marker is an error, not a short conversation.**
//
// This is the older companion seen from the wire: it answers the transcript request, writes the
// events, and closes — a clean end, and every frame valid. Returning what arrived would be
// indistinguishable from a complete read, so a screen would draw a truncated conversation as the
// whole one and nothing anywhere would say otherwise.
//
// ⚠ The first version of this test closed the CLIENT's connection instead, and the scanner's own
// error answered before the branch it meant to measure was reached — it passed with the check
// removed. A fake that hangs up on its own side is what actually exercises it.
func TestHistoryRefusesAStreamThatEndedWithoutSayingSo(t *testing.T) {
	dir, err := os.MkdirTemp(shortRoot(), "mgh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		if !sc.Scan() {
			return
		}
		// Two perfectly good frames and a clean hang-up. No marker, because this build has none.
		_, _ = io.WriteString(conn, `{"ok":true,"event":{"seq":1,"session":"s1","type":"part.appended","data":{"text":"one"}}}`+"\n")
		_, _ = io.WriteString(conn, `{"ok":true,"event":{"seq":2,"session":"s1","type":"part.appended","data":{"text":"two"}}}`+"\n")
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := c.History("s1")
	if err == nil {
		t.Errorf("표 없이 끝난 스트림을 완전한 읽기로 받았다(사건 %d 개) — 잘린 대화가 전부인 것처럼 그려진다", len(got))
	}
	if got != nil {
		t.Errorf("실패한 읽기가 행을 지을 사건을 함께 돌려줬다 — 부르는 쪽이 오류를 흘리면 그것이 화면에 선다: %v", seqs(got))
	}
}

// **The advertisement and the marker are one fact, so one test holds both.**
//
// ⚠ A mutation removing the `history` capability survived every other guard: the bridge's own tests
// dial a FAKE daemon whose caps are hand-written, so nothing there can notice the real one going
// quiet. And the cost of that drift is not a missing feature — it is a client that reads
// `transcript`, calls a read that waits for a marker nobody sends, and never returns. So the name
// and the behaviour are pinned together, in the direction a client uses them: advertised, therefore
// a read that ends.
func TestHistoryIsAdvertisedExactlyWhenTheReplayEndIsNamed(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}, log: []event.Event{ev(1, "one")}}
	if !hasCap(capsOf(eng), "history") {
		t.Fatal("전사를 읽어 주는 데몬이 `history` 를 안 광고한다 — 클라이언트는 `transcript` 만 보고 " +
			"끝을 기다리다 영영 안 돌아온다")
	}
	c := start(t, eng)
	done := make(chan struct{})
	var err error
	go func() {
		_, err = c.History("s1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("`history` 를 광고하면서 재생의 끝을 안 댄다 — 광고가 거짓이면 그것을 믿은 쪽이 매달린다")
	}
	if err != nil {
		t.Fatalf("History: %v", err)
	}

	// The other direction: a daemon that cannot read a transcript at all must not claim either name.
	if plain := capsOf(&fakeEngine{}); hasCap(plain, "history") || hasCap(plain, "transcript") {
		t.Errorf("전사를 못 읽는 데몬이 그 이름들을 광고한다: %v", plain)
	}
}

// **A cursor of 0 draws no note**, which is what makes History's reading of a note unambiguous.
//
// History always asks with 0, so the only thing a note can mean on its stream is "the end could not
// be named". That is a reading, and a reading needs something holding it up: this is it. If a
// since-0 stream ever starts carrying other notes, History's early return becomes wrong and this test
// is where it is caught.
func TestAnAnswerableZeroCursorIsSilent(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}, log: []event.Event{ev(1, "one"), ev(2, "two")}}
	for _, since := range []int64{0, -1, -99} {
		if got, note := answerable(context.Background(), eng, "s1", since); note != "" {
			t.Errorf("since=%d 에 안내가 붙었다(%q) — History 는 안내를 「끝을 못 댄다」로 읽는다", since, note)
		} else if got != since {
			t.Errorf("since=%d 가 %d 로 바뀌었다", since, got)
		}
	}
}

// **The end could not be read, so it is SAID — not left silent.**
//
// ⚠ My own two decisions contradicted each other: the door's comment said an unreadable head "costs
// the marker, not the stream", and History was written to wait for that marker. Together they made a
// one-shot read that never returns — and the bridge serves requests in order, so every later request
// waits behind it. The fix is that the stream says so, in the way it already talks about itself.
func TestHistoryFailsWhenTheDaemonCannotNameTheReplayEnd(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{},
		log:    []event.Event{ev(1, "one")},
		newErr: context.DeadlineExceeded}
	c := start(t, eng)
	done := make(chan struct{})
	var err error
	go func() {
		_, err = c.History("s1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("끝을 못 읽었는데 아무 말도 안 해서 읽기가 안 끝난다 — 브리지는 요청을 차례로 처리하므로 " +
			"뒤의 요청까지 이 하나에 걸린다")
	}
	if err == nil {
		t.Fatal("끝을 못 댄 스트림을 완전한 읽기로 받았다")
	}
	if !strings.Contains(err.Error(), "cannot say where the replay stops") {
		t.Errorf("실패가 사유를 안 나른다: %v", err)
	}
}

// **A reconnect that is already up to date gets the marker too.**
//
// The first version only said it for an EMPTY log, and missed the ordinary case: a client whose cursor
// is at the end has nothing to replay, so the loop that would have sent the marker never runs. The
// screen then stays on "catching up" until somebody types — which is exactly the ambiguity the marker
// was added to remove, surviving in the most common path.

// **A reconnect that is already up to date gets the marker too.**
//
// The first version said it only for an EMPTY log and missed the ordinary case: a client whose cursor
// sits at the end has nothing to replay, so the loop that would have sent the marker never runs. The
// screen then stays on "catching up" until somebody types — the very ambiguity the marker was added to
// remove, surviving in the most common path.
//
// ⚠ Read at the wire, because the marker is what is being measured. The first version of this test
// asserted "no events arrived and no note", and both are true with the fix REMOVED — it passed for the
// wrong reason. The first frame on this stream must BE the marker.
func TestTheMarkerComesWhenTheCursorIsAlreadyAtTheEnd(t *testing.T) {
	eng := &transcriptEngine{fakeEngine: &fakeEngine{}, log: []event.Event{ev(1, "one"), ev(2, "two")}}
	c := start(t, eng)

	first := make(chan string, 1)
	go func() {
		raw, err := c.Raw([]byte(`{"method":"transcript","session":"s1","since":2}`))
		if err != nil {
			first <- "!" + err.Error()
			return
		}
		first <- string(raw)
	}()
	select {
	case got := <-first:
		if strings.HasPrefix(got, "!") {
			t.Fatalf("재접속 스트림이 거절했다: %s", got[1:])
		}
		var resp Response
		if err := json.Unmarshal([]byte(got), &resp); err != nil {
			t.Fatalf("첫 프레임이 JSON 이 아니다: %s", got)
		}
		if resp.Event != nil {
			t.Fatalf("재생할 것이 없는데 사건이 왔다: %s", got)
		}
		if !resp.Live {
			t.Errorf("끝에 있는 커서로 다시 붙었는데 재생의 끝을 안 댄다 — 화면은 누가 입력할 때까지 "+
				"「불러오는 중」에 머문다: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("재접속 스트림이 프레임 하나도 안 준다 — 그것이 이 결함의 증상 그대로다")
	}
}

// **A companion that goes quiet without a word must not hold the caller for ever.**
//
// ⚠ The marker removes this for daemons from this build, and the note removes it for one that cannot
// read its log head — neither covers a peer that simply stops talking: an older build, a wedged
// engine, a relay that lost its far side. The connection stays OPEN, so nothing ends the read.
//
// This is the bound that was missing when the first rows door hung for 202s. The mutation that
// removed it survived every other guard here, because their fakes all hang up.
func TestHistoryDoesNotWaitForEverOnAQuietStream(t *testing.T) {
	was := historyIdle
	historyIdle = 200 * time.Millisecond
	t.Cleanup(func() { historyIdle = was })

	dir, err := os.MkdirTemp(shortRoot(), "mgq")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	held := make(chan struct{})
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		// Held open on purpose: one good frame, then silence. Closing here would end the read through
		// the scanner instead, which is the path the other tests already cover.
		defer func() { <-held; conn.Close() }()
		sc := bufio.NewScanner(conn)
		if !sc.Scan() {
			return
		}
		_, _ = io.WriteString(conn, `{"ok":true,"event":{"seq":1,"session":"s1","type":"part.appended","data":{"text":"one"}}}`+"\n")
		select {} // never another word
	}()
	t.Cleanup(func() { close(held) })

	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	done := make(chan struct{})
	go func() {
		_, err = c.History("s1")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("조용해진 스트림에 걸려 안 돌아온다 — 브리지가 요청을 차례로 처리하므로 뒤의 요청도 함께 멈춘다")
	}
	if err == nil {
		t.Fatal("한 프레임만 오고 조용해진 스트림을 완전한 읽기로 받았다")
	}
	if !strings.Contains(err.Error(), "never declared over") {
		t.Errorf("실패가 무엇을 기다렸는지 안 말한다: %v", err)
	}
}

// **The bound is on silence, not on the size of the conversation.**
//
// ⚠ A deadline set ONCE would cut off a long or slow replay mid-read, and every fixture here delivers
// instantly — so that mutation survived until this test existed. The failure it would cause is the
// worst kind for a transcript: a conversation that draws its first half and stops, with an error that
// blames the network.
//
// The gaps are each under the bound and the total is over it: the only way to pass is to reset.
func TestTheSilenceBoundDoesNotCutOffASlowReplay(t *testing.T) {
	was := historyIdle
	historyIdle = 250 * time.Millisecond
	t.Cleanup(func() { historyIdle = was })

	dir, err := os.MkdirTemp(shortRoot(), "mgs")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	const frames = 6 // 6 × 100ms = 600ms of streaming against a 250ms silence bound
	go func() {
		conn, aerr := ln.Accept()
		if aerr != nil {
			return
		}
		defer conn.Close()
		sc := bufio.NewScanner(conn)
		if !sc.Scan() {
			return
		}
		for i := 1; i <= frames; i++ {
			time.Sleep(100 * time.Millisecond)
			if _, werr := io.WriteString(conn, `{"ok":true,"event":{"seq":`+strconv.Itoa(i)+
				`,"session":"s1","type":"part.appended","data":{"text":"x"}}}`+"\n"); werr != nil {
				return
			}
		}
		_, _ = io.WriteString(conn, `{"ok":true,"live":true}`+"\n")
	}()

	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got, err := c.History("s1")
	if err != nil {
		t.Fatalf("천천히 오는 재생이 끊겼다 — 한도가 침묵이 아니라 읽기 전체에 걸려 있다: %v", err)
	}
	if len(got) != frames {
		t.Errorf("사건 %d 개를 받았다, %d 개여야 한다 — 전사가 앞 절반만 그려진다", len(got), frames)
	}
}
