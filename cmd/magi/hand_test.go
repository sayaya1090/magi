package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/cluster"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The far side of a crossing: a real companion, behind a real socket, answering the real protocol.
//
// The relay is deliberately a byte pipe, so a client dialling this socket and a client speaking
// through an ssh to it are holding the same conversation. Everything these tests check happens
// above the pipe, which is why there is no ssh anywhere in them and no gap where one would go.
type arrival struct {
	t      *testing.T
	cfgDir string
	store  *jsonl.Store
	reader *app.App
	// bus is the companion's own, held so a test can announce what it wrote to the store. The
	// engine does both; appending here and staying silent would leave a watch listening to
	// nothing, which is a test of the fixture and not of the door.
	bus    *bus.Bus
	said   int
	live   int32 // watches running inside this companion right now
	work   *recordingWork
	taking handover
	got    struct {
		sync.Mutex
		prompts []string
	}
	// carried is what the companion last said it was carrying: queued, and in hand.
	carried struct {
		sync.Mutex
		waiting  int
		handling bool
	}
	// load is what the companion wrote down about the work arriving, in order.
	load struct {
		sync.Mutex
		at []struct {
			full  bool
			ahead int
		}
	}
}

// noted is what this companion recorded about arriving work.
func (ar *arrival) noted() []struct {
	full  bool
	ahead int
} {
	ar.load.Lock()
	defer ar.load.Unlock()
	return append([]struct {
		full  bool
		ahead int
	}(nil), ar.load.at...)
}

func newArrival(t *testing.T) *arrival {
	t.Helper()
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b := bus.New()
	return &arrival{t: t, cfgDir: shortSockDir(t), store: st, bus: b,
		reader: app.New(st, nil, builtin.NewRegistry(), b, nil, app.Config{})}
}

// shortSockDir is a directory a unix socket can be bound in: the OS limit is about 104 bytes and a
// t.TempDir() under a long test name gets close.
func shortSockDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "mg")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// recordingWork is the companion's own store, with a Submit that records instead of running a turn.
//
// Everything a handover READS is real — where the log stands, what has been said, what the fleet
// makes of this companion. Only starting a turn is stubbed, because a turn needs a model and this
// is a test of the door.
type recordingWork struct {
	*app.App
	ar   *arrival
	side session.SessionID
	// mu guards busy and opened. The drain goroutine writes them while the test reads them, which
	// is the real shape of the thing being tested — one at a time is enforced ACROSS goroutines —
	// so an unguarded field here is a race in the fixture rather than a fact about the code. Found
	// by CI, whose test run has -race and whose local gate does not.
	mu     sync.Mutex
	busy   session.SessionID
	opened int
	// parked stands in for the person's own queued interjection: real state in the App, but
	// putting one there needs a running turn to interject into, and what is under test is the
	// ORDER — that a free workspace still is not this piece's turn.
	parked bool
}

// setPersonWaiting is the person having typed something while the agent worked.
func (w *recordingWork) setPersonWaiting(v bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.parked = v
}

func (w *recordingWork) PersonWaiting() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.parked
}

// setBusy and idle are what a turn starting and ending look like to this fake.
func (w *recordingWork) setBusy(sid session.SessionID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.busy = sid
}

// freeIf clears the busy marker when sid is the session that held it, and reports whether it did.
func (w *recordingWork) freeIf(sid string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if string(w.busy) != sid {
		return false
	}
	w.busy = ""
	return true
}

// CreateSession opens a real one in the store, a fresh id each time — the first is <sid>_side, so
// a test can name it. A stub handing back one fixed id would have made two askers look like one,
// which is the thing under test.
func (w *recordingWork) CreateSession(_ context.Context, c command.CreateSession) (session.SessionID, error) {
	w.mu.Lock()
	w.opened++
	n := w.opened
	w.mu.Unlock()
	id := w.side
	if n > 1 {
		id = session.SessionID(fmt.Sprintf("%s%d", w.side, n))
	}
	w.ar.append(string(id), ev(w.ar.t, event.TypeSessionCreated,
		event.SessionCreatedData{Workdir: c.Workdir}))
	return id, nil
}

// Running is what serialises the work. Nothing is running in these tests unless one says so.
func (w *recordingWork) Running() (session.SessionID, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.busy, w.busy != ""
}

