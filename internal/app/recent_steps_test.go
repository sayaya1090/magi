package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A plugin that learns from past work (a routine learner's idle review) reads what earlier turns did:
// every tool call, its arguments and its result, in order, numbered by the user turn it belongs to.
func TestRecentToolStepsReadsPastTurnsWithTheirResults(t *testing.T) {
	first := asideCall("read", `{"path":"a.txt"}`)
	second := asideCall("read", `{"path":"b.txt"}`)
	second[0].ToolCall.CallID = "second"
	llm := &scriptLLM{steps: [][]port.ProviderEvent{first, asideText("one"), second, asideText("two")}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	for name, body := range map[string]string{"a.txt": "alpha body", "b.txt": "beta body"} {
		if err := os.WriteFile(filepath.Join(wd, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sid, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"read a", "read b"} {
		if err := a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Parts: []session.Part{{Kind: session.PartText, Text: text}},
			Actor: event.Actor{Kind: event.ActorUser, ID: "u"}}); err != nil {
			t.Fatal(err)
		}
		waitIdle(t, a, sid)
	}
	got, err := a.RecentToolSteps(ctx, wd, 4)
	if err != nil || len(got) != 1 || got[0].Session != string(sid) {
		t.Fatalf("sessions: %+v (%v)", got, err)
	}
	steps := got[0].Steps
	if len(steps) != 2 || steps[0].Turn != 1 || steps[1].Turn != 2 ||
		!strings.Contains(steps[0].Output, "alpha body") || !strings.Contains(steps[1].Output, "beta body") ||
		!strings.Contains(string(steps[1].Args), "b.txt") {
		t.Fatalf("steps: %+v", steps)
	}
	if other, _ := a.RecentToolSteps(ctx, t.TempDir(), 4); len(other) != 0 {
		t.Fatalf("another workspace's sessions were read: %+v", other)
	}
}
