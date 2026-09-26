package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/command"
)

// refusingEngine takes nothing: every prompt it is handed fails to be written.
type refusingEngine struct{ Engine }

var errDiskFull = errors.New("the disk is full")

func (refusingEngine) Submit(context.Context, command.SubmitPrompt) error { return errDiskFull }
func (refusingEngine) Steer(context.Context, command.SubmitPrompt) error  { return errDiskFull }

// runCmd runs a command and everything it batches, collecting the messages that come back.
func runCmd(c tea.Cmd) []tea.Msg {
	if c == nil {
		return nil
	}
	msg := c()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, cc := range b {
			out = append(out, runCmd(cc)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// A prompt the engine refused used to leave the spinner turning over a message that was never
// sent: the error was discarded, no turn started, and no event ever came to stop it.
func TestARefusedPromptStopsTheSpinnerAndGivesTheTextBack(t *testing.T) {
	m := newTestModel(t)
	m.app = refusingEngine{m.app}
	before := len(m.blocks)

	cmd := m.submit("please fix the build")
	if !m.running {
		t.Fatal("precondition: a submit turns the spinner on")
	}
	var failed *sendFailedMsg
	for _, msg := range runCmd(cmd) {
		if f, ok := msg.(sendFailedMsg); ok {
			failed = &f
		}
	}
	if failed == nil {
		t.Fatal("the refused Submit produced no sendFailedMsg — the error was dropped")
	}
	next, _ := m.Update(*failed)
	m = next.(Model)

	if m.running {
		t.Error("the spinner is still on for a turn that never started")
	}
	if len(m.blocks) != before {
		t.Errorf("the transcript still shows the unsent prompt: %d blocks, want %d", len(m.blocks), before)
	}
	if got := m.ta.Value(); got != "please fix the build" {
		t.Errorf("the input should hold the text again, got %q", got)
	}
	if !strings.Contains(m.snackbar, "not sent") || !strings.Contains(m.snackbar, "the disk is full") {
		t.Errorf("the reason should be on screen, snackbar = %q", m.snackbar)
	}
}

// A refused steer leaves the running turn alone — it is still running — and gives the text back.
func TestARefusedSteerLeavesTheTurnRunning(t *testing.T) {
	m := newTestModel(t)
	m.app = refusingEngine{m.app}
	m.running = true
	before := len(m.blocks)

	var failed *sendFailedMsg
	for _, msg := range runCmd(m.steer("also update the docs")) {
		if f, ok := msg.(sendFailedMsg); ok {
			failed = &f
		}
	}
	if failed == nil {
		t.Fatal("the refused Steer produced no sendFailedMsg")
	}
	next, _ := m.Update(*failed)
	m = next.(Model)

	if !m.running {
		t.Error("a refused steer must not stop the turn that is running")
	}
	if len(m.blocks) != before {
		t.Errorf("the transcript still shows the unsent steer: %d blocks, want %d", len(m.blocks), before)
	}
	if got := m.ta.Value(); got != "also update the docs" {
		t.Errorf("the input should hold the text again, got %q", got)
	}
}
