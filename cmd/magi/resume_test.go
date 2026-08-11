package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// movable is a daemon that can be pointed at another of its own conversations.
//
// The real one is daemonEngine, which needs a workspace, a store, a socket and a published record.
// What Resume actually decides with is narrower — where it is, whether a turn is running, which
// sessions exist — so the fixture is those three and the record is a function, the same way
// production passes one.
type movable struct {
	at    *where
	work  *recordingWork
	known []session.SessionID
	// wrote is what the record was told, in order. A move that decides correctly and never says so
	// is a move nothing outside the process knows about.
	mu    sync.Mutex
	wrote []session.SessionID
	fail  error
}

func (m *movable) record(sid session.SessionID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.wrote = append(m.wrote, sid)
	return nil
}

func (m *movable) said() []session.SessionID {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]session.SessionID(nil), m.wrote...)
}

// resume is the production decision with the production locking, over this fixture.
//
// It mirrors daemonEngine.Resume deliberately: the sequence — refuse inside the lock, mark the
// conversation being left, then say where it went — is the thing under test, and a fixture that
// reordered it would be testing a different program.
func (m *movable) resume(ctx context.Context, ar *arrival, sid session.SessionID) error {
	return m.at.move(func(from session.SessionID) (session.SessionID, error) {
		if sid == from {
			return "", nil
		}
		if busy, running := m.work.Running(); running {
			return "", errBusy{busy}
		}
		found := false
		for _, k := range m.known {
			if k == sid {
				found = true
			}
		}
		if !found {
			return "", errUnknown{sid}
		}
		if err := ar.reader.NoteSessionMoved(ctx, from, sid); err != nil {
			return "", err
		}
		if err := m.record(sid); err != nil {
			return "", err
		}
		return sid, nil
	})
}

type errBusy struct{ sid session.SessionID }

func (e errBusy) Error() string { return "mid-turn in " + string(e.sid) }

type errUnknown struct{ sid session.SessionID }

func (e errUnknown) Error() string { return string(e.sid) + " is not a conversation of this workspace" }

func newMovable(ar *arrival, at session.SessionID, known ...session.SessionID) *movable {
	ar.work = &recordingWork{App: ar.reader, ar: ar, side: at + "_side"}
	// The store refuses an append into a session it has never seen opened, which is the rule that
	// keeps a typo from creating a conversation. These exist for real, as they do in production.
	for _, sid := range append([]session.SessionID{at}, known...) {
		ar.append(string(sid), ev(ar.t, event.TypeSessionCreated,
			event.SessionCreatedData{Workdir: "/w"}))
	}
	return &movable{at: newWhere(at), work: ar.work, known: append([]session.SessionID{at}, known...)}
}

// A companion mid-turn cannot leave the conversation it is speaking in.
//
// The console greys the control, which is a drawing. This is the same rule where it can be
// enforced — and it is checked INSIDE the lock, against the session the move is actually starting
// from, because a decision made against a value that can change under it is the defect rather than
// a detail of it.
func TestACompanionMidTurnDoesNotLeaveTheConversation(t *testing.T) {
	ar := newArrival(t)
	m := newMovable(ar, "a1", "a7")
	m.work.setBusy("a1")

	err := m.resume(context.Background(), ar, "a7")
	if err == nil {
		t.Fatal("it moved while a turn was running")
	}
	if m.at.now() != "a1" {
		t.Errorf("it is in %s — the refusal did not hold", m.at.now())
	}
	if len(m.said()) != 0 {
		t.Errorf("the record was rewritten anyway: %v", m.said())
	}
}

// A session from somewhere else is not a conversation this companion may be pointed at.
//
// The console names a session by id, and an id is a string: one from another workspace would have
// this daemon writing into a log it does not own, under a record claiming it as its own.
func TestACompanionRefusesASessionThatIsNotItsOwn(t *testing.T) {
	ar := newArrival(t)
	m := newMovable(ar, "a1", "a7")

	if err := m.resume(context.Background(), ar, "somebody-elses"); err == nil {
		t.Fatal("it accepted a session from another workspace")
	}
	if m.at.now() != "a1" {
		t.Errorf("it moved to %s anyway", m.at.now())
	}
}

// The conversation it leaves says so, and says where it went.
//
// Written into the OLD session: what a reader of it needs is the reason its transcript stops. The
// new one needs no mark — what happened there is that work carried on.
func TestTheConversationItLeavesIsMarked(t *testing.T) {
	ar := newArrival(t)
	m := newMovable(ar, "a1", "a7")

	if err := m.resume(context.Background(), ar, "a7"); err != nil {
		t.Fatal(err)
	}
	if m.at.now() != "a7" {
		t.Fatalf("it is in %s", m.at.now())
	}
	if got := m.said(); len(got) != 1 || got[0] != "a7" {
		t.Fatalf("the record was told %v", got)
	}

	evs, err := ar.store.Read(context.Background(), "a1", 0)
	if err != nil {
		t.Fatal(err)
	}
	var found *event.Event
	for i := range evs {
		if evs[i].Type == event.TypeSessionMoved {
			found = &evs[i]
		}
	}
	if found == nil {
		t.Fatal("the conversation it left has no mark — its transcript simply stops")
	}
	var d event.SessionMovedData
	if err := json.Unmarshal(found.Data, &d); err != nil {
		t.Fatal(err)
	}
	if d.To != "a7" {
		t.Errorf("the mark says it went to %q, so a reader cannot follow it", d.To)
	}
	// And nothing was written into the one it moved TO.
	to, err := ar.store.Read(context.Background(), "a7", 0)
	if err == nil {
		for _, e := range to {
			if e.Type == event.TypeSessionMoved {
				t.Error("the session it moved to was marked as well, which reads as leaving it")
			}
		}
	}
}

