package jsonl

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// shrinkLineCap lowers the over-long-line threshold for the duration of a test so
// the skip path is exercised without writing 128 MiB. Returns a restore func.
func shrinkLineCap(t *testing.T) func() {
	t.Helper()
	prev := maxLogLineBytes
	maxLogLineBytes = 256 << 10 // 256 KiB — far above a normal event, tiny to write
	return func() { maxLogLineBytes = prev }
}

// seedTwoEvents writes a valid session (created + one fact) and returns its path.
func seedTwoEvents(t *testing.T, root, wd string, sid session.SessionID) string {
	t.Helper()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	created, _ := json.Marshal(event.SessionCreatedData{Workdir: wd, Agent: "default"})
	if _, err := s.Append(context.Background(), sid, event.Event{Type: event.TypeSessionCreated, TS: time.Now(), Data: created}); err != nil {
		t.Fatal(err)
	}
	note, _ := json.Marshal(map[string]string{"msg": "hello"})
	if _, err := s.Append(context.Background(), sid, event.Event{Type: event.TypeError, TS: time.Now(), Data: note}); err != nil {
		t.Fatal(err)
	}
	return s.sessionPath(wd, sid)
}

// A single line larger than one read buffer must not brick the store: New() indexes
// every log, so an over-long line there once raised bufio.Scanner "token too long"
// and failed startup/resume for every session in the directory. It must now be
// tolerated (skipped) like a short corrupt line, with the good events preserved.
func TestOverlongLineDoesNotBrickStore(t *testing.T) {
	defer shrinkLineCap(t)()
	root := t.TempDir()
	wd := "/tmp/proj"
	sid := session.SessionID("s_overlong")
	path := seedTwoEvents(t, root, wd, sid)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strings.Repeat("x", maxLogLineBytes+4096) + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	s, err := New(root)
	if err != nil {
		t.Fatalf("New() bricked by an overlong line: %v", err)
	}
	evs, err := s.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatalf("Read() bricked by an overlong line: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("good events lost around overlong line: got %d, want 2", len(evs))
	}
}

// An over-long line sandwiched between good events is skipped without consuming its
// neighbors: events before and after it all survive a reload.
func TestOverlongLineMidFileKeepsNeighbors(t *testing.T) {
	defer shrinkLineCap(t)()
	root := t.TempDir()
	wd := "/tmp/proj2"
	sid := session.SessionID("s_mid")
	path := seedTwoEvents(t, root, wd, sid)

	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString(strings.Repeat("y", maxLogLineBytes+8192) + "\n")
	f.Close()

	s, err := New(root)
	if err != nil {
		t.Fatalf("New bricked: %v", err)
	}
	note, _ := json.Marshal(map[string]string{"msg": "after"})
	if _, err := s.Append(context.Background(), sid, event.Event{Type: event.TypeError, TS: time.Now(), Data: note}); err != nil {
		t.Fatal(err)
	}
	s2, _ := New(root)
	evs, err := s2.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatalf("Read bricked: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("neighbors of overlong line lost: got %d events, want 3", len(evs))
	}
}
