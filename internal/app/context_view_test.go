package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
)

func TestContextView(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	sid := startSession(t, a, wd)
	runOn(t, a, sid, "do a thing")

	v, err := a.ContextView(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Context window", "used", "messages:", "compactions:"} {
		if !strings.Contains(v, want) {
			t.Fatalf("context view missing %q:\n%s", want, v)
		}
	}
}

func TestContextViewAfterCompaction(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{})
	ctx := context.Background()
	sid := startSession(t, a, wd)
	runOn(t, a, sid, "do a thing")
	if err := a.Compact(ctx, command.Compact{SessionID: sid, Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
		t.Fatal(err)
	}
	v, err := a.ContextView(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v, "compactions: 1") {
		t.Fatalf("context view should report the compaction:\n%s", v)
	}
	// The before/after token figures must be real (not the old always-zero after).
	if strings.Contains(v, "→0 tok") {
		t.Fatalf("compaction tokens should be populated, got:\n%s", v)
	}
}

func TestCommas(t *testing.T) {
	cases := map[int]string{0: "0", 12: "12", 999: "999", 1000: "1,000", 12345: "12,345", 1234567: "1,234,567", -1000: "-1,000"}
	for n, want := range cases {
		if got := commas(n); got != want {
			t.Errorf("commas(%d) = %q, want %q", n, got, want)
		}
	}
}
