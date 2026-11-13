package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/auth"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The double answers the recovery doors like a daemon a restart left one uncertain write in: the
// state is "check this" until the person has said something about the call, and then it is
// resumable. That is enough to tell a page that redraws from the daemon's re-read state from one
// that redraws from what it sent.
func (r *recordingEngine) RecoveryStateOf(_ context.Context, sid string) app.RecoveryState {
	for _, s := range r.seen() {
		if strings.HasPrefix(s, "resolve:") {
			return app.RecoveryState{Kind: app.RecoveryResumable}
		}
	}
	return app.RecoveryState{Kind: app.RecoveryNeedsCheck, Uncertain: []app.UncertainCall{
		{CallID: "c_w", Tool: "write", Target: "notes.md"}}}
}

func (r *recordingEngine) ResolveCall(_ context.Context, sid, callID, verdict, note string) error {
	return r.note("resolve:" + callID + ":" + verdict + ":" + note)
}

func (r *recordingEngine) RetryRecovery(_ context.Context, sid string) error {
	return r.note("retry")
}

func decodeRecovery(t *testing.T, body string) app.RecoveryState {
	t.Helper()
	var st app.RecoveryState
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		t.Fatalf("not a recovery state: %v: %q", err, body)
	}
	return st
}

// What a restart left is read from the daemon — the only process that knows a recovery it could
// not finish — and a verdict goes to the daemon that owns the log. The page is answered with the
// state as the daemon reads it afterwards, so it redraws from the truth rather than from its guess.
func TestTheConsoleReadsAndSettlesWhatARestartLeft(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "cut", eng)
	f.session("cut", wd, "write the notes", 1, false)
	q := "?d=" + url.QueryEscape(sock)

	w := get(t, f.srv.recovery, "/recovery"+q)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /recovery replied %d: %s", w.Code, w.Body.String())
	}
	st := decodeRecovery(t, w.Body.String())
	if st.Kind != app.RecoveryNeedsCheck || len(st.Uncertain) != 1 || st.Uncertain[0].Target != "notes.md" {
		t.Fatalf("the page was told %+v, want the one uncertain write", st)
	}

	w = post(t, f.srv, f.srv.recovery, "/recovery"+q, url.Values{
		"call": {"c_w"}, "verdict": {"not_ran"}, "note": {"the file is absent"}})
	if w.Code != http.StatusOK {
		t.Fatalf("resolving replied %d: %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 1 || got[0] != "resolve:c_w:not_ran:the file is absent" {
		t.Fatalf("the daemon received %v, want the verdict as sent", got)
	}
	if st := decodeRecovery(t, w.Body.String()); st.Kind != app.RecoveryResumable {
		t.Fatalf("after the verdict the page was told %q, want the daemon's re-read state", st.Kind)
	}

	if w := post(t, f.srv, f.srv.recovery, "/recovery"+q, url.Values{"retry": {"1"}}); w.Code != http.StatusOK {
		t.Fatalf("retry replied %d: %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 2 || got[1] != "retry" {
		t.Fatalf("the daemon received %v, want a retry second", got)
	}
}

func TestTheConsoleRefusesAVerdictItCannotSend(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "cut", eng)
	f.session("cut", wd, "write the notes", 1, false)
	q := "?d=" + url.QueryEscape(sock)

	for _, bad := range []url.Values{
		{"verdict": {"ran"}},
		{"call": {"c_w"}},
		{"call": {"c_w"}, "verdict": {"maybe"}},
	} {
		if w := post(t, f.srv, f.srv.recovery, "/recovery"+q, bad); w.Code != http.StatusBadRequest {
			t.Errorf("%v replied %d, want 400", bad, w.Code)
		}
	}
	if got := eng.seen(); len(got) != 0 {
		t.Fatalf("a refused verdict reached the daemon: %v", got)
	}
}

// Saying what happened to a call is the same weight as answering a blocked tool: it decides what
// the companion may do next, without deciding what it works on.
func TestSettlingARestartNeedsTheAnswerPermission(t *testing.T) {
	if need, ok := mayWrite["/recovery"]; !ok || need != auth.Answer {
		t.Fatalf("/recovery needs %v (classified %t), want answer", need, ok)
	}
}

// The banner follows the log without a reload: a conversation a restart cut arrives on the stream
// as its own frame, the way the open turn does, because it is a fact about the session and not a row.
func TestTheStreamSaysWhatARestartLeft(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "cut", true)
	f.session("cut", wd, "write the notes", 0, false)
	ed, _ := json.Marshal(event.ErrorData{Message: "interrupted", Code: "interrupted", Resumable: true})
	fd, _ := json.Marshal(event.TurnFinishedData{})
	if _, err := f.store.Append(context.Background(), session.SessionID("cut"),
		event.Event{Type: event.TypeError, Data: ed, TS: time.Now()},
		event.Event{Type: event.TypeTurnFinished, Data: fd, TS: time.Now()}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	w := newStreamRecorder()
	go f.srv.events(w, httptest.NewRequest(http.MethodGet, "/events?d="+url.QueryEscape(sock), nil).WithContext(ctx))
	if !w.waitFor(t, "event: recovery", 3*time.Second) {
		t.Fatalf("the stream never said what the restart left: %s", w.body())
	}
	if b := w.body(); !strings.Contains(b, string(app.RecoveryResumable)) {
		t.Fatalf("the recovery frame does not say the turn can be continued: %s", b)
	}
}
