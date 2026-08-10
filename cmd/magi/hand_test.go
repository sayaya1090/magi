package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// The far side of a crossing: a real companion behind a real socket, on a real store.
type arrival struct {
	t      *testing.T
	cfgDir string
	store  *jsonl.Store
	reader *app.App
	got    struct {
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
	return &arrival{t: t, cfgDir: shortSockDir(t), store: st,
		reader: app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})}
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

func (ar *arrival) engineFor() daemon.Engine { return &takingEngine{ar: ar} }

// publish starts a companion here and gives it a finished turn, so it reads as idle.
func (ar *arrival) publish(name, sid string) string {
	ar.t.Helper()
	wd := ar.t.TempDir()
	sock := filepath.Join(ar.cfgDir, "daemon-"+sid+".sock")
	ctx, cancel := context.WithCancel(context.Background())
	ar.t.Cleanup(cancel)
	go func() { _ = daemon.Serve(ctx, ar.engineFor(), sock) }()
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
	ar.append(sid, ev(ar.t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: wd}),
		ev(ar.t, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m0",
			Parts: []session.Part{{Kind: session.PartText, Text: "get set up"}}}),
		ev(ar.t, event.TypeTurnFinished, event.TurnFinishedData{}))
	return sock
}

func (ar *arrival) append(sid string, evs ...event.Event) {
	ar.t.Helper()
	for _, e := range evs {
		if _, err := ar.store.Append(context.Background(), session.SessionID(sid), e); err != nil {
			ar.t.Fatal(err)
		}
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

// takingEngine records what was submitted, which is what "the work arrived" means here.
type takingEngine struct{ ar *arrival }

func (e *takingEngine) Submit(_ context.Context, c command.SubmitPrompt) error {
	e.ar.got.Lock()
	defer e.ar.got.Unlock()
	var b strings.Builder
	for _, p := range c.Parts {
		b.WriteString(p.Text)
	}
	e.ar.got.prompts = append(e.ar.got.prompts, b.String())
	return nil
}
func (e *takingEngine) Steer(context.Context, command.SubmitPrompt) error  { return nil }
func (e *takingEngine) Interrupt(context.Context, command.Interrupt) error { return nil }
func (e *takingEngine) Waiting(session.SessionID) (app.Ask, bool)          { return app.Ask{}, false }
func (e *takingEngine) Doing(session.SessionID) (string, bool)             { return "", false }
func (e *takingEngine) RespondPermission(context.Context, command.RespondPermission) error {
	return nil
}
func (e *takingEngine) RespondQuestion(context.Context, command.RespondQuestion) error { return nil }

func (ar *arrival) prompts() []string {
	ar.got.Lock()
	defer ar.got.Unlock()
	return append([]string(nil), ar.got.prompts...)
}

func (ar *arrival) hand(body any) (handReply, string, int) {
	ar.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		ar.t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := handHere(bytes.NewReader(b), &out, &errOut, ar.reader, ar.cfgDir)
	var rep handReply
	if out.Len() > 0 {
		if jerr := json.Unmarshal(out.Bytes(), &rep); jerr != nil {
			ar.t.Fatalf("%v: %s", jerr, out.String())
		}
	}
	return rep, errOut.String(), code
}

func (ar *arrival) state(body any) (stateReply, int) {
	ar.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		ar.t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := handoffStateHere(bytes.NewReader(b), &out, &errOut, ar.reader, ar.cfgDir)
	var rep stateReply
	if out.Len() > 0 {
		if jerr := json.Unmarshal(out.Bytes(), &rep); jerr != nil {
			ar.t.Fatalf("%v: %s", jerr, out.String())
		}
	}
	return rep, code
}

// Work arriving over ssh lands in the named companion's session, with the asker's label above it
// and the request untouched.
func TestWorkArrivingFromAnotherMachineLandsInTheNamedCompanion(t *testing.T) {
	ar := newArrival(t)
	ar.publish("design", "s_design")

	rep, said, code := ar.hand(handRequest{From: "master", To: "design",
		Request: "rewrite the settings screen", Label: "— asked by master on mini, ..."})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, said)
	}
	if rep.Refused != "" {
		t.Fatalf("refused: %s", rep.Refused)
	}
	if rep.Session != "s_design" {
		t.Errorf("it reported session %q", rep.Session)
	}
	got := ar.prompts()
	if len(got) != 1 {
		t.Fatalf("%d prompts arrived", len(got))
	}
	if !strings.HasPrefix(got[0], "— asked by master on mini, ...") {
		t.Errorf("the asker's label was not kept: %q", got[0])
	}
	if !strings.HasSuffix(got[0], "rewrite the settings screen") {
		t.Errorf("the request was altered: %q", got[0])
	}
}

