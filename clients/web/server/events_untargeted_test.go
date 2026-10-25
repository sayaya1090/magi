package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The fleet screen's stream is addressed at nobody, and must stay so in a workspace that has a
// companion of its own: that companion's open turn is not the screen's, and drawn as the
// window-wide turn bar it ran on a list whose every row was idle.
func TestAnUntargetedStreamDoesNotCarryTheWorkspaceCompanionsTurn(t *testing.T) {
	f := newFleetFixture(t)
	wd := shortTempDir(t)
	sock := f.daemonAt(wd, "busy", true)
	f.session("busy", wd, "still thinking", 1, false) // a turn that has not finished
	f.srv.here = sock

	stream := func(path string) *streamRecorder {
		ctx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		w := newStreamRecorder()
		go f.srv.events(w, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
		return w
	}

	// The same companion, asked for by name, does report its turn — so the fixture really is mid-turn.
	aimed := stream("/events?d=" + url.QueryEscape(sock))
	if !aimed.waitFor(t, "event: turn", 3*time.Second) {
		t.Fatalf("the fixture's companion is not mid-turn, so this test measures nothing: %s", aimed.body())
	}

	nobody := stream("/events")
	if !nobody.waitFor(t, "event: fleet", 3*time.Second) {
		t.Fatalf("the untargeted stream sent no roster: %s", nobody.body())
	}
	time.Sleep(1500 * time.Millisecond)
	if b := nobody.body(); strings.Contains(b, "event: turn") || strings.Contains(b, "still thinking") {
		t.Errorf("the list screen was sent the workspace companion's turn or transcript: %s", b)
	}
}
