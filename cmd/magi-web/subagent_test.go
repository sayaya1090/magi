package main

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// child writes a session that names another as its parent — what a spawned subagent's log is.
func (f *fleetFixture) child(sid, workdir, parent, role, task string) {
	f.t.Helper()
	ev := func(t event.Type, d any) event.Event {
		b, err := json.Marshal(d)
		if err != nil {
			f.t.Fatal(err)
		}
		return event.Event{Type: t, Data: b, TS: time.Now()}
	}
	evs := []event.Event{
		ev(event.TypeSessionCreated, event.SessionCreatedData{Workdir: workdir, Parent: parent, Agent: role}),
		ev(event.TypePromptSubmitted, event.PromptSubmittedData{
			MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: task}}}),
		ev(event.TypeTurnFinished, event.TurnFinishedData{}),
	}
	if _, err := f.store.Append(context.Background(), session.SessionID(sid), evs...); err != nil {
		f.t.Fatal(err)
	}
}

// The children of a turn are the sessions that name it as their parent.
//
// Read from the store rather than from the daemon's register of running children: that register is
// in another process, and what is durable is the child's own log — which is also what survives the
// daemon, and the week after a run is when somebody asks what a subagent actually did.
func TestSubagentsAreTheSessionsThatNameThisOneAsParent(t *testing.T) {
	f := newFleetFixture(t)
	sock := f.daemonAt("/w/design", "s_parent", true)
	f.session("s_parent", "/w/design", "the turn", 0, true)
	f.child("s_kid", "/w/design", "s_parent", "scout", "go and look")
	f.session("s_sibling", "/w/design", "an unrelated turn", 0, true)

	w := get(t, f.srv.subagents, "/subagents?d="+url.QueryEscape(sock))
	if w.Code != 200 {
		t.Fatalf("/subagents answered %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "s_kid") || !strings.Contains(body, "scout") {
		t.Errorf("the child is missing: %s", body)
	}
	// A session of the same workspace that is nobody's child is not a subagent of this turn.
	if strings.Contains(body, "s_sibling") {
		t.Errorf("an unrelated session is listed as a child: %s", body)
	}
}

// A session id arriving from a page is checked against the sessions this workspace owns.
//
// Otherwise "which session" is an argument for reading any log on the machine, and it comes from a
// URL — the same class of input the ?d= allowlist exists for one layer up.
func TestATranscriptIsOnlyReadForASessionThisWorkspaceOwns(t *testing.T) {
	f := newFleetFixture(t)
	sock := f.daemonAt("/w/design", "s_mine", true)
	f.session("s_mine", "/w/design", "the work", 0, true)
	f.session("s_elsewhere", "/w/other", "somebody else's work", 0, true)

	if w := get(t, f.srv.transcript, "/transcript?d="+url.QueryEscape(sock)+"&session=s_mine"); w.Code != 200 {
		t.Errorf("a session of this workspace answered %d: %s", w.Code, w.Body.String())
	} else if !strings.Contains(w.Body.String(), "the work") {
		t.Errorf("the transcript came back without its own content: %s", w.Body.String())
	}
	if w := get(t, f.srv.transcript, "/transcript?d="+url.QueryEscape(sock)+"&session=s_elsewhere"); w.Code == 200 {
		t.Errorf("a session of another workspace was served: %s", w.Body.String())
	}
	if w := get(t, f.srv.transcript, "/transcript?d="+url.QueryEscape(sock)); w.Code != 400 {
		t.Errorf("naming no session answered %d, want 400", w.Code)
	}
}