// Submit records the prompt AND writes it to the log, which is what a real one does before it
// starts a turn. Recording only in memory left the log with a turn that finished without ever
// having been asked for — and "the first turn that finishes past here" does not recognise that.
func (w *recordingWork) Submit(_ context.Context, c command.SubmitPrompt) error {
	// Busy FIRST, before anything a test can watch for.
	//
	// A test learns the piece started by seeing its prompt, so every state this fake owes the
	// caller has to be true by the time the prompt is visible. Recorded first and marked busy
	// after, there is a window where the piece has visibly started and the workspace still looks
	// free: a test that finishes the turn in that window clears nothing, the marker is set a
	// moment later by the very call that was finishing, and the queue never moves again. Found as
	// a one-in-eight flake in TestAQueuedPieceGetsItsOwnAnswer, whose next piece waited out the
	// deadline. A real App has no such window — accepting the prompt and running the turn are the
	// same act there.
	w.setBusy(c.SessionID)
	var b strings.Builder
	for _, p := range c.Parts {
		b.WriteString(p.Text)
	}
	w.ar.got.Lock()
	w.ar.got.prompts = append(w.ar.got.prompts, b.String())
	w.ar.got.Unlock()
	w.ar.append(string(c.SessionID), ev(w.ar.t, event.TypePromptSubmitted,
		event.PromptSubmittedData{MessageID: "m_handed", Parts: c.Parts}))

	return nil
}

// takingEngine is a daemon that can be handed work: the ordinary engine surface, plus the handover
// that production wires in.
//
// live counts the watches currently running inside it. A watch that nobody is listening to writes
// nothing, so a dead connection cannot announce itself by a failing write — which makes "the watch
// noticed and stopped" invisible from outside the process, and untestable without this.
type takingEngine struct {
	handover
	live *int32
}

func (e takingEngine) Watch(ctx context.Context, receipt string, say func(daemon.Handover) error) error {
	atomic.AddInt32(e.live, 1)
	defer atomic.AddInt32(e.live, -1)
	return e.handover.Watch(ctx, receipt, say)
}

func (takingEngine) Submit(context.Context, command.SubmitPrompt) error             { return nil }
func (takingEngine) Steer(context.Context, command.SubmitPrompt) error              { return nil }
func (takingEngine) Interrupt(context.Context, command.Interrupt) error             { return nil }
func (takingEngine) Waiting(session.SessionID) (app.Ask, bool)                      { return app.Ask{}, false }
func (takingEngine) Doing(session.SessionID) (string, bool)                         { return "", false }
func (takingEngine) RespondQuestion(context.Context, command.RespondQuestion) error { return nil }
func (takingEngine) RespondPermission(context.Context, command.RespondPermission) error {
	return nil
}

// publish starts a companion here and gives it a finished turn, so it reads as idle. It returns a
// client speaking to it — the same client the relay carries, over a shorter pipe.
func (ar *arrival) publish(name, sid string) (*daemon.Client, *daemon.Receipts) {
	ar.t.Helper()
	wd := ar.t.TempDir()
	sock := filepath.Join(ar.cfgDir, "daemon-"+sid+".sock")
	receipts := daemon.NewReceipts()
	ar.work = &recordingWork{App: ar.reader, ar: ar, side: session.SessionID(sid + "_side")}
	eng := takingEngine{live: &ar.live, handover: handover{
		work: ar.work,
		at:   newWhere(session.SessionID(sid)), workdir: wd, configDir: ar.cfgDir, receipts: receipts,
		mine: newSideSessions(), queued: newWaiting(func(n int, handling bool) {
			ar.carried.Lock()
			defer ar.carried.Unlock()
			ar.carried.waiting, ar.carried.handling = n, handling
		}),
		note: func(full bool, ahead int) {
			ar.load.Lock()
			defer ar.load.Unlock()
			ar.load.at = append(ar.load.at, struct {
				full  bool
				ahead int
			}{full, ahead})
		},
	}}
	// Kept, so a test about ORDER can ask it to try now rather than wait out the backstop tick.
	ar.taking = eng.handover
	ctx, cancel := context.WithCancel(context.Background())
	ar.t.Cleanup(cancel)
	go eng.handover.run(ctx) // the daemon starts this; without it nothing leaves the queue
	go func() { _ = daemon.Serve(ctx, eng, sock) }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	unpublish, err := daemon.Publish(sock, wd, sid, daemon.Identity{Name: name})
	if err != nil {
		ar.t.Fatal(err)
	}
	ar.t.Cleanup(unpublish)
	// A turn that already finished, with something said in it. Both halves matter: the finish is
	// what makes the companion read as idle, and the answer is what a handover must not mistake for
	// its own.
	ar.append(sid, ev(ar.t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: wd}),
		ev(ar.t, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m0",
			Parts: []session.Part{{Kind: session.PartText, Text: "get set up"}}}))
	ar.finishes(sid, "already set up, before anybody handed anything over")

	cl, derr := daemon.Dial(sock)
	if derr != nil {
		ar.t.Fatal(derr)
	}
	ar.t.Cleanup(func() { cl.Close() })
	return cl, receipts
}

