package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/cluster"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Known reads both halves together: with nobody live on this machine, it is the sightings alone.
func TestKnownReadsBothHalvesTogether(t *testing.T) {
	home := t.TempDir()
	if err := writeMembers(home, []cluster.Member{{
		Host: "otherbox", Socket: "/tmp/daemon-x.sock", Name: "x", Seen: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	ms := Known(home, time.Now())
	if len(ms) != 1 || ms[0].Host != "otherbox" {
		t.Fatalf("the sighting half must arrive, got %+v", ms)
	}
}

// Moved rewrites which conversation the record names, and the readers read it back.
func TestMovedRewritesTheRecordsSession(t *testing.T) {
	home := t.TempDir()
	sock := filepath.Join(home, "daemon-a.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(sock, "/w/a", "s_first", Identity{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := Moved(sock, session.SessionID("s_second")); err != nil {
		t.Fatal(err)
	}
	if sid, err := PublishedSession(sock); err != nil || sid != "s_second" {
		t.Fatalf("the record names the new conversation, got (%q, %v)", sid, err)
	}
}

// Find answers only from the published set — a path from a page must not become a path this
// process dials.
func TestFindMatchesOnlyThePublishedSet(t *testing.T) {
	home := t.TempDir()
	sock := filepath.Join(home, "daemon-b.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(sock, "/w/b", "s_b", Identity{}); err != nil {
		t.Fatal(err)
	}
	if in, err := Find(home, sock); err != nil || in.Workdir != "/w/b" {
		t.Fatalf("a published socket resolves, got (%+v, %v)", in, err)
	}
	if _, err := Find(home, filepath.Join(home, "daemon-nope.sock")); err == nil {
		t.Fatal("an unpublished path must be refused by name")
	}
}

// A receipt is the handle handed-over work is asked about by: minted at once, positioned when the
// work starts, and unknown when expired or invented.
func TestReceiptsRoundTrip(t *testing.T) {
	r := NewReceipts()
	id, err := r.Give("s_work")
	if err != nil || id == "" {
		t.Fatal(err)
	}
	sid, since, started, ok := r.Where(id)
	if !ok || sid != "s_work" || started || since != 0 {
		t.Fatalf("minted but not started: (%q %d %v %v)", sid, since, started, ok)
	}
	r.Started(id, 41)
	if _, since, started, _ := r.Where(id); !started || since != 41 {
		t.Fatalf("the position the answer is found after, got (%d, %v)", since, started)
	}
	if _, _, _, ok := r.Where("deadbeef"); ok {
		t.Fatal("an invented receipt is simply not found")
	}
	if _, _, _, ok := r.Where(""); ok {
		t.Fatal("the empty receipt is nobody's")
	}
	r.Started("unknown", 7) // a no-op, not a panic
}

// Waiting.Event rebuilds the prompt a viewer in another process can answer — above all the call
// id, because that is what an answer is addressed to.
func TestWaitingEventRebuildsTheAskablePrompt(t *testing.T) {
	var none *Waiting
	if _, err := none.Event("s"); err == nil {
		t.Fatal("no pending prompt is an error, not an empty event")
	}
	w := &Waiting{ID: "call_7", Kind: "question", What: "which way?", Options: []string{"a", "b"}}
	ev, err := w.Event(session.SessionID("s_q"))
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != event.TypeQuestionRequested || ev.SessionID != "s_q" {
		t.Fatalf("a question travels as question.requested, got %+v", ev)
	}
	if !strings.Contains(string(ev.Data), "call_7") {
		t.Fatalf("the call id is what an answer is addressed to; it must ride the data: %s", ev.Data)
	}
	var d map[string]any
	if json.Unmarshal(ev.Data, &d) != nil {
		t.Fatal("the payload must be the wire's own JSON")
	}
}
