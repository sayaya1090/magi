package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// refusesPrompts writes everything except a submitted prompt — the one write resurface makes.
type refusesPrompts struct {
	port.Store
}

func (r refusesPrompts) Append(ctx context.Context, sid session.SessionID, evs ...event.Event) ([]int64, error) {
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			return nil, errors.New("the disk is full")
		}
	}
	return r.Store.Append(ctx, sid, evs...)
}

// A queued request that could not be written back into the log will not run, because the re-run
// seeds from the log. The drain has nobody to return that to, so it has to be said — it used to be
// discarded with `_ =` and leave no trace at all.
func TestAPromptThatCannotBeRequeuedIsSaid(t *testing.T) {
	inner, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(refusesPrompts{inner}, &usageLLM{text: "reply"}, builtin.Default(), bus.New(), nil,
		Config{Permission: "allow"}))
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	a.resurface(context.Background(), sid, "m_origin", "please also run the tests")

	got := buf.String()
	if !strings.Contains(got, "will not run") || !strings.Contains(got, "the disk is full") {
		t.Fatalf("a lost requeue must be logged with its cause, got %q", got)
	}
}

// The same for every best-effort fact: a write the store refused names what was lost and why.
func TestABestEffortFactThatDidNotLandIsSaid(t *testing.T) {
	inner, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(refusesPrompts{inner}, &usageLLM{text: "reply"}, builtin.Default(), bus.New(), nil,
		Config{Permission: "allow"}))
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	a.appendBestEffort(context.Background(), sid, event.TypePromptSubmitted,
		event.Actor{Kind: event.ActorSystem, ID: "test"}, []byte(`{}`))

	got := buf.String()
	if !strings.Contains(got, string(event.TypePromptSubmitted)) || !strings.Contains(got, "the disk is full") {
		t.Fatalf("a refused best-effort write must be logged with its type and cause, got %q", got)
	}
}

// And for a note the loop leaves the model: one that did not land is advice never read.
func TestANoteThatDidNotLandIsSaid(t *testing.T) {
	inner, _ := jsonl.New(t.TempDir())
	a := closeAfter(t, New(refusesPrompts{inner}, &usageLLM{text: "reply"}, builtin.Default(), bus.New(), nil,
		Config{Permission: "allow"}))
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	a.notePromptText(context.Background(), sid, event.Actor{Kind: event.ActorSystem, ID: "steer"}, "look at the tests first")

	got := buf.String()
	if !strings.Contains(got, "steer") || !strings.Contains(got, "the disk is full") {
		t.Fatalf("a refused note must be logged with who left it and why it failed, got %q", got)
	}
}