func (ar *arrival) append(sid string, evs ...event.Event) {
	ar.t.Helper()
	for _, e := range evs {
		seqs, err := ar.store.Append(context.Background(), session.SessionID(sid), e)
		if err != nil {
			ar.t.Fatal(err)
		}
		e.SessionID = session.SessionID(sid)
		if len(seqs) > 0 {
			e.Seq = seqs[0]
		}
		ar.bus.Publish(e)
	}
}

// finishes gives the companion an assistant answer and closes the turn, which is what the way back
// is watching for.
// Each call is its own message: parts sharing a MessageID are one message, and two answers glued
// into one would hide exactly the mistake these tests are looking for.
func (ar *arrival) finishes(sid, said string) {
	ar.t.Helper()
	ar.said++
	// The answer is in the log BEFORE the workspace is free, which is the order the real App ends
	// a turn in: runLoop writes the assistant text and turn.finished, and the run goroutine drops
	// the cancel — what Running() reads — on its way out afterwards (internal/app/app.go).
	//
	// Freed first, the drain could start the next piece and mark where its answer begins at a
	// point BEFORE this answer was written, so the next piece would collect this one's words. Seen
	// as a rare "the second piece was handed the answer to the first" — the exact confusion these
	// tests exist to catch, arriving from the fixture rather than from the code.
	ar.append(sid,
		ev(ar.t, event.TypePartAppended, event.PartAppendedData{
			MessageID: fmt.Sprintf("m%d", ar.said),
			Role:      session.RoleAssistant,
			Part:      session.Part{Kind: session.PartText, Text: said}}),
		ev(ar.t, event.TypeTurnFinished, event.TurnFinishedData{}))
	if ar.work != nil {
		ar.work.freeIf(sid) // the turn ended; the workspace is free again
	}
}

func ev(t *testing.T, typ event.Type, d any) event.Event {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: typ, Data: b, TS: time.Now()}
}

// side is the conversation handed-over work runs in, which is never the published one.
func (ar *arrival) side(sid string) string { return sid + "_side" }

// carrying is what this companion last announced about its load.
func (ar *arrival) carrying() (int, bool) {
	ar.carried.Lock()
	defer ar.carried.Unlock()
	return ar.carried.waiting, ar.carried.handling
}

func (ar *arrival) prompts() []string {
	ar.got.Lock()
	defer ar.got.Unlock()
	return append([]string(nil), ar.got.prompts...)
}

// Work arriving from another machine lands in the companion that was reached, with the asker's
// label above it and the request untouched.
//
// Nothing names a session and nothing names a companion. The caller connected to this one, so there
// is nothing left to resolve — which is what stopped "which account did ssh land as" from deciding
// whether work could be delivered at all.
func TestWorkArrivingFromAnotherMachineLandsInTheCompanionReached(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")

	receipt, err := cl.Hand("— asked by master on mini, ...", "rewrite the settings screen")
	if err != nil {
		t.Fatalf("it was refused: %v", err)
	}
	if receipt == "" {
		t.Fatal("no receipt came back, so the answer could never be collected")
	}
	until(t, "the work to be started", func() bool { return len(ar.prompts()) == 1 })
	got := ar.prompts()
	// Byte for byte, separator included. Prefix-and-suffix would pass on a label glued to the first
	// word of the request, which is exactly what happened the last time this was two message parts.
	if want := "— asked by master on mini, ...\n\nrewrite the settings screen"; got[0] != want {
		t.Errorf("what arrived is not what was sent:\n got %q\nwant %q", got[0], want)
	}
}

// An arrival with no label still gets one.
//
// A request with no attribution is indistinguishable from something the person sitting there typed,
// and the rule that stops work being passed on for ever is read off exactly this mark.
func TestAnArrivalWithNoLabelIsStillMarkedAsHandedOver(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")

	if _, err := cl.Hand("", "do it"); err != nil {
		t.Fatalf("it was refused: %v", err)
	}
	until(t, "the work to be started", func() bool { return len(ar.prompts()) == 1 })
	if got := ar.prompts(); !strings.Contains(got[0], "asked by") {
		t.Fatalf("an unlabelled arrival reads as something a person typed: %q", got)
	}
}