// Two moves at once do not each decide against the state the other is changing.
//
// Two consoles, or a console and a phone. Unserialised, both read the same starting session, both
// write a mark saying the companion left it, and the record ends up wherever the slower one
// finished — so the log claims two departures from one conversation and the roster names a session
// nobody chose last.
func TestTwoMovesAtOnceLeaveOneTrail(t *testing.T) {
	ar := newArrival(t)
	m := newMovable(ar, "a1", "a7", "a9")

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, to := range []session.SessionID{"a7", "a9"} {
		wg.Add(1)
		go func(i int, to session.SessionID) {
			defer wg.Done()
			errs[i] = m.resume(context.Background(), ar, to)
		}(i, to)
	}
	wg.Wait()

	// Both are legal moves, so both may succeed — what must not happen is two departures from one
	// conversation, or a record that names a session the last mark did not.
	marks := 0
	evs, _ := ar.store.Read(context.Background(), "a1", 0)
	for _, e := range evs {
		if e.Type == event.TypeSessionMoved {
			marks++
		}
	}
	if marks != 1 {
		t.Errorf("%d departures written into one conversation", marks)
	}
	said := m.said()
	if len(said) == 0 {
		t.Fatalf("nothing reached the record: %v", errs)
	}
	if said[len(said)-1] != m.at.now() {
		t.Errorf("the record says %s and the companion is in %s", said[len(said)-1], m.at.now())
	}
}

// A move that cannot be published did not happen.
//
// The record is what every reader believes — the fleet row, the console resolving which session to
// send to, a terminal attaching. If it could not be written, a companion that moved anyway would
// be somewhere nothing else can see.
func TestAMoveThatCannotBePublishedIsNotAMove(t *testing.T) {
	ar := newArrival(t)
	m := newMovable(ar, "a1", "a7")
	m.fail = errUnknown{"the record is not writable"}

	if err := m.resume(context.Background(), ar, "a7"); err == nil {
		t.Fatal("it reported success with nothing published")
	}
	if m.at.now() != "a1" {
		t.Errorf("it moved to %s with nothing published — no reader can find it there", m.at.now())
	}
}

// The record survives being read while it is written.
//
// Its readers POLL it — the console every three seconds, per companion — and it is rewritten
// whenever the queue depth changes or the companion moves. Truncated and rewritten in place, a
// reader landing in the middle gets an empty or half-written file and reports the companion as
// unreadable: a daemon blinking out of existence for no reason anybody could see afterwards.
func TestTheRecordIsNeverReadHalfWritten(t *testing.T) {
	dir := shortSockDir(t)
	sock := dir + "/daemon-x.sock"
	undo, err := daemon.Publish(sock, t.TempDir(), "a1", daemon.Identity{Name: "design"})
	if err != nil {
		t.Fatal(err)
	}
	defer undo()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	var bad int32
	var mu sync.Mutex
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			in, rerr := daemon.Published(sock)
			if rerr != nil || in.Name != "design" {
				mu.Lock()
				bad++
				mu.Unlock()
			}
		}
	}()
	for i := 0; i < 300; i++ {
		if werr := daemon.Announce(sock, i%5, i%2 == 0); werr != nil {
			t.Fatal(werr)
		}
		if werr := daemon.Moved(sock, session.SessionID([]string{"a1", "a7"}[i%2])); werr != nil {
			t.Fatal(werr)
		}
	}
	close(stop)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if bad > 0 {
		t.Errorf("%d reads saw a record that was not there or not whole", bad)
	}
}

// The two fields written by two different goroutines do not overwrite each other.
//
// The queue's depth comes from the drain and the session from a serve goroutine, and each is a
// read of the whole record, one change, and a write back. Overlapping, one of the two changes is
// lost — whichever finished first, silently.
func TestTheQueueDepthAndTheSessionDoNotLoseEachOther(t *testing.T) {
	dir := shortSockDir(t)
	sock := dir + "/daemon-y.sock"
	undo, err := daemon.Publish(sock, t.TempDir(), "a0", daemon.Identity{Name: "api"})
	if err != nil {
		t.Fatal(err)
	}
	defer undo()

	// Each writer counts UP, so the last value each wrote is known and an older one surviving is
	// the lost update itself rather than a value that might have been written either way round.
	const n = 150
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i <= n; i++ {
			_ = daemon.Announce(sock, i, true)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 1; i <= n; i++ {
			_ = daemon.Moved(sock, session.SessionID(fmt.Sprintf("a%d", i)))
		}
	}()
	wg.Wait()

	in, err := daemon.Published(sock)
	if err != nil {
		t.Fatal(err)
	}
	if in.Waiting != n {
		t.Errorf("the queue depth is %d and the last write was %d — a read-modify-write put back "+
			"a copy taken before it", in.Waiting, n)
	}
	if want := fmt.Sprintf("a%d", n); in.Session != want {
		t.Errorf("the session is %q and the last move was to %q — the same loss, the other way "+
			"round: a roster pointing at a conversation the companion has left", in.Session, want)
	}
	if !strings.Contains(in.Name, "api") {
		t.Errorf("a field nobody touched was lost: %+v", in)
	}
}
