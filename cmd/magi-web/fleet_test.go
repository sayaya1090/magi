package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// fleetFixture builds a viewer over a real store and a real config directory, so the test exercises
// the same two facts the dashboard derives everything from: whether a socket answers, and what the
// log says.
type fleetFixture struct {
	srv    *server
	store  *jsonl.Store
	cfgDir string
	t      *testing.T
}

// shortTempDir is t.TempDir() with a path a unix socket can live at.
//
// A unix address is about 104 bytes and t.TempDir() on macOS starts with /var/folders/…/T/ plus the
// test's own name — TestTargetAcceptsAnyListedDaemonAndNothingElse alone is 45 of them. Binding
// there fails with "invalid argument", which says nothing about length; observed exactly that way
// writing this test, which is also why daemon.tooLong exists.
func shortTempDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "magiweb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func newFleetFixture(t *testing.T) *fleetFixture {
	t.Helper()
	cfg, data := shortTempDir(t), t.TempDir()
	st, err := jsonl.New(data)
	if err != nil {
		t.Fatal(err)
	}
	reader := app.New(st, nil, builtin.NewRegistry(), bus.New(), nil, app.Config{})
	return &fleetFixture{
		srv:    &server{reader: reader, cfgDir: cfg, clients: map[string]*daemon.Client{}},
		store:  st,
		cfgDir: cfg,
		t:      t,
	}
}

// daemonAt publishes a record for a workspace, and listens on the socket when live is true. A dead
// daemon is exactly a record with nothing behind it — the crash case, which is the one the
// dashboard exists to make visible.
func (f *fleetFixture) daemonAt(workdir, sid string, live bool) string {
	f.t.Helper()
	// Short socket paths: the OS refuses long ones and a t.TempDir() under a long test name gets
	// close. Named by hand rather than via SocketPath for the same reason.
	sock := filepath.Join(f.cfgDir, "daemon-"+sid+".sock")
	if live {
		ln, err := net.Listen("unix", sock)
		if err != nil {
			f.t.Fatal(err)
		}
		f.t.Cleanup(func() { ln.Close() })
		go func() {
			for {
				c, err := ln.Accept()
				if err != nil {
					return
				}
				c.Close()
			}
		}()
	} else if err := os.WriteFile(sock, nil, 0o600); err != nil {
		f.t.Fatal(err) // a socket file with nobody listening: what SIGKILL leaves behind
	}
	unpublish, err := daemon.Publish(sock, workdir, sid)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(unpublish)
	return sock
}

// session writes a log: a prompt, some tool calls, and a turn.finished only if finished is true.
func (f *fleetFixture) session(sid, workdir, prompt string, steps int, finished bool) {
	f.t.Helper()
	ctx := context.Background()
	ev := func(t event.Type, d any) event.Event {
		b, err := json.Marshal(d)
		if err != nil {
			f.t.Fatal(err)
		}
		return event.Event{Type: t, Data: b, TS: time.Now()}
	}
	evs := []event.Event{
		ev(event.TypeSessionCreated, event.SessionCreatedData{Workdir: workdir}),
		ev(event.TypePromptSubmitted, event.PromptSubmittedData{
			MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: prompt}}}),
	}
	for i := 0; i < steps; i++ {
		evs = append(evs, ev(event.TypePartAppended, event.PartAppendedData{
			MessageID: "a1", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
				CallID: "c", Name: "bash", Args: json.RawMessage(`{"command":"go build ./..."}`)}}}))
	}
	if finished {
		evs = append(evs, ev(event.TypeTurnFinished, event.TurnFinishedData{}))
	}
	if _, err := f.store.Append(ctx, session.SessionID(sid), evs...); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fleetFixture) get() []agentView {
	f.t.Helper()
	w := httptest.NewRecorder()
	f.srv.fleet(w, httptest.NewRequest(http.MethodGet, "/fleet", nil))
	if w.Code != http.StatusOK {
		f.t.Fatalf("/fleet answered %d: %s", w.Code, w.Body.String())
	}
	var out []agentView
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		f.t.Fatalf("/fleet returned unreadable JSON: %v", err)
	}
	return out
}

func find(t *testing.T, list []agentView, sid string) agentView {
	t.Helper()
	for _, a := range list {
		if a.Session == sid {
			return a
		}
	}
	t.Fatalf("no agent with session %q in %+v", sid, list)
	return agentView{}
}

