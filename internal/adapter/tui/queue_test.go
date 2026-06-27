package tui

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/port"
)

// stubLLM streams a single text reply and finishes — enough to drive the model.
type stubLLM struct{}

func (stubLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: "ok"}
	ch <- port.ProviderEvent{Type: port.ProviderFinish}
	close(ch)
	return ch, nil
}

func newTestModel(t *testing.T) Model {
	t.Helper()
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := app.New(store, stubLLM{}, builtin.Default(), bus.New(), nil, app.Config{Permission: "allow"})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return New(context.Background(), a, sid, "m", t.TempDir(), true, "")
}

// Enter while running steers the message into the running turn: it appears in the
// transcript immediately and the input is cleared (no queue).
func TestEnterWhileRunningSteers(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.ta.SetValue("keep going but also do X")
	before := len(m.blocks)
	if _, handled := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter should be handled while running")
	}
	if len(m.blocks) != before+1 {
		t.Fatalf("steer should add a user block immediately, blocks %d→%d", before, len(m.blocks))
	}
	if m.blocks[len(m.blocks)-1].kind != blockUser {
		t.Fatal("last block should be the steered user message")
	}
	if m.ta.Value() != "" {
		t.Fatalf("input should be cleared after steering, got %q", m.ta.Value())
	}
}

// Safe slash commands run while working; unsafe ones are rejected.
func TestSafeWhileRunning(t *testing.T) {
	safe := []string{"/help", "/model", "/agents", "/tools", "/sessions", "/diff", "/permission"}
	for _, c := range safe {
		if !safeWhileRunning(c) {
			t.Errorf("%s should be safe while running", c)
		}
	}
	unsafe := []string{"/rewind", "/resume", "/clear", "/init", "/ultra", "/compact", "/quit"}
	for _, c := range unsafe {
		if safeWhileRunning(c) {
			t.Errorf("%s should NOT be safe while running", c)
		}
	}
}

// An unsafe slash command while running is rejected (no transcript change).
func TestUnsafeSlashRejectedWhileRunning(t *testing.T) {
	m := newTestModel(t)
	m.running = true
	m.ta.SetValue("/clear")
	before := len(m.blocks)
	m.handleKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(m.blocks) != before {
		t.Fatal("unsafe slash command should not modify the transcript while running")
	}
}
