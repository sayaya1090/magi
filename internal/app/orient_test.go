package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// newOrientApp builds an App over a temp store and a session rooted at a caller-controlled
// workdir, so a test can seed build anchors into that directory before orienting. The provider
// is never called — maybeOrient only reads/writes the store — so the scripted steerLLM suffices.
func newOrientApp(t *testing.T, workdir string) (*App, session.SessionID) {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := New(store, &steerLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow"})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: workdir})
	if err != nil {
		t.Fatal(err)
	}
	return a, sid
}

// orientText returns the concatenated text of every durable orient record (system actor "orient").
func orientText(t *testing.T, a *App, sid session.SessionID) string {
	t.Helper()
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorSystem && e.Actor.ID == "orient" {
			var d event.PromptSubmittedData
			if json.Unmarshal(e.Data, &d) == nil {
				b.WriteString(joinPartText(d.Parts))
			}
		}
	}
	return b.String()
}

func seedPrompt(t *testing.T, a *App, sid session.SessionID, text string) {
	t.Helper()
	if err := a.appendPromptText(context.Background(), sid, event.Actor{Kind: event.ActorUser, ID: "u"}, text); err != nil {
		t.Fatal(err)
	}
}

const nonTrivial = "please implement the new configuration parser and add tests for it"

// The build/verify anchors present in the workdir must land in the main context as a factual
// orient message, so the executor (and the planner, which reads the window) starts grounded in
// the real environment rather than the instruction prose alone.
func TestOrientLandsBuildAnchors(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "Makefile"), []byte("test:\n\tgo test ./...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wd, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, sid := newOrientApp(t, wd)
	seedPrompt(t, a, sid, nonTrivial)

	a.maybeOrient(context.Background(), a.sessionInfo(context.Background(), sid))

	got := orientText(t, a, sid)
	for _, want := range []string{"Working environment", "Makefile", "go test ./...", "go.mod"} {
		if !strings.Contains(got, want) {
			t.Errorf("orient message missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// maybeOrient must inject exactly once per session: a second call is a no-op (grounded latch).
func TestOrientIdempotent(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "Makefile"), []byte("all:\n\techo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, sid := newOrientApp(t, wd)
	seedPrompt(t, a, sid, nonTrivial)
	s := a.sessionInfo(context.Background(), sid)

	a.maybeOrient(context.Background(), s)
	a.maybeOrient(context.Background(), s)

	if n := strings.Count(orientText(t, a, sid), "Working environment"); n != 1 {
		t.Fatalf("orient injected %d times, want exactly 1", n)
	}
}

// A trivial one-clause prompt is handled in one shot; grounding it would waste context, so
// maybeOrient injects nothing (but still latches grounded, so it never fires later either).
func TestOrientTrivialSkip(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "Makefile"), []byte("all:\n\techo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, sid := newOrientApp(t, wd)
	seedPrompt(t, a, sid, "fix typo")

	a.maybeOrient(context.Background(), a.sessionInfo(context.Background(), sid))

	if got := orientText(t, a, sid); got != "" {
		t.Fatalf("trivial prompt should not be oriented, got:\n%s", got)
	}
}

// An empty (or unreadable) workdir yields no anchors: maybeOrient must degrade gracefully and
// inject nothing rather than an empty/placeholder grounding block.
func TestOrientEmptyWorkdirGraceful(t *testing.T) {
	a, sid := newOrientApp(t, t.TempDir()) // empty dir: no anchors
	seedPrompt(t, a, sid, nonTrivial)

	a.maybeOrient(context.Background(), a.sessionInfo(context.Background(), sid))

	if got := orientText(t, a, sid); got != "" {
		t.Fatalf("empty workdir should inject nothing, got:\n%s", got)
	}
}

// Orienting is ON by default; MAGI_ORIENT=off (the A/B knob) is the only way to suppress it, so an
// unset or truthy value orients and only an explicit falsey value opts out.
func TestOrientEnabledFlag(t *testing.T) {
	t.Setenv("MAGI_ORIENT", "")
	if !orientEnabled() {
		t.Fatal("unset MAGI_ORIENT should default ON")
	}
	t.Setenv("MAGI_ORIENT", "off")
	if orientEnabled() {
		t.Fatal("MAGI_ORIENT=off should be OFF")
	}
	t.Setenv("MAGI_ORIENT", "0")
	if orientEnabled() {
		t.Fatal("MAGI_ORIENT=0 should be OFF")
	}
	t.Setenv("MAGI_ORIENT", "1")
	if !orientEnabled() {
		t.Fatal("MAGI_ORIENT=1 should be ON")
	}
}
