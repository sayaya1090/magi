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
	bus  *bus.Bus
	said int
	live int32 // watches running inside this companion right now
	got  struct {
		sync.Mutex
		prompts []string
	}
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
	ar *arrival
}

func (w *recordingWork) Submit(_ context.Context, c command.SubmitPrompt) error {
	w.ar.got.Lock()
	defer w.ar.got.Unlock()
	var b strings.Builder
	for _, p := range c.Parts {
		b.WriteString(p.Text)
	}
	w.ar.got.prompts = append(w.ar.got.prompts, b.String())
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
	eng := takingEngine{live: &ar.live, handover: handover{
		work: &recordingWork{App: ar.reader, ar: ar}, sid: session.SessionID(sid),
		configDir: ar.cfgDir, receipts: receipts,
	}}
	ctx, cancel := context.WithCancel(context.Background())
	ar.t.Cleanup(cancel)
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
	ar.append(sid,
		ev(ar.t, event.TypePartAppended, event.PartAppendedData{
			MessageID: fmt.Sprintf("m%d", ar.said),
			Role:      session.RoleAssistant,
			Part:      session.Part{Kind: session.PartText, Text: said}}),
		ev(ar.t, event.TypeTurnFinished, event.TurnFinishedData{}))
}

func ev(t *testing.T, typ event.Type, d any) event.Event {
	t.Helper()
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return event.Event{Type: typ, Data: b, TS: time.Now()}
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
	got := ar.prompts()
	if len(got) != 1 {
		t.Fatalf("%d prompts arrived", len(got))
	}
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
	got := ar.prompts()
	if len(got) != 1 || !strings.Contains(got[0], "asked by") {
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
	ar.finishes("s_design", "done it")
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
	ar.finishes("s_design", "somebody else's answer")

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
// network because a companion was busy.
func TestARefusalIsAnAnswerAndNotAFailedCall(t *testing.T) {
	ar := newArrival(t)
	cl, _ := ar.publish("design", "s_design")
	// Mid-turn: a prompt with no finish after it, which is what a companion at work looks like.
	ar.append("s_design", ev(t, event.TypePromptSubmitted, event.PromptSubmittedData{
		MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: "rebuilding the index"}}}))

	_, err := cl.Hand("— asked by master", "do it")
	if err == nil {
		t.Fatal("a companion mid-turn took the work anyway")
	}
	var refused daemon.Refused
	if !errors.As(err, &refused) {
		t.Fatalf("a companion that answered reads as one that could not be reached: %#v", err)
	}
	if !strings.Contains(refused.Why, "mid-turn") {
		t.Errorf("the refusal does not say why: %q", refused.Why)
	}
	if len(ar.prompts()) != 0 {
		t.Error("it was sent anyway")
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
	ar.finishes("s_design", "the screen is rewritten")

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
	if _, ok := before.Since(receipt); !ok {
		t.Fatal("the receipt it issued was not recorded")
	}
	// What a restart leaves: the same companion, the same log, no memory of taking anything.
	fresh := daemon.NewReceipts()
	if _, ok := fresh.Since(receipt); ok {
		t.Fatal("a restarted daemon recognised a receipt it never issued")
	}
	after := handover{work: &recordingWork{App: ar.reader, ar: ar},
		sid: "s_design", configDir: ar.cfgDir, receipts: fresh}
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

	ar.finishes("s_design", "the screen is rewritten")
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
	ar.finishes("s_design", "done anyway")
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
