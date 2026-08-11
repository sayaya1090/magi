package tui

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// A prompt answered somewhere else stops being asked here.
//
// Two UIs on one daemon is the arrangement the socket exists for — a browser on a phone and a
// terminal on the desk — and the modal was cleared only by THIS screen answering. So the other one
// sat holding a question that had already been decided, over a turn that had moved on, and the only
// way out of it was to answer something nobody was waiting for. Reported from a live pair.
func TestAPromptAnsweredElsewhereClosesHere(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40

	m.applyEvent(ev(t, event.TypePermissionRequested, event.PermissionRequestedData{
		CallID: "call_7", Name: "write", Args: []byte(`{"path":"hello.py"}`)}))
	if m.perm == nil {
		t.Fatal("the permission prompt did not come up")
	}
	// Somebody else's answer. The decision is a fact, so it reaches this viewer through the log
	// without anything new being invented.
	m.applyEvent(ev(t, event.TypePermissionDecided, event.PermissionDecidedData{
		CallID: "call_7", Decision: "allow"}))
	if m.perm != nil {
		t.Error("the permission modal is still up after the call was decided")
	}

	// A question's answer is not a fact — it goes straight to the tool that was waiting — so the
	// daemon says so on the bus and the attached view synthesises the same thing when the prompt
	// stops being reported.
	m.applyEvent(ev(t, event.TypeQuestionRequested, event.QuestionRequestedData{
		CallID: "q1", Question: "which surface?"}))
	if m.quest == nil {
		t.Fatal("the question did not come up")
	}
	m.applyEvent(ev(t, event.TypeQuestionAnswered, event.QuestionAnsweredData{CallID: "q1"}))
	if m.quest != nil {
		t.Error("the question modal is still up after it was answered")
	}
}

// A decision about a DIFFERENT call leaves this one alone.
func TestAnotherCallsDecisionDoesNotCloseThisPrompt(t *testing.T) {
	m := newTestModel(t)
	m.applyEvent(ev(t, event.TypePermissionRequested, event.PermissionRequestedData{
		CallID: "call_7", Name: "bash"}))
	m.applyEvent(ev(t, event.TypePermissionDecided, event.PermissionDecidedData{
		CallID: "call_9", Decision: "deny"}))
	if m.perm == nil {
		t.Error("a decision about another call took this prompt away")
	}
}

// The terminal draws the same distinction: done-with-something-to-read is not failed.
func TestALintedWriteDoesNotDrawAsFailed(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.blocks = []block{{kind: blockToolCall, name: "write", callID: "c1",
		args: `{"path":"hello.py","content":"print(1)"}`}}

	// What the engine produces for a file that was written and then diagnosed: an error, so the
	// agent reads it, and advisory, because the write happened.
	m.foldToolResult("c1", "wrote 22 bytes to hello.py\n\n[diagnostics]\nPython: unused import", false, true)
	out := m.transcript()
	if strings.Contains(out, "✗") {
		t.Errorf("a write that landed on disk is drawn as a failure:\n%s", out)
	}
	if !strings.Contains(out, "⚠") {
		t.Errorf("nothing says there is something to read:\n%s", out)
	}

	// A real failure still says so.
	m.blocks = []block{{kind: blockToolCall, name: "write", callID: "c2", args: `{"path":"x"}`}}
	m.cache = m.cache[:0]
	m.foldToolResult("c2", "refused: you said no", false, false)
	if out := m.transcript(); !strings.Contains(out, "✗") {
		t.Errorf("a refused call lost its mark:\n%s", out)
	}
}