// The dashboard's whole claim is that it can tell four situations apart, and each of the four means
// something different to the person reading it. "abandoned" is the one that pays for the feature:
// a daemon that died mid-turn looks identical to a finished one in every other view.
func TestFleetTellsTheFourStatesApart(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	f.daemonAt(wd, "working1", true)
	f.session("working1", wd, "make the tests pass", 3, false)
	f.daemonAt(wd, "idle1", true)
	f.session("idle1", wd, "what does this do", 1, true)
	f.daemonAt(wd, "aband1", false)
	f.session("aband1", wd, "refactor the store", 7, false)
	f.daemonAt(wd, "stop1", false)
	f.session("stop1", wd, "done here", 2, true)

	list := f.get()
	if len(list) != 4 {
		t.Fatalf("expected 4 agents, got %d: %+v", len(list), list)
	}
	for sid, want := range map[string]string{
		"working1": "working", "idle1": "idle", "aband1": "abandoned", "stop1": "stopped",
	} {
		if got := find(t, list, sid).State; got != want {
			t.Errorf("session %s: state %q, want %q", sid, got, want)
		}
	}

	// A working agent carries what it is working ON and how far it has got. A card that said only
	// "working" would send you into every agent to find out which one is yours.
	w := find(t, list, "working1")
	if !strings.Contains(w.Last, "make the tests pass") {
		t.Errorf("working card does not say what the turn was: %q", w.Last)
	}
	if w.Steps != 3 {
		t.Errorf("working card reports %d steps, the log has 3", w.Steps)
	}
	// And an abandoned one says how much work was thrown away — the number that decides whether
	// you resume it.
	if a := find(t, list, "aband1"); a.Steps != 7 {
		t.Errorf("abandoned card reports %d steps, the log has 7", a.Steps)
	}
}

// The full workspace path is on the card. The socket NAME carries only a base name and a hash, so a
// dashboard built from filenames alone cannot tell two checkouts of the same repo apart — which is
// the exact case where sending a steer to the wrong one costs the most.
func TestFleetCarriesTheWorkspacePath(t *testing.T) {
	f := newFleetFixture(t)
	a, b := filepath.Join(shortTempDir(t), "magi"), filepath.Join(shortTempDir(t), "magi")
	for _, d := range []string{a, b} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.daemonAt(a, "one", true)
	f.session("one", a, "first", 1, false)
	f.daemonAt(b, "two", true)
	f.session("two", b, "second", 1, false)

	list := f.get()
	if got := find(t, list, "one").Workdir; got != a {
		t.Errorf("workdir %q, want %q", got, a)
	}
	if got := find(t, list, "two").Workdir; got != b {
		t.Errorf("workdir %q, want %q", got, b)
	}
	// Both are named "magi" — which is the point: the name alone is not enough, and the card shows
	// both because of it.
	if find(t, list, "one").Name != "magi" || find(t, list, "two").Name != "magi" {
		t.Error("the card name should be the base directory")
	}
}

// A socket the viewer was not started in is still reachable — that is the whole request: monitor
// every agent, and enter any of them. But only sockets List already found: the parameter comes from
// a page, and a path from a page must not become a path this process dials.
func TestTargetAcceptsAnyListedDaemonAndNothingElse(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "other", true)
	f.session("other", wd, "hello", 1, false)
	f.srv.here = filepath.Join(f.cfgDir, "daemon-somewhere-else.sock") // this viewer's own directory

	in, err := f.srv.target(httptest.NewRequest(http.MethodGet, "/events?d="+sock, nil))
	if err != nil {
		t.Fatalf("a listed daemon should be reachable: %v", err)
	}
	if in.Session != "other" {
		t.Errorf("target resolved to session %q, want other", in.Session)
	}

	for _, bad := range []string{"/etc/passwd", filepath.Join(f.cfgDir, "daemon-nope.sock"), "../../x"} {
		if _, err := f.srv.target(httptest.NewRequest(http.MethodGet, "/events?d="+bad, nil)); err == nil {
			t.Errorf("target accepted %q, which no daemon published", bad)
		}
	}
	// No ?d= and no daemon in this directory is an error, not a silent fallback to somebody else's
	// agent: a composer pointed at an agent you did not choose is worse than a composer that says
	// it has nowhere to send.
	if _, err := f.srv.target(httptest.NewRequest(http.MethodGet, "/events", nil)); err == nil {
		t.Error("target with no ?d= and no local daemon should fail")
	}
}

// The viewer's own directory is marked. With several agents in the list, the one you are standing
// in is the one you most often want, and it is otherwise indistinguishable.
func TestFleetMarksTheLocalDaemon(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	mine := f.daemonAt(wd, "mine", true)
	f.session("mine", wd, "local", 1, false)
	f.daemonAt(wd, "theirs", true)
	f.session("theirs", wd, "remote", 1, false)
	f.srv.here = mine

	list := f.get()
	if !find(t, list, "mine").Here {
		t.Error("the local daemon is not marked")
	}
	if find(t, list, "theirs").Here {
		t.Error("another daemon is marked as local")
	}
}
