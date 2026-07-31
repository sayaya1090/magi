package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A sweep across the surfaces the unit tests never reach: whole views that were at 0% coverage,
// the modals that interrupt a turn, the panes, and scrolling. Each one drives Update() the way the
// runtime does and asserts the frame is coherent — text present, nothing wider than the terminal,
// no panic on the paths a user actually walks.

// renders is the sweep's baseline check: a frame that is non-empty, fits the terminal, and (when
// asked) contains what it is supposed to. Width is measured in CELLS — the frame is full of escape
// sequences that occupy no columns.
func (s *script) renders(what string, want ...string) {
	s.t.Helper()
	raw := s.rawView()
	if strings.TrimSpace(ansiSeq.ReplaceAllString(raw, "")) == "" {
		s.t.Fatalf("%s: the frame is blank", what)
	}
	plain := ansiSeq.ReplaceAllString(raw, "")
	for _, w := range want {
		if !strings.Contains(plain, w) {
			s.t.Errorf("%s: %q missing from the frame:\n%s", what, w, plain)
		}
	}
	for i, line := range strings.Split(raw, "\n") {
		if w := lipgloss.Width(line); w > s.m.width {
			s.t.Errorf("%s: line %d is %d cells in a %d-column terminal:\n%q", what, i, w, s.m.width, line)
			break
		}
	}
}

// A permission prompt arrives mid-turn. It is a modal over a live transcript, which is the exact
// shape that hid the two ordering bugs, and the decision has to reach the tool that is blocked on it.
func TestAPermissionPromptOverALiveTurn(t *testing.T) {
	s := newScript(t)
	s.typeText("delete the temp files").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "delete the temp files"}}})
	s.assistantText("I will remove them.")
	s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
		CallID: "c1", Name: "bash", Args: []byte(`{"command":"rm -rf /tmp/x"}`), Reason: "destructive",
	})

	s.renders("a permission modal", "bash")
	if s.m.perm == nil {
		t.Fatal("the modal must be armed")
	}
	// The transcript underneath is not lost while the modal is up.
	s.renders("the transcript behind the modal", "delete the temp files")
}

// Council verdicts stream in back-to-back and share one row. A frame painted between them must not
// cache the row half-built — that is a real cache bug the code comments describe, and only a
// rendered sequence can catch a regression of it.
func TestCouncilVerdictsShareOneRowEvenWhenAFramePaintsBetween(t *testing.T) {
	s := newScript(t)
	s.typeText("finish it").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "finish it"}}})
	s.emit(event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior", "Balthasar", "Casper"}, Rule: "majority",
		Task: "finish it", Actions: "- bash `ls` → ok", Changes: "### a.go\n+x",
	})
	s.emit(event.TypeCouncilVerdict, event.CouncilVerdictData{Round: 1, Member: "Melchior", Lens: "correctness", Decision: "done"})
	_ = s.view() // a frame lands mid-round
	s.emit(event.TypeCouncilVerdict, event.CouncilVerdictData{Round: 1, Member: "Balthasar", Lens: "verification", Decision: "done"})
	s.emit(event.TypeCouncilVerdict, event.CouncilVerdictData{Round: 1, Member: "Casper", Lens: "completeness", Decision: "continue"})

	s.renders("all three verdicts", "Melchior", "Balthasar", "Casper")
	rows := 0
	for _, b := range s.m.blocks {
		if b.kind == blockCouncilVerdict {
			rows++
			if len(b.councilVerdicts) != 3 {
				t.Errorf("one round is one row: %d verdicts on it", len(b.councilVerdicts))
			}
		}
	}
	if rows != 1 {
		t.Errorf("three verdicts of one round made %d rows", rows)
	}
}

// The evidence view behind a verdict must show every section the members saw. It gained the tool
// output only today; before that it listed the task, the plan, the claim and the diff, and silently
// omitted the one block the members' own prompt calls real evidence.
func TestTheEvidenceViewShowsWhatTheMembersSaw(t *testing.T) {
	s := newScript(t)
	s.emit(event.TypeCouncilConvened, event.CouncilConvenedData{
		Round: 1, Members: []string{"Melchior"}, Rule: "majority",
		Task:    "implement the interface",
		Report:  "done, the tests pass",
		Actions: "- bash `go test ./...` → ok: PASS",
		Changes: "### x.go\n+func F() {}",
	})
	ev := s.m.pendingCouncilEvidence
	for _, want := range []string{"implement the interface", "done, the tests pass", "go test ./...", "x.go"} {
		if !strings.Contains(ev, want) {
			t.Errorf("the evidence view dropped %q:\n%s", want, ev)
		}
	}
}

