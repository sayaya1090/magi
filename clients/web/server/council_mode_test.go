package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

func (r *recordingEngine) SetCouncil(sid session.SessionID, enabled bool) error {
	return r.note("council:" + string(sid) + ":" + map[bool]string{true: "true", false: "false"}[enabled])
}

func TestCouncilModePinsTheDisplayedSessionAndValidatesBoolean(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "council-mode", eng)
	f.session("council-mode", wd, "check council", 1, false)
	probe := httptest.NewRequest(http.MethodGet, "/council-mode?d="+url.QueryEscape(sock), nil)
	info, err := f.srv.target(probe)
	if err != nil {
		t.Fatal(err)
	}
	q := "?d=" + url.QueryEscape(sock) + "&session=" + url.QueryEscape(info.Session)
	read := get(t, f.srv.councilMode, "/council-mode"+q)
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"enabled":false`) {
		t.Fatalf("read: %d %s", read.Code, read.Body.String())
	}
	for _, values := range []url.Values{{"session": {"old"}, "enabled": {"true"}}, {"session": {info.Session}, "enabled": {"yes"}}, {"enabled": {"true"}}} {
		w := post(t, f.srv, f.srv.councilMode, "/council-mode?d="+url.QueryEscape(sock), values)
		if w.Code < 400 {
			t.Fatalf("invalid request accepted: %v", values)
		}
	}
	if len(eng.seen()) != 0 {
		t.Fatalf("invalid calls reached engine: %v", eng.seen())
	}
	w := post(t, f.srv, f.srv.councilMode, "/council-mode?d="+url.QueryEscape(sock), url.Values{"session": {info.Session}, "enabled": {"true"}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if got := eng.seen(); len(got) != 1 || got[0] != "council:"+info.Session+":true" {
		t.Fatalf("engine received %v", got)
	}
}

func TestCompactionRejectsAStaleDisplayedSession(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	eng := &recordingEngine{}
	sock := f.liveDaemon(t, wd, "compact-target", eng)
	f.session("compact-target", wd, "compact", 1, false)
	w := post(t, f.srv, f.srv.compact, "/compact?d="+url.QueryEscape(sock), url.Values{"session": {"stale"}})
	if w.Code < 400 {
		t.Fatalf("stale compaction accepted: %d", w.Code)
	}
	if len(eng.seen()) != 0 {
		t.Fatalf("stale compaction reached engine: %v", eng.seen())
	}
}
