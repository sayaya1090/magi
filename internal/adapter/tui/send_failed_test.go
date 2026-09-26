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
func (refusingEngine) RespondQuestion(context.Context, command.RespondQuestion) error {
	return errors.New("question q1 is not waiting for an answer")
}
func (refusingEngine) RespondPermission(context.Context, command.RespondPermission) error {
	return errors.New("permission p1 is not waiting for a decision")
}
func (refusingEngine) Interrupt(context.Context, command.Interrupt) error {
	return errors.New("the daemon is gone")
}

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

// An answer or a decision the engine would not take — another screen got there first, or the
// prompt expired — is said, not reported as success.
func TestARefusedAnswerOrDecisionIsSaid(t *testing.T) {
	m := newTestModel(t)
	m.app = refusingEngine{m.app}

	m.quest = &questReq{callID: "q1", question: "which?", options: []string{"a", "b"}}
	var got []string
	for _, msg := range runCmd(m.answerQuestion("a")) {
		if n, ok := msg.(noticeMsg); ok {
			got = append(got, string(n))
		}
	}
	m.perm = &permReq{callID: "p1"}
	for _, msg := range runCmd(m.respond("allow")) {
		if n, ok := msg.(noticeMsg); ok {
			got = append(got, string(n))
		}
	}
	if len(got) != 2 || !strings.Contains(got[0], "answer not delivered") || !strings.Contains(got[1], "decision not delivered") {
		t.Fatalf("both refusals should come back as notices, got %q", got)
	}
	next, _ := m.Update(noticeMsg(got[0]))
	if m = next.(Model); !strings.Contains(m.snackbar, "not waiting for an answer") {
		t.Errorf("the notice should reach the snackbar, got %q", m.snackbar)
	}
}

// Esc on a running turn that the engine could not interrupt says so: the turn is still going.
func TestAnInterruptThatFailedIsSaid(t *testing.T) {
	m := newTestModel(t)
	m.app = refusingEngine{m.app}
	m.running = true
	m.focusPane = -1
	if _, handled := m.handleKey(tea.KeyPressMsg{Code: tea.KeyEscape}); !handled {
		t.Fatal("esc on a running turn should be handled")
	}
	if !strings.Contains(m.snackbar, "could not interrupt") || !strings.Contains(m.snackbar, "the daemon is gone") {
		t.Errorf("a failed interrupt must be on screen, snackbar = %q", m.snackbar)
	}
}
