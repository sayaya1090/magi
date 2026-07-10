package app

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// SetUserLabel is the choke point for a plugin-injected transcript user label; it
// stores the label on the session and broadcasts user.label.changed so the TUI
// re-reads it from one signal (mirrors SetModel). A later reader sees the value.
func TestSetUserLabelBroadcastsEvent(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &fakeLLM{}, builtin.Default(), bus.New(), nil, Config{
		Model: session.ModelRef{Provider: "openai", Model: "base-model"},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, cancelSub, _ := a.Subscribe(ctx, sid, 0)
	defer cancelSub()

	a.SetUserLabel(sid, "Alice")

	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type != event.TypeUserLabelChanged {
				continue
			}
			var d event.UserLabelData
			if json.Unmarshal(e.Data, &d) != nil || d.Label != "Alice" {
				t.Fatalf("user.label.changed payload = %q, want Alice", d.Label)
			}
			if got := a.UserLabel(sid); got != "Alice" {
				t.Fatalf("UserLabel = %q after SetUserLabel, want Alice", got)
			}
			return
		case <-deadline:
			t.Fatal("no user.label.changed event after SetUserLabel")
		}
	}
}

// A non-ASCII label survives storage and broadcast losslessly: UserLabel reads back
// the exact runes, and the emitted user.label.changed payload carries raw UTF-8 —
// never an ASCII-escaped \uXXXX form. This is the app-side core-clean proof for the
// report where "you" rendered as the literal 변냁재: magi's own marshal path
// preserves Korean, so an escaped label can only be a string a plugin escaped before
// handing it to us.
func TestSetUserLabelUnicodeRoundTrip(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &fakeLLM{}, builtin.Default(), bus.New(), nil, Config{
		Model: session.ModelRef{Provider: "openai", Model: "base-model"},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, cancelSub, _ := a.Subscribe(ctx, sid, 0)
	defer cancelSub()

	const want = "변냁재" // U+BCC0 U+B0C1 U+C7AC — the runes the report showed escaped
	a.SetUserLabel(sid, want)

	if got := a.UserLabel(sid); got != want {
		t.Fatalf("UserLabel = %q, want %q (in-memory state must not mangle unicode)", got, want)
	}

	deadline := time.After(3 * time.Second)
	for {
		select {
		case e := <-ch:
			if e.Type != event.TypeUserLabelChanged {
				continue
			}
			if bytes.Contains(e.Data, []byte(`\u`)) {
				t.Fatalf("emitted payload is ASCII-escaped: %s — magi must marshal raw UTF-8", e.Data)
			}
			if !bytes.Contains(e.Data, []byte(want)) {
				t.Fatalf("emitted payload %s does not carry the raw UTF-8 label %q", e.Data, want)
			}
			var d event.UserLabelData
			if err := json.Unmarshal(e.Data, &d); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if r := []rune(d.Label); len(r) != 3 || r[0] != 0xBCC0 || r[1] != 0xB0C1 || r[2] != 0xC7AC {
				t.Fatalf("payload decoded to %d runes %U, want U+BCC0 U+B0C1 U+C7AC", len(r), r)
			}
			return
		case <-deadline:
			t.Fatal("no user.label.changed event after SetUserLabel")
		}
	}
}

// An empty or whitespace-only label is ignored: no state change, no broadcast.
func TestSetUserLabelIgnoresEmpty(t *testing.T) {
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &fakeLLM{}, builtin.Default(), bus.New(), nil, Config{
		Model: session.ModelRef{Provider: "openai", Model: "base-model"},
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})

	a.SetUserLabel(sid, "   ")
	if got := a.UserLabel(sid); got != "" {
		t.Fatalf("whitespace label should be ignored, got %q", got)
	}
}
