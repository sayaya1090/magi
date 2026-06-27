package jsonl

import (
	"context"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
)

// Truncate (rewind) keeps seq<=upToSeq, survives reopen, and continues seq.
func TestTruncate(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	ts := time.Now()

	s, _ := New(root)
	s.Append(ctx, "s1", created(wd, ts))
	for i := 0; i < 5; i++ { // seq 1..6
		s.Append(ctx, "s1", fact(event.TypePartAppended, ts))
	}
	if err := s.Truncate(ctx, "s1", 3); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	got, _ := s.Read(ctx, "s1", 0)
	if len(got) != 3 || got[len(got)-1].Seq != 3 {
		t.Fatalf("after truncate got %d events, last seq %d; want 3 and 3", len(got), got[len(got)-1].Seq)
	}
	// New append continues from 4 (not restart).
	out, _ := s.Append(ctx, "s1", fact(event.TypePartAppended, ts))
	if out[0] != 4 {
		t.Errorf("seq after truncate = %d, want 4", out[0])
	}
	// Survives reopen.
	s2, _ := New(root)
	got2, _ := s2.Read(ctx, "s1", 0)
	if len(got2) != 4 {
		t.Errorf("after reopen got %d events, want 4", len(got2))
	}
}

// Read on an unknown session returns nothing (no error).
func TestReadUnknownSession(t *testing.T) {
	s, _ := New(t.TempDir())
	got, err := s.Read(context.Background(), "ghost", 0)
	if err != nil || got != nil {
		t.Errorf("read unknown: got %v err %v, want nil,nil", got, err)
	}
}

// Append with no events is a no-op.
func TestAppendEmpty(t *testing.T) {
	s, _ := New(t.TempDir())
	out, err := s.Append(context.Background(), "s1")
	if err != nil || out != nil {
		t.Errorf("append empty: %v %v", out, err)
	}
}