// Scrolling a transcript longer than the viewport. The wheel handler was at 0%: a user with a long
// session scrolls constantly, and an off-by-one there either strands the view or hides the newest
// line behind the input box.
func TestScrollingALongTranscript(t *testing.T) {
	s := newScript(t)
	s.typeText("read everything").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "read everything"}}})
	for i := 0; i < 40; i++ {
		s.assistantText(strings.Repeat("x", 20) + " line")
	}
	s.emit(event.TypeTurnFinished, event.TurnFinishedData{})
	_ = s.view()
	// Judged on the viewport offset, not on the frame differing: a spinner or a clock makes two
	// frames differ without anything having scrolled, which is a test that passes for the wrong
	// reason and then keeps passing after the scrolling breaks.
	bottom := s.m.vp.YOffset()
	if bottom == 0 {
		t.Fatal("the transcript is not longer than the viewport, so scrolling it proves nothing")
	}
	for i := 0; i < 10; i++ {
		s.send(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	}
	if up := s.m.vp.YOffset(); up >= bottom {
		t.Errorf("ten wheel-ups did not move the viewport: %d → %d", bottom, up)
	}
	s.renders("after scrolling up")

	for i := 0; i < 40; i++ {
		s.send(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	if back := s.m.vp.YOffset(); back != bottom {
		t.Errorf("scrolling back down did not return to the bottom: %d, want %d", back, bottom)
	}
}

// The side panel carries the plan and the context meter, both at 0%. A plan arrives as an event and
// the panel has to absorb it without overflowing the column it lives in.
func TestThePanelAbsorbsAPlanAndAContextMeter(t *testing.T) {
	s := newScript(t)
	s.emit(event.TypeTodosChanged, event.TodosChangedData{Todos: []session.Todo{
		{Content: "install the packages", Status: "completed"},
		{Content: "generate the stubs and wire them into the server implementation", Status: "in_progress"},
		{Content: "run it", Status: "pending"},
	}})
	s.emit(event.TypeContextUsage, event.ContextUsageData{Tokens: 52428, Window: 65536, Percent: 80, OutTokens: 1200})
	s.renders("the panel with a plan")
}

// Slash commands that render a whole view. Each was at 0%, and each is one keystroke away for any
// user. They must not blank the frame or panic on a session with no history.
func TestSlashCommandsRenderSomething(t *testing.T) {
	for _, cmd := range []string{"/help", "/tools", "/context", "/cost", "/sessions", "/permission", "/diff"} {
		t.Run(cmd, func(t *testing.T) {
			s := newScript(t)
			s.typeText(cmd).enter()
			s.renders(cmd + " renders")
		})
	}
}

// An error ends a turn. The frame must say so rather than leaving the spinner running forever.
func TestAnErrorEndsTheTurnVisibly(t *testing.T) {
	s := newScript(t)
	s.typeText("do it").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "do it"}}})
	s.emit(event.TypeError, event.ErrorData{Message: "the provider refused the request"})

	s.renders("an errored turn", "the provider refused the request")
	if s.m.running {
		t.Error("the turn errored — the spinner must stop")
	}
}

// steer is the user typing while a turn runs, followed by the engine's echo of that prompt.
func (s *script) steer(id, text string) *script {
	s.t.Helper()
	s.typeText(text).enter()
	return s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: id, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
}

// Several steers pile up during one long turn. Each is its own request and none may be lost,
// merged, or reordered against the others — a user who asked three things must see three.
func TestSeveralSteersPileUpWithoutLosingAny(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "count the rows")
	s.assistantText("working on it")
	s.steer("r2", "alpha")
	s.steer("r3", "bravo")
	s.steer("r4", "charlie")

	s.order("three steers in order", "alpha", "bravo", "charlie")
	seen := map[string]bool{}
	for _, b := range s.m.blocks {
		if b.kind == blockUser {
			seen[b.text] = true
		}
	}
	for _, want := range []string{"count the rows", "alpha", "bravo", "charlie"} {
		if !seen[want] {
			t.Errorf("a request the user typed is not in the transcript: %q", want)
		}
	}
}

// A sentence typed while the permission modal is up must not answer it. The user steers mid-turn
// as a matter of course, so a prompt opens between two keystrokes and the next letter used to
// decide it: driving the modal with ordinary prose, "this should not become a request" was
// answered by its own `n`, the modal closed, and the rest of the sentence became a prompt. An `a`
// would have granted the tool for the session; a `p` writes an allow rule to disk.
func TestASentenceCannotAnswerAPermissionPrompt(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "delete them")
	s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
		CallID: "c1", Name: "bash", Args: []byte(`{"command":"rm x"}`), Reason: "destructive"})
	before := len(s.m.blocks)
	s.typeText("this should not become a request. yes, no, always, project.")

	if s.m.perm == nil {
		t.Fatal("a typed sentence decided a permission the user never read")
	}
	if len(s.m.blocks) != before {
		t.Errorf("typing reached the transcript through the modal: %+v", s.m.blocks[before:])
	}
	s.renders("the modal still up", "bash")
}

