package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// crashedWrite leaves a conversation the way a killed magi does: a write that had started and
// no answer to it. Recovery then closes it exactly as a restart would, so the screen reads the
// state a person would really come back to.
func crashedWrite(t *testing.T, m *Model, store port.Store) session.SessionID {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	sid, err := m.app.CreateSession(ctx, command.CreateSession{Workdir: dir})
	if err != nil {
		t.Fatal(err)
	}
	cr, _ := json.Marshal(event.SessionCreatedData{Workdir: dir})
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m1", Parts: []session.Part{{Kind: session.PartText, Text: "write the notes"}}})
	cd, _ := json.Marshal(event.PartAppendedData{Role: session.RoleAssistant, Part: session.Part{
		Kind: session.PartToolCall, ToolCall: &session.ToolCall{CallID: "c_write", Name: "write",
			Args: json.RawMessage(`{"path":"a.txt","content":"x"}`)}}})
	sd, _ := json.Marshal(event.ToolStartedData{CallID: "c_write", Tool: "write"})
	now := time.Now()
	if _, err := store.Append(ctx, sid,
		event.Event{SessionID: sid, Type: event.TypeSessionCreated, TS: now, Data: cr},
		event.Event{SessionID: sid, Type: event.TypePromptSubmitted, TS: now, Data: pd},
		event.Event{SessionID: sid, Type: event.TypePartAppended, TS: now, Data: cd},
		event.Event{SessionID: sid, Type: event.TypeToolStarted, TS: now, Data: sd},
	); err != nil {
		t.Fatal(err)
	}
	for _, r := range m.app.(*app.App).RecoverInterrupted(ctx, dir) {
		if r.Err != nil {
			t.Fatalf("recovery: %v", r.Err)
		}
	}
	return sid
}

// Coming back to a conversation a restart cut says what is waiting, keeps saying it while it
// waits, and lets the person settle each call from the keyboard. Settling one call changes the
// conversation's state in the log, not just on this screen.
func TestResumingACutConversationShowsWhatToCheckAndSettlesIt(t *testing.T) {
	mm, store := newTestModelWithStore(t)
	m := &mm
	m.width, m.height = 120, 40
	sid := crashedWrite(t, m, store)

	m.switchSession(sid)
	if m.recovery.Kind != app.RecoveryNeedsCheck {
		t.Fatalf("state after resume = %q, want needs_check", m.recovery.Kind)
	}
	if chip := ansi.Strip(m.recoveryChip()); !strings.Contains(chip, "check 1") {
		t.Fatalf("the header does not say one call needs checking: %q", chip)
	}

	m.handleSlash("/recover")
	list := m.blocks[len(m.blocks)-1].text
	for _, want := range []string{"1", "write", "a.txt", "/recover 1"} {
		if !strings.Contains(list, want) {
			t.Errorf("/recover does not name %q: %q", want, list)
		}
	}

	m.handleSlash("/recover 1 not_ran the file is absent")
	if m.recovery.Kind != app.RecoveryResumable {
		t.Fatalf("after settling the only call the state is %q, want resumable", m.recovery.Kind)
	}
	if st := m.app.RecoveryStateOf(context.Background(), sid); st.Kind != app.RecoveryResumable {
		t.Fatalf("the verdict stayed on the screen: the log says %q", st.Kind)
	}
	if chip := ansi.Strip(m.recoveryChip()); !strings.Contains(chip, "continue") {
		t.Fatalf("a resumable conversation does not offer to continue: %q", chip)
	}
}

func TestRecoverRefusesACallOrVerdictItDoesNotKnow(t *testing.T) {
	mm, store := newTestModelWithStore(t)
	m := &mm
	m.width, m.height = 120, 40
	sid := crashedWrite(t, m, store)
	m.switchSession(sid)
	before, _ := store.Read(context.Background(), sid, 0)

	for _, bad := range []string{"/recover 2 ran", "/recover 1 maybe", "/recover x ran"} {
		m.snackbar = ""
		m.handleSlash(bad)
		if m.snackbar == "" {
			t.Errorf("%q was accepted in silence", bad)
		}
	}
	after, _ := store.Read(context.Background(), sid, 0)
	if len(after) != len(before) {
		t.Fatalf("a refused verdict wrote %d events", len(after)-len(before))
	}
	if m.recovery.Kind != app.RecoveryNeedsCheck {
		t.Fatalf("a refused verdict moved the state to %q", m.recovery.Kind)
	}
}

func TestAFreshConversationHasNoRecoveryChip(t *testing.T) {
	m := newTestModel(t)
	m.switchSession(m.sid)
	if chip := m.recoveryChip(); chip != "" {
		t.Fatalf("nothing was cut and the header says %q", ansi.Strip(chip))
	}
}

func TestTheRecoveryChipIsDrawnInTheHeader(t *testing.T) {
	mm, store := newTestModelWithStore(t)
	m := &mm
	sid := crashedWrite(t, m, store)
	m.resize(140, 30)
	m.ready = true
	m.switchSession(sid)
	head, _, _ := strings.Cut(ansi.Strip(m.View().Content), "\n")
	if !strings.Contains(head, "check 1") {
		t.Fatalf("the header line does not carry the recovery chip: %q", head)
	}
}
