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
	"github.com/sayaya1090/magi/internal/adapter/fleet"
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
	unpublish, err := daemon.Publish(sock, workdir, sid, daemon.Identity{})
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

func (f *fleetFixture) get() []fleet.Agent {
	f.t.Helper()
	w := httptest.NewRecorder()
	f.srv.fleet(w, httptest.NewRequest(http.MethodGet, "/fleet", nil))
	if w.Code != http.StatusOK {
		f.t.Fatalf("/fleet answered %d: %s", w.Code, w.Body.String())
	}
	var out []fleet.Agent
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		f.t.Fatalf("/fleet returned unreadable JSON: %v", err)
	}
	return out
}

// /fleet is the dashboard's whole data source, and it must survive the round trip: the derivation
// is tested in internal/adapter/fleet, so what is left to check here is that the handler serves it
// as JSON the page can read, marking the daemon this viewer belongs to.
func TestTheFleetEndpointServesWhatThePageReads(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	mine := f.daemonAt(wd, "mine", true)
	f.session("mine", wd, "make the tests pass", 3, false)
	f.daemonAt(wd, "theirs", false)
	f.session("theirs", wd, "refactor the store", 7, false)
	f.srv.here = mine

	list := f.get()
	if len(list) != 2 {
		t.Fatalf("expected 2 agents, got %d: %+v", len(list), list)
	}
	var got, other fleet.Agent
	for _, a := range list {
		if a.Session == "mine" {
			got = a
		} else {
			other = a
		}
	}
	if got.State != fleet.Working || !got.Here || got.Steps != 3 {
		t.Errorf("the local working agent came back as %+v", got)
	}
	if !strings.Contains(got.Task, "make the tests pass") {
		t.Errorf("the card does not say what the turn was: %q", got.Task)
	}
	if other.State != fleet.Abandoned || other.Here {
		t.Errorf("the dead agent came back as %+v", other)
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
