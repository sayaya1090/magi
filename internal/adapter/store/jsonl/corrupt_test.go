package jsonl

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A corrupt or torn line in a session log must not brick the store: New (which
// indexes every log), Read, and ListSessions all skip the bad line and return the
// valid events. Otherwise one bad line takes down resume / the whole store.
func TestReadToleratesCorruptLine(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	ts := time.Now()
	if _, err := s.Append(ctx, "s1", created(wd, ts)); err != nil { // seq 1
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, "s1", fact(event.TypePromptSubmitted, ts)); err != nil { // seq 2
		t.Fatal(err)
	}

	// Inject a garbage line and a torn (partial, unterminated) trailing line.
	f, err := os.OpenFile(s.sessionPath(wd, "s1"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("this is not json\n")
	f.WriteString(`{"type":"part.appended","seq":3,`) // torn: no closing brace / newline
	f.Close()

	// A fresh store must still open despite the corruption.
	s2, err := New(dir)
	if err != nil {
		t.Fatalf("New on a store with one corrupt line should not fail: %v", err)
	}
	evs, err := s2.Read(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("Read should tolerate the corrupt line: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("want the 2 valid events (garbage skipped), got %d", len(evs))
	}
	if metas, err := s2.ListSessions(ctx, wd); err != nil || len(metas) != 1 {
		t.Fatalf("ListSessions should tolerate corruption: metas=%d err=%v", len(metas), err)
	}
}

// Appending after a TORN (unterminated, no-newline) final line must not glue the new
// event onto the broken line — otherwise both parse as one unparseable line and the
// freshly-appended event is silently lost. The store writes a separating newline first.
func TestAppendAfterTornLineSurvives(t *testing.T) {
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	ts := time.Now()
	if _, err := s.Append(ctx, "s1", created(wd, ts)); err != nil { // seq 1, valid
		t.Fatal(err)
	}
	// Inject a torn trailing line directly (a partially-flushed write: no newline).
	f, err := os.OpenFile(s.sessionPath(wd, "s1"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"type":"part.appended","seq":2,`) // torn: no closing brace / newline
	f.Close()

	// A reopened store appends a new event — it must survive, not be lost to the torn tail.
	s2, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Append(ctx, "s1", fact(event.TypePromptSubmitted, ts)); err != nil {
		t.Fatal(err)
	}
	evs, err := s2.Read(ctx, "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	// The valid created event + the newly appended prompt both survive; only the torn
	// middle line is skipped.
	if len(evs) != 2 {
		t.Fatalf("torn-tail append lost an event: want 2 valid events, got %d", len(evs))
	}
	sawPrompt := false
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			sawPrompt = true
		}
	}
	if !sawPrompt {
		t.Fatal("the appended event must survive a torn final line")
	}
}