// The answer is the turn that finished after the work landed, and never one that finished before.
//
// A companion has usually been doing something else for an hour before anybody hands it anything,
// so its log is full of finished turns with answers in them. What separates them from this
// request's answer is the position the receipt was minted at, and nothing else — lose it and the
// way back returns the last thing the companion happened to say, immediately, looking correct.
func TestAnEarlierAnswerIsNotMistakenForThisOne(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")

	receipt, err := cl.Hand("— asked by master", "do it")
	if err != nil {
		t.Fatal(err)
	}
	got, err := cl.Handed(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if got.Done {
		t.Fatalf("a turn that finished before the work arrived was reported as its answer: %+v", got)
	}
	ar.finishes(ar.side("s_design"), "done it")
	got, err = cl.Handed(receipt)
	if err != nil || !got.Done || got.Answer != "done it" {
		t.Fatalf("a turn finishing after the work landed was not seen as its answer: %+v %v", got, err)
	}
}

// A caller that did not hand the work over cannot read its answer.
//
// The door used to take a session and a position, which are two numbers with nothing about WHO. So
// naming somebody else's session — by mistake far more likely than by malice, since a caller
// holding several of these can use the wrong one — came back with their answer, attributed to your
// request and looking entirely normal.
//
// A receipt is the handle and the permission at once. There is nothing else to present: the wire
// has no session field on this method at all, so a caller cannot name work it did not hand over
// even by accident.
func TestAnswersCannotBeReadWithoutTheReceiptForThem(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	receipt, err := cl.Hand("— asked by master", "do it")
	if err != nil {
		t.Fatal(err)
	}
	ar.finishes(ar.side("s_design"), "somebody else's answer")

	// A made-up receipt is no better than none.
	if got, err := cl.Handed("00000000000000000000000000000000"); err == nil || got.Done {
		t.Fatalf("an invented receipt was answered: %+v", got)
	}
	// Naming the session instead, which is what the door used to accept. It is not a field on this
	// method, so it arrives as a request with no receipt — and is refused for that reason.
	if got, err := cl.Handed(""); err == nil || got.Answer != "" {
		t.Fatalf("a question with no receipt was answered: %+v", got)
	}
	// And the real one still works, so this is a lock and not a wall.
	if got, err := cl.Handed(receipt); err != nil || !got.Done {
		t.Fatalf("the receipt that was issued did not open it: %+v %v", got, err)
	}
}

// A refusal is the companion's own answer, and reads differently from a machine that never spoke.
//
// The two used to be told apart by an exit code, because the door was a subcommand over ssh. Over
// the protocol they are told apart by which of them produced the error: a daemon that answered and
// said no, or a link that never reached one. A caller that cannot tell will send somebody to fix a
// network because a companion was full.
func TestARefusalIsAnAnswerAndNotAFailedCall(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	ar.work.setBusy("the-person-is-working") // so nothing drains and the queue fills

	for i := 0; i < maxWaiting; i++ {
		if _, err := cl.Hand("— asked by master", "do it"); err != nil {
			t.Fatalf("piece %d was refused before the queue was full: %v", i, err)
		}
	}
	_, err := cl.Hand("— asked by master", "one too many")
	if err == nil {
		t.Fatal("a full companion took more work")
	}
	var refused daemon.Refused
	if !errors.As(err, &refused) {
		t.Fatalf("a companion that answered reads as one that could not be reached: %#v", err)
	}
	if !strings.Contains(refused.Why, "Ask somebody else") {
		t.Errorf("the refusal does not say what to do instead: %q", refused.Why)
	}
	if len(ar.prompts()) != 0 {
		t.Error("something was started while the workspace was in use")
	}
}

// Asked about, the companion answers with the finished text — and before that, with nothing.
func TestTheFarSideAnswersOnlyOnceTheTurnHasFinished(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	receipt, err := cl.Hand("— asked by master", "do it")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := cl.Handed(receipt); err != nil || got.Done {
		t.Fatalf("an unfinished turn answered %+v %v", got, err)
	}
	ar.finishes(ar.side("s_design"), "the screen is rewritten")

	got, err := cl.Handed(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Done || got.Answer != "the screen is rewritten" {
		t.Fatalf("the finished answer came back as %+v", got)
	}
}

// A companion that restarted does not know the receipt, which is how the waiting side learns the
// work is gone.
//
// A running daemon cannot report its own death, and the receipts it minted were never written down
// — deliberately, because a restart did not finish the turn they point at, so preserving them would
// preserve only the ability to ask a question whose answer is permanently "not yet". What is left
// is the honest answer: this is not work I took.
func TestACompanionThatRestartedDoesNotRecogniseTheOldReceipt(t *testing.T) {
	ar := newArrival(t)
	cl, before := ar.publish("design", "s_design")
	receipt, err := cl.Hand("— asked by master", "do it")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, ok := before.Where(receipt); !ok {
		t.Fatal("the receipt it issued was not recorded")
	}
	// What a restart leaves: the same companion, the same log, no memory of taking anything.
	fresh := daemon.NewReceipts()
	if _, _, _, ok := fresh.Where(receipt); ok {
		t.Fatal("a restarted daemon recognised a receipt it never issued")
	}
	after := handover{work: &recordingWork{App: ar.reader, ar: ar, side: "s_design_side"},
		at: newWhere("s_design"), workdir: ar.t.TempDir(), configDir: ar.cfgDir, receipts: fresh,
		mine: newSideSessions(), queued: newWaiting(nil)}
	if _, err := after.Handed(context.Background(), receipt); err == nil {
		t.Fatal("a restarted companion answered about work it has no record of taking")
	}
}

// The answer is pushed when the turn ends, without anybody asking again.
//
// What this replaces is the waiting side spawning a process across a network every three seconds
// for up to two hours — a tick sized for reading a log file two microseconds away, left driving an
// ssh. The socket was already open both ways; only the protocol was one-way.
func TestTheAnswerIsPushedWhenTheTurnEnds(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	receipt, err := cl.Hand("— asked by master", "do it")
	if err != nil {
		t.Fatal(err)
	}

	// A connection of its own: a watch gives its connection over to the stream, so a caller that
	// watched on the one it does everything else with would be unable to do anything else.
	sock := filepath.Join(ar.cfgDir, "daemon-s_design.sock")
	watcher, derr := daemon.Dial(sock)
	if derr != nil {
		t.Fatal(derr)
	}
	defer watcher.Close()

	got := make(chan daemon.Handover, 4)
	done := make(chan error, 1)
	go func() {
		done <- watcher.Watch(receipt, func(h daemon.Handover) bool {
			got <- h
			return !(h.Done || h.Over)
		})
	}()

	// Nothing yet: an unfinished turn has nothing to say, and a frame saying so would be the poll
	// this replaces, dressed as a push.
	select {
	case h := <-got:
		t.Fatalf("an unfinished turn pushed %+v", h)
	case <-time.After(300 * time.Millisecond):
	}

	ar.finishes(ar.side("s_design"), "the screen is rewritten")
	select {
	case h := <-got:
		if !h.Done || h.Answer != "the screen is rewritten" {
			t.Fatalf("the pushed frame was %+v", h)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the turn ended and nothing was pushed")
	}
	// And the stream ends itself, rather than leaving a connection open on both machines for
	// however long the asker takes to notice.
	select {
	case werr := <-done:
		if werr != nil {
			t.Fatalf("the stream ended with %v", werr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not end after its last word")
	}
}

// A watch whose peer walks away ends, instead of holding a goroutine until the daemon stops.
//
// Nothing is written to a watch that has nothing to report, so a dead connection cannot be noticed
// by a failing write — there is no write. It is noticed by reading for the hang-up.
func TestAWatchEndsWhenThePeerHangsUp(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	receipt, err := cl.Hand("— asked by master", "do it")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(ar.cfgDir, "daemon-s_design.sock")
	watcher, derr := daemon.Dial(sock)
	if derr != nil {
		t.Fatal(derr)
	}
	go func() { _ = watcher.Watch(receipt, func(daemon.Handover) bool { return true }) }()
	until(t, "the watch to start", func() bool { return atomic.LoadInt32(&ar.live) == 1 })

	watcher.Close()
	until(t, "the watch to notice nobody is listening", func() bool {
		return atomic.LoadInt32(&ar.live) == 0
	})

	// And the work is unaffected: it is the companion's own, and its answer is in its own
	// transcript whether or not anybody was watching.
	ar.finishes(ar.side("s_design"), "done anyway")
	if got, err := cl.Handed(receipt); err != nil || !got.Done || got.Answer != "done anyway" {
		t.Fatalf("the work was affected by nobody listening: %+v %v", got, err)
	}
}

func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for " + what)
}

// A companion here is dialled; a companion elsewhere is never dialled by its path.
//
// This is the one hazard in preferring a local dial. A socket is a path, and two machines belonging
// to one person keep their checkouts in the same places — so the path does not fail, it opens
// whichever LOCAL companion happens to sit at it, and the work arrives in the wrong workspace
// looking delivered.
//
// The far branch here reaches nothing (the host does not resolve, and there may be no ssh at all).
// That is the point: whatever it does, it must not be talking to the daemon in this test.
func TestACompanionElsewhereIsNeverReachedByDiallingAPathHere(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	sock := filepath.Join(ar.cfgDir, "daemon-s_design.sock")
	cl.Close()

	// Answering at all is the signal: this daemon knows no such receipt, and says so in its own
	// words, which only something speaking to THIS daemon can produce.
	spokeTo := func(host string) bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, err := reachCompanion(ctx, host, sock)
		if err != nil {
			return false
		}
		defer c.Close()
		var refused daemon.Refused
		return errors.As(errOf(c.Handed("nope")), &refused)
	}
	for _, here := range []string{"", daemon.Host()} {
		if !spokeTo(here) {
			t.Errorf("a companion on this machine (host %q) was not reached by dialling it", here)
		}
	}
	if spokeTo("not-this-machine.invalid") {
		t.Fatal("a companion said to be on another machine was reached by dialling a socket here")
	}
}

func errOf(_ daemon.Handover, err error) error { return err }

// A companion whose socket is right here is dialled, whatever it calls its machine.
//
// Sockets live in the config directory, so a shared one — two containers with a mount in common,
// two workstations with a network home — puts a companion that calls itself something else on this
// disk, answering. "Is that hostname mine" said no and sent the work out through ssh to reach a
// path in the next directory; on a container with no sshd it did not reach it at all.
//
// The record beside the socket is what settles it, and the test above is the other half: a record
// naming a machine the caller did not ask about is somebody else's path and must not be dialled.
func TestACompanionUnderAnotherMachinesNameIsStillDialledWhenItIsHere(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	sock := filepath.Join(ar.cfgDir, "daemon-s_design.sock")
	cl.Close()

	// Republished as a companion belonging to "sidecar", the way another container writing into
	// this directory would leave it.
	b, err := json.Marshal(daemon.Info{
		Socket: sock, Workdir: ar.t.TempDir(), Session: "s_design", Name: "design", Host: "sidecar"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daemon.SessionFile(sock), b, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, rerr := reachCompanion(ctx, "sidecar", sock)
	if rerr != nil {
		t.Fatalf("a companion answering on this disk could not be reached: %v", rerr)
	}
	defer c.Close()
	var refused daemon.Refused
	if !errors.As(errOf(c.Handed("nope")), &refused) {
		t.Fatal("it did not reach the daemon that is right there — it went the long way round")
	}
}

// A workspace with a lot of skills sends a sample and the true count, not one or the other.
//
// The list is bounded because it rides on every member of every exchange, every minute, from every
// machine. The count is not bounded, because it decides hub elections and a capped one would make
// a companion with two hundred skills tie with one that has twelve.
func TestAWorkspaceWithManySkillsSendsASampleAndTheRealCount(t *testing.T) {
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(wd, ".magi", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		f := filepath.Join(wd, ".magi", "skills", fmt.Sprintf("s%02d.md", i))
		if err := os.WriteFile(f, []byte("does a thing\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, names := countCan(st, wd)
	if n != 20 {
		t.Errorf("the count is %d, not the twenty things it can do", n)
	}
	if len(names) != cluster.MaxDoes {
		t.Errorf("%d names would travel with every sighting, want at most %d", len(names), cluster.MaxDoes)
	}
}

// Handed-over work never lands in the conversation a person is attached to, and two askers never
// share one.
//
// A request borrows the workspace and the skills — the capability — not the conversation. Somebody
// watching their own session must not have another agent's request appear in it, and an answer
// written for one asker must not be sitting in the history the next one is answered from.
func TestWorkFromSomebodyElseGetsItsOwnConversation(t *testing.T) {
	ar := newArrival(t)
	cl, receipts := ar.publish("design", "s_design")

	first, err := cl.Hand("— asked by master", "name the tokens")
	if err != nil {
		t.Fatal(err)
	}
	where, _, _, ok := receipts.Where(first)
	if !ok {
		t.Fatal("the receipt names nowhere")
	}
	if where == "s_design" {
		t.Fatal("the work landed in the session a person is attached to")
	}
	// The same asker again continues where it left off — that is what makes a re-ask worth
	// anything, and it is the same mechanism as the isolation rather than a second one.
	second, err := cl.Hand("— asked by master", "and the contrast ratios")
	if err != nil {
		t.Fatal(err)
	}
	again, _, _, _ := receipts.Where(second)
	if again != where {
		t.Errorf("a second request from the same asker opened a new conversation (%s then %s)",
			where, again)
	}
	// A different asker does not.
	other := handover{work: ar.work, at: newWhere("s_design"), workdir: ar.t.TempDir(),
		configDir: ar.cfgDir, receipts: receipts, mine: newSideSessions(), queued: newWaiting(nil)}
	mine, merr := other.sessionFor(context.Background(), "— asked by master")
	if merr != nil {
		t.Fatal(merr)
	}
	theirs, terr := other.sessionFor(context.Background(), "— asked by scribe")
	if terr != nil {
		t.Fatal(terr)
	}
	if mine == theirs {
		t.Errorf("two askers were given one conversation (%s)", mine)
	}
}

// One at a time, and busy means queued rather than refused.
//
// The conversations are isolated; the WORK is not, and must not be. This is an agent that edits
// files — two turns at once in one tree are two writers with nothing between them, the person's
// own turn included. But bouncing the request off a companion who happens to be mid-turn puts the
// retry on the asker, which is either a model that gives up on the right companion or one that
// polls. So it goes in the inbox, and says so until it starts.
func TestWorkIsQueuedWhileTheWorkspaceIsInUse(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")

	ar.work.setBusy("the-person-is-working")
	receipt, err := cl.Hand("— asked by master", "name the tokens")
	if err != nil {
		t.Fatalf("work was refused instead of taken: %v", err)
	}
	// Taken, not started: nothing may run on top of the turn already going.
	time.Sleep(200 * time.Millisecond)
	if n := len(ar.prompts()); n != 0 {
		t.Fatalf("%d pieces started on top of a turn already running", n)
	}
	// And asking says so, rather than the silence of a companion that is thinking.
	got, herr := cl.Handed(receipt)
	if herr != nil {
		t.Fatal(herr)
	}
	if !strings.Contains(got.News, "not started yet") || got.Over || got.Done {
		t.Fatalf("a queued piece reads as %+v", got)
	}
	// When the workspace frees up it starts, without anybody asking again.
	ar.work.setBusy("")
	until(t, "the queue to drain", func() bool { return len(ar.prompts()) == 1 })
	if got, _ := cl.Handed(receipt); strings.Contains(got.News, "not started") {
		t.Errorf("it started but still reads as queued: %+v", got)
	}
}

// The next piece starts when the turn before it ends, not when a timer next looks.
//
// The drain has a backstop tick for the one thing it cannot observe — the person's own turn ending
// in their own session — and if that were the only trigger every queued piece would wait out the
// tick for no reason. Watching the session it just started is how the common case is noticed
// without polling for what this process already knows.
func TestTheNextPieceStartsWhenTheOneBeforeItEnds(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")

	if _, err := cl.Hand("— asked by master", "first"); err != nil {
		t.Fatal(err)
	}
	until(t, "the first piece to start", func() bool { return len(ar.prompts()) == 1 })
	if _, err := cl.Hand("— asked by scribe", "second"); err != nil {
		t.Fatal(err)
	}
	// Held: one at a time.
	time.Sleep(150 * time.Millisecond)
	if n := len(ar.prompts()); n != 1 {
		t.Fatalf("%d pieces running at once", n)
	}

	started := time.Now()
	ar.finishes(ar.side("s_design"), "the first one is done")
	until(t, "the next piece to start", func() bool { return len(ar.prompts()) == 2 })
	// Well inside the backstop: with only the tick this waits it out, which is the difference the
	// turn-finish watch makes.
	if waited := time.Since(started); waited > drainEvery/2 {
		t.Errorf("the next piece waited %s — the tick found it, nothing noticed the turn ending", waited)
	}
}

// A wake that arrives a moment before the workspace is free still starts the piece promptly.
//
// The two are not one instant: the nudge rides the turn-finished event, and the flag that says a
// turn is in flight is dropped by the goroutine retiring behind it. Lose that race — and it is a
// race, so it is lost sometimes — and the drain looks, sees busy, and goes back to sleep until the
// backstop, with everything the piece needed already true three seconds earlier.
func TestAWakeThatArrivesBeforeTheWorkspaceIsFreeDoesNotCostTheBackstop(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	ar.work.setBusy("the-person-is-working")

	if _, err := cl.Hand("— asked by master", "count the lines"); err != nil {
		t.Fatal(err)
	}
	// The nudge for a turn that has ended, delivered while the workspace still reads busy. This is
	// the only wake there will be: nothing else nudges when the flag is dropped afterwards.
	ar.taking.queued.wake()
	time.Sleep(20 * time.Millisecond)
	if n := len(ar.prompts()); n != 0 {
		t.Fatalf("%d pieces started while the workspace was busy, which is two turns in one workspace", n)
	}

	ar.work.freeIf("the-person-is-working")
	freed := time.Now()
	until(t, "the piece to start once the workspace freed", func() bool { return len(ar.prompts()) == 1 })
	if waited := time.Since(freed); waited > drainEvery/2 {
		t.Errorf("the piece waited %s after the workspace freed — that is the backstop finding it, "+
			"which is what the retry after a busy wake exists to prevent", waited)
	}
}

// A piece that waited in a queue gets its own answer, not the one in front of it.
//
// The position a receipt stands for used to be taken when the work was TAKEN. A piece that then
// waited had turns finishing in front of it, so "the first turn that finishes past here" was
// somebody else's — and the asker got that answer, attributed to its own request, looking entirely
// plausible. Observed live: two pieces handed back to back came back with the same answer, twice.
func TestAQueuedPieceGetsItsOwnAnswer(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")

	first, err := cl.Hand("— asked by master", "count the lines")
	if err != nil {
		t.Fatal(err)
	}
	until(t, "the first piece to start", func() bool { return len(ar.prompts()) == 1 })
	second, err := cl.Hand("— asked by master", "count the characters")
	if err != nil {
		t.Fatal(err)
	}
	// Both live in one side session, so nothing but the position tells their answers apart.
	side := ar.side("s_design")
	ar.finishes(side, "four lines")
	until(t, "the second piece to start", func() bool { return len(ar.prompts()) == 2 })
	ar.finishes(side, "eight characters")

	one, err := cl.Handed(first)
	if err != nil || !one.Done || one.Answer != "four lines" {
		t.Fatalf("the first piece answered %+v %v", one, err)
	}
	two, err := cl.Handed(second)
	if err != nil {
		t.Fatal(err)
	}
	if two.Answer == "four lines" {
		t.Fatal("the second piece was handed the answer to the first, which is what a wrong " +
			"answer that looks right is made of")
	}
	if !two.Done || two.Answer != "eight characters" {
		t.Fatalf("the second piece answered %+v", two)
	}
}

// Every arrival is written down, taken or turned away, with how much was already in front of it.
//
// The live depth beside it is what an asker reads; this is what is left afterwards. They answer
// different questions and neither substitutes: "how busy is it right now" is an instant, and
// "is one of these enough" is a pattern that no instant contains.
func TestEveryArrivalIsWrittenDownWithHowMuchWasAlreadyThere(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	ar.work.setBusy("the-person-is-working") // so nothing drains and the queue fills

	for i := 0; i < maxWaiting; i++ {
		if _, err := cl.Hand("— asked by master", "do it"); err != nil {
			t.Fatalf("piece %d was refused before the queue was full: %v", i, err)
		}
	}
	if _, err := cl.Hand("— asked by master", "one too many"); err == nil {
		t.Fatal("a full companion took more work")
	}

	got := ar.noted()
	if len(got) != maxWaiting+1 {
		t.Fatalf("%d arrivals of %d were written down: %+v", len(got), maxWaiting+1, got)
	}
	for i := 0; i < maxWaiting; i++ {
		if got[i].full {
			t.Errorf("arrival %d was taken and recorded as turned away", i)
		}
		// How many were in front of it. A count of arrivals cannot tell a companion that was
		// briefly busy from one that runs at its limit; this can.
		if got[i].ahead != i {
			t.Errorf("arrival %d says %d were already waiting", i, got[i].ahead)
		}
	}
	last := got[len(got)-1]
	if !last.full {
		t.Error("the refusal was written down as work taken")
	}
	if last.ahead != maxWaiting {
		t.Errorf("the refusal says the queue was %d deep", last.ahead)
	}
}

// A companion says when it picks a piece up and when it puts it down.
//
// Its state says nothing about this: that is read from the session a person attaches to, and this
// runs in a conversation of its own. An asker reading only the state would see "idle" and hand work
// to a companion that is mid-turn.
func TestACompanionSaysWhenItIsInTheMiddleOfSomething(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	if _, err := cl.Hand("— asked by master", "do it"); err != nil {
		t.Fatal(err)
	}
	until(t, "the piece to start", func() bool { _, h := ar.carrying(); return h })

	ar.finishes(ar.side("s_design"), "done")
	// Put down when the turn ends. Left set, this companion would be avoided by every asker for as
	// long as it ran, with nothing able to correct it.
	until(t, "the piece to be put down", func() bool { _, h := ar.carrying(); return !h })
}

// Work handed over waits behind the person, not only behind the turn.
//
// Both wake at the same instant — the run goroutine drains its own queued interjection when a turn
// ends, and this drain is nudged by the same ending — so which one took the workspace was a race
// with no rule in it. Losing it means somebody who typed a correction while their agent worked now
// waits for a request that arrived from another machine.
func TestHandedOverWorkWaitsBehindThePerson(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	ar.work.setBusy("the-person-is-working") // so the piece is queued rather than started at once
	if _, err := cl.Hand("— asked by master", "audit the config"); err != nil {
		t.Fatalf("the piece was refused: %v", err)
	}
	sent := func() int {
		ar.got.Lock()
		defer ar.got.Unlock()
		return len(ar.got.prompts)
	}
	if sent() != 0 {
		t.Fatal("precondition: the piece started while a turn was running")
	}

	// The turn ends, and the workspace is free — but the person has something parked, and that
	// outranks a request handed in from somewhere else.
	ar.work.setPersonWaiting(true)
	ar.work.setBusy("")
	ar.taking.startNext(context.Background())
	if n := sent(); n != 0 {
		t.Fatalf("handed-over work started while the person's own was waiting (%d started)", n)
	}

	// Once nothing of the person's is waiting it goes, which is what makes this an order rather
	// than a block.
	ar.work.setPersonWaiting(false)
	ar.taking.startNext(context.Background())
	if n := sent(); n != 1 {
		t.Errorf("with the person's queue empty, the piece did not start (%d started)", n)
	}
}
