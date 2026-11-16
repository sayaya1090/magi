package jsonl

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// SyncLog forces one session's log down on demand — what the app asks before it waits on a person.
func TestSyncLogForcesTheSessionLog(t *testing.T) {
	var n int
	prev := syncFile
	syncFile = func(f *os.File) error { n++; return prev(f) }
	t.Cleanup(func() { syncFile = prev })
	s := newStore(t)
	ctx := context.Background()
	sid := session.SessionID("s_wait")
	if _, err := s.Append(ctx, sid, created(wd, time.Now()), fact(event.TypePartAppended, time.Now())); err != nil {
		t.Fatal(err)
	}
	n = 0
	if err := s.SyncLog(ctx, sid); err != nil || n != 1 {
		t.Fatalf("SyncLog: err %v, %d syncs, want 1", err, n)
	}
	if err := s.SyncLog(ctx, session.SessionID("s_none")); err != nil {
		t.Errorf("a session with no log has nothing to sync: %v", err)
	}
}