// The position their log stood at is taken BEFORE the work goes in.
//
// It is the whole definition of the answer — the first turn that finishes past this point. Taken
// afterwards it would already be past the turn being started, and the wait would sit through this
// request's answer and deliver the one after it.
func TestThePositionIsTakenBeforeTheWorkGoesIn(t *testing.T) {
	ar := newArrival(t)
	ar.publish("design", "s_design")

	rep, _, _ := ar.hand(handRequest{From: "master", To: "design", Request: "do it"})
	if rep.Since == 0 {
		t.Fatal("no position came back, so an answer could never be found again")
	}
	done, _ := ar.reader.AnswerSince(context.Background(), "s_design", rep.Since)
	if done {
		t.Fatal("the position already includes a finished turn — the answer would be the previous one")
	}
	ar.append("s_design", ev(t, event.TypeTurnFinished, event.TurnFinishedData{}))
	if done, _ := ar.reader.AnswerSince(context.Background(), "s_design", rep.Since); !done {
		t.Fatal("a turn finishing after the work landed was not seen as its answer")
	}
}

// An address nobody here answers to is refused, and the refusal exits 0.
//
// The exit code is the point. ssh reports the remote command's status, and a refusal that exited
// non-zero would be indistinguishable from a machine that could not be reached — the asker would
// report "buildbox did not take it" when buildbox answered perfectly clearly.
func TestARefusalIsAnAnswerAndNotAFailedCall(t *testing.T) {
	ar := newArrival(t)
	ar.publish("design", "s_design")

	rep, said, code := ar.hand(handRequest{From: "master", To: "nobody", Request: "do it"})
	if code != 0 {
		t.Fatalf("a refusal exited %d (%s), which reads as an unreachable machine", code, said)
	}
	if rep.Refused == "" {
		t.Fatal("an unknown address was not refused")
	}
	if !strings.Contains(rep.Refused, "design") {
		t.Errorf("the refusal does not say who there is: %q", rep.Refused)
	}
	if len(ar.prompts()) != 0 {
		t.Error("it was sent anyway")
	}
}

// Asked about, the far side answers with the finished text — and before that, with nothing.
func TestTheFarSideAnswersOnlyOnceTheTurnHasFinished(t *testing.T) {
	ar := newArrival(t)
	ar.publish("design", "s_design")
	rep, _, _ := ar.hand(handRequest{From: "master", To: "design", Request: "do it"})

	if got, code := ar.state(stateRequest{Session: "s_design", Since: rep.Since}); code != 0 || got.Done {
		t.Fatalf("an unfinished turn answered %+v", got)
	}
	ar.append("s_design",
		ev(t, event.TypePartAppended, event.PartAppendedData{MessageID: "m9",
			Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: "the screen is rewritten"}}),
		ev(t, event.TypeTurnFinished, event.TurnFinishedData{}))

	got, code := ar.state(stateRequest{Session: "s_design", Since: rep.Since})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !got.Done || got.Answer != "the screen is rewritten" {
		t.Fatalf("the finished answer came back as %+v", got)
	}
}

// A companion that stopped without finishing ends the wait rather than leaving it silent.
func TestAStoppedCompanionEndsTheWaitAcrossTheWire(t *testing.T) {
	ar := newArrival(t)
	// Published with nothing listening: what a killed daemon leaves behind.
	sock := filepath.Join(ar.cfgDir, "daemon-s_gone.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unpublish, err := daemon.Publish(sock, t.TempDir(), "s_gone", daemon.Identity{Name: "gone"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(unpublish)
	ar.append("s_gone", ev(t, event.TypeSessionCreated, event.SessionCreatedData{Workdir: "/w"}),
		ev(t, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "m0",
			Parts: []session.Part{{Kind: session.PartText, Text: "do it"}}}))

	got, code := ar.state(stateRequest{Session: "s_gone", Since: 0})
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !got.Over || got.News == "" {
		t.Fatalf("a daemon that died mid-work left the wait with %+v", got)
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
