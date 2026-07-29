package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A turn note is the one thing in the finish seam that magi did not author. It goes back word for
// word: magi does not read it, rank it, or decide when it is relevant — the agent said it mattered,
// and that is the whole of what magi knows about it.
func TestTurnNotesComeBackVerbatimAtTheFinish(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Round: 1, Decision: council.Continue, Feedback: "not yet",
			Verdicts: []council.Verdict{{Member: "Melchior", Lens: "correctness", Decision: council.Continue}}},
		{Round: 1, Decision: council.Done,
			Verdicts: []council.Verdict{{Member: "Melchior", Lens: "correctness", Decision: council.Done}}},
	}}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	ctx := context.Background()
	s := a.sessionInfo(ctx, sid)

	const note = "field 1 maps to column 3; do not re-derive this"
	a.noteForTurn(sid, note)

	// A rejected declaration carries them: the agent keeps working and needs them most here.
	out, err := a.councilAdvice(ctx, s, nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, note) || !strings.Contains(out, "WHAT YOU ASKED TO BE REMINDED OF") {
		t.Errorf("a rejected declaration must hand the notes back:\n%s", out)
	}
	// …and so does an accepted one, before the agent writes its final answer.
	out, err = a.councilAdvice(ctx, s, nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, note) {
		t.Errorf("an accepted declaration must hand the notes back too:\n%s", out)
	}
	// The other finish seam: the agent went quiet and is asked to declare.
	tc := turnCtx{s: s, agent: AgentSpec{Name: "coder"}, guard: newRunGuard()}
	a.cfg.Workflow = false
	if _, done := a.requireFinishDeclaration(ctx, tc, true, &turnState{}); !done {
		t.Fatal("a working turn that never declared must be asked")
	}
	if txt := sessionText(t, a, sid); !strings.Contains(txt, note) {
		t.Errorf("the declaration reminder must carry the notes:\n%s", txt)
	}
}

// Nothing noted is silence. A heading with no items under it would be magi speaking for an agent
// that said nothing.
func TestNoNotesNoBlock(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	if b := a.turnNotesBlock(sid); b != "" {
		t.Errorf("no notes must render nothing, got %q", b)
	}
	a.noteForTurn(sid, "   ") // blank is not a note
	a.noteForTurn(sid, "")
	if b := a.turnNotesBlock(sid); b != "" {
		t.Errorf("blank text is not a note, got %q", b)
	}
	if notesTail("") != "" {
		t.Error("an empty block appends nothing to a finish message")
	}
}

// The same note twice is one note, and a spinning agent cannot fill the finish seam with its own
// text: the cap holds. Order is preserved — the agent wrote them in the order it learned them.
func TestTurnNotesDedupeAndBound(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	a.noteForTurn(sid, "first")
	a.noteForTurn(sid, "first")
	a.noteForTurn(sid, "second")
	b := a.turnNotesBlock(sid)
	if strings.Count(b, "first") != 1 {
		t.Errorf("a repeat is not a second note:\n%s", b)
	}
	if i, j := strings.Index(b, "first"), strings.Index(b, "second"); i > j {
		t.Errorf("notes keep the order they were written:\n%s", b)
	}
	for i := 0; i < turnNotesCap+10; i++ {
		a.noteForTurn(sid, string(rune('a'+i%26))+strings.Repeat("x", i))
	}
	a.mu.Lock()
	n := len(a.stateLocked(sid).turnNotes)
	a.mu.Unlock()
	if n > turnNotesCap {
		t.Errorf("the cap must hold, got %d notes", n)
	}
}

// Notes belong to the work that raised them. A new top-level request is new work, and last turn's
// reminders would be answering a question nobody asked.
func TestTurnNotesDoNotCrossTurns(t *testing.T) {
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow"})
	a.noteForTurn(sid, "from the previous task")
	a.resetForNewTopLevel(sid)
	if b := a.turnNotesBlock(sid); b != "" {
		t.Errorf("a new turn starts with no reminders, got %q", b)
	}
	// Todos are turn-scoped the same way; this is the sibling behaviour, not a new rule.
	if td := a.Todos(sid); len(td) != 0 {
		t.Errorf("the plan resets with the turn too, got %v", td)
	}
	_ = session.Todo{}
}
