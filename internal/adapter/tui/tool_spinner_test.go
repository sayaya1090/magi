package tui

import (
	"strings"
	"testing"
)

// A tool call that is running says so by moving.
//
// Its glyph was a still ⚙ whether the call had been going for a second or for four minutes, which
// is exactly the case somebody watches the screen for — the request bubble above it was already
// spinning, and the row that was actually taking the time was not.
func TestARunningToolCallTurnsWhileItRuns(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.running, m.turnReqID = true, "r1"
	m.blocks = []block{
		{kind: blockUser, text: "go and do it", reqID: "r1"},
		{kind: blockToolCall, name: "bash", args: `{"command":"go test ./..."}`, callID: "c1"},
	}

	out := m.transcript()
	if strings.Contains(out, "⚙") {
		t.Errorf("a call that is running still shows the resting glyph:\n%s", out)
	}
	// Whatever frame it is on, it is one of the spinner's — asserted against the spinner itself
	// rather than a hard-coded glyph, so this does not break when the style changes.
	if frame := strings.TrimRight(m.sp.View(), " "); frame != "" && !strings.Contains(out, frame) {
		t.Errorf("the running call does not carry a spinner frame (%q):\n%s", frame, out)
	}

	// Once the result lands it settles: an outcome is a fact and facts do not animate.
	m.blocks[1].done, m.blocks[1].ok, m.blocks[1].result = true, true, "ok"
	m.cache = m.cache[:0]
	if out := m.transcript(); !strings.Contains(out, "✓") {
		t.Errorf("a finished call does not show its outcome:\n%s", out)
	}
}

// A call left unfinished by an EARLIER turn keeps the still glyph.
//
// It is not running — nothing is going to happen to it — and it is served from the cache, so a
// spinner there would not turn: it would freeze on whichever frame the cache happened to catch,
// which reads as a call in progress that never progresses.
func TestACallAbandonedByAnEarlierTurnDoesNotSpin(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.running, m.turnReqID = true, "r2"
	m.blocks = []block{
		{kind: blockUser, text: "the first thing", reqID: "r1"},
		{kind: blockToolCall, name: "bash", args: `{"command":"sleep 600"}`, callID: "c0"},
		{kind: blockUser, text: "never mind, this instead", reqID: "r2"},
		{kind: blockToolCall, name: "read", args: `{"path":"go.mod"}`, callID: "c1"},
	}
	out := m.transcript()
	if !strings.Contains(out, "⚙") {
		t.Errorf("the abandoned call lost its still glyph:\n%s", out)
	}
	// And the live one still moves, so this is a distinction and not a blanket.
	if frame := strings.TrimRight(m.sp.View(), " "); frame != "" && !strings.Contains(out, frame) {
		t.Errorf("the call of the running turn does not spin:\n%s", out)
	}
}

// Nothing spins when nothing is running.
func TestAnIdleTranscriptHasNoSpinningTool(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 120, 40
	m.blocks = []block{
		{kind: blockUser, text: "do it", reqID: "r1"},
		{kind: blockToolCall, name: "bash", args: `{"command":"true"}`, callID: "c1"},
	}
	if frame := strings.TrimRight(m.sp.View(), " "); frame != "" && strings.Contains(m.transcript(), frame) {
		t.Error("a transcript with no turn running is animating something")
	}
}
