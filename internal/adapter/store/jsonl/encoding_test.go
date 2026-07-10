package jsonl

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// The JSONL store is the one boundary that turns events into bytes on disk and
// back, so it is the likeliest place an encoding regression could hide. A prompt
// carrying non-ASCII text — Korean, an emoji (astral/surrogate-pair range), and a
// combining sequence — must persist and replay across a store reopen as the exact
// same runes, and the on-disk line must hold raw UTF-8, not ASCII-escaped \uXXXX.
func TestPersistPreservesUnicode(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	ts := time.Now()

	const text = "안녕하세요 🌟 café é" // Korean + astral emoji + precomposed & combining
	d, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m1",
		Parts:     []session.Part{{Kind: session.PartText, Text: text}},
	})

	s1, _ := New(root)
	if _, err := s1.Append(ctx, "s1", created(wd, ts)); err != nil {
		t.Fatalf("append created: %v", err)
	}
	if _, err := s1.Append(ctx, "s1", event.Event{Type: event.TypePromptSubmitted, TS: ts, Data: d}); err != nil {
		t.Fatalf("append prompt: %v", err)
	}

	s2, err := New(root) // reopen: forces a read back from the raw JSONL file
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got, err := s2.Read(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var found bool
	for _, e := range got {
		if e.Type != event.TypePromptSubmitted {
			continue
		}
		if strings.Contains(string(e.Data), `\u`) {
			t.Fatalf("persisted line is ASCII-escaped: %s", e.Data)
		}
		var p event.PromptSubmittedData
		if err := json.Unmarshal(e.Data, &p); err != nil {
			t.Fatalf("unmarshal replayed prompt: %v", err)
		}
		if len(p.Parts) != 1 || p.Parts[0].Text != text {
			t.Fatalf("replayed text = %q, want %q (store must round-trip UTF-8 losslessly)", p.Parts[0].Text, text)
		}
		found = true
	}
	if !found {
		t.Fatal("prompt.submitted not found after reopen")
	}
}