// …and a deliberate answer still works: the letter focuses its button, enter presses it.
func TestALetterFocusesAndEnterAnswers(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "delete them")
	s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
		CallID: "c1", Name: "bash", Args: []byte(`{"command":"rm x"}`), Reason: "destructive"})
	s.typeText("a")
	if s.m.perm == nil {
		t.Fatal("a letter must focus, not press")
	}
	if got := s.m.perm.sel; got != permIndex("always") {
		t.Errorf("`a` must focus the always button, selection is %d", got)
	}
	s.enter()
	if s.m.perm != nil {
		t.Error("enter must press the focused button")
	}
}

// esc still denies in one key: it is not printable, so it cannot arrive inside a sentence, and
// denying is the recoverable direction.
func TestEscStillDeniesInOneKey(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "delete them")
	s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
		CallID: "c1", Name: "bash", Args: []byte(`{"command":"rm x"}`), Reason: "destructive"})
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.m.perm != nil {
		t.Error("esc must answer the prompt outright")
	}
}

// The turn errors while a steer is waiting. The error ends the turn, so nothing is coming to pick
// the steer up — it must not keep telling the user it is waiting for a turn that is over.
func TestAWaitingSteerDoesNotOutliveAnErroredTurn(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "count the rows")
	s.assistantText("working")
	s.steer("r2", "headercheck")
	s.emit(event.TypeError, event.ErrorData{Message: "the provider refused the request"})

	for _, b := range s.m.blocks {
		if b.kind == blockUser && b.queued {
			t.Errorf("the turn is over and nothing will pick this up, yet it still says waiting: %q", b.text)
		}
	}
}

// A resize while a modal is up. Both draw over the transcript, and the modal is sized from the
// terminal — a stale width leaves it wider than the screen it sits on.
func TestAResizeUnderAModalStaysInsideTheTerminal(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "delete them")
	s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
		CallID: "c1", Name: "bash", Args: []byte(`{"command":"rm -rf /very/long/path/that/keeps/going/and/going"}`), Reason: "destructive"})
	s.send(tea.WindowSizeMsg{Width: 46, Height: 20})
	s.renders("a modal after a resize")
}

// Parallel tool results complete out of order. Each must fold into ITS OWN call — the code comments
// describe a read of A once showing B's content, which is the worst kind of display bug because the
// frame looks perfectly normal.
func TestOutOfOrderToolResultsFoldIntoTheirOwnCalls(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "read both")
	s.toolCall("read", "cA")
	s.toolCall("read", "cB")
	s.toolResult("cB", "contents of B")
	s.toolResult("cA", "contents of A")

	s.renders("both results", "contents of A", "contents of B")
	for _, b := range s.m.blocks {
		if b.kind != blockToolCall {
			continue
		}
		if strings.Contains(b.text, "contents of A") && strings.Contains(b.text, "contents of B") {
			t.Errorf("one call absorbed both results:\n%s", b.text)
		}
	}
}

// A very narrow terminal. Users do split panes; the layout must degrade rather than emit lines
// wider than the screen or divide by a width it assumed was larger.
func TestANarrowTerminalDoesNotOverflow(t *testing.T) {
	for _, w := range []int{20, 30, 46} {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: w, Height: 16})
		s.steer("r1", "count the rows in the very large file")
		s.assistantText("There are three hundred and five rows in that file, give or take a header.")
		s.emit(event.TypeTodosChanged, event.TodosChangedData{Todos: []session.Todo{
			{Content: "a step with a fairly long description", Status: "in_progress"}}})
		s.renders("a narrow terminal")
	}
}

// Interrupting a running turn. esc is the only way out, and after it the UI must stop claiming to
// be working — a spinner that outlives the interrupt is how a user ends up waiting on nothing.
func TestInterruptStopsTheRunningIndicator(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "grind on this")
	s.emit(event.TypePartDelta, event.PartDeltaData{MessageID: "m_a", PartID: "p1", Kind: session.PartText, Text: "thinking"})
	if !s.m.running {
		t.Fatal("the turn should be running before the interrupt")
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.emit(event.TypeError, event.ErrorData{Message: "interrupted"})
	if s.m.running {
		t.Error("the turn was interrupted — the running indicator must stop")
	}
	s.renders("after an interrupt")
}
