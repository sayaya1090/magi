package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Sweep two: the full-screen views and the panes. These replace the transcript entirely, so a
// defect here is not a cosmetic slip — the user is looking at that screen and nothing else. All of
// them were at 0%, which means every one shipped without ever having been rendered in a test.

// A subagent starts, works, and finishes. Its pane is a whole second transcript living beside the
// main one, with its own status, and the composition of the two is exactly the shape that hid the
// ordering bugs in the main view.
func TestASubagentPaneLivesBesideTheTranscript(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "review the repo")
	child := session.SessionID("s_child")
	s.emit(event.TypeSessionCreated, event.SessionCreatedData{
		Workdir: "/app", Agent: "explorer", Parent: string(s.m.sid),
		Model: session.ModelRef{Provider: "openai", Model: "m"},
	})
	// The child's own events arrive tagged with the child's sid.
	s.send(eventMsg{sid: child, sub: s.m.mainSub, ev: event.Event{
		Seq: 100, Type: event.TypeToolProgress, Actor: event.Actor{Kind: event.ActorAgent, ID: "explorer"},
		Data: mustJSON(event.ToolProgressData{Text: "reading the tree"}),
	}})
	s.renders("the main transcript with a child running", "review the repo")
}

// The route editor replaces the screen. It lists the session model and per-agent routing, and it
// has to render on a session that has never been routed — the common case, and the one where a nil
// map or an empty list would panic or blank the frame.
func TestTheRouteEditorRendersOnAnUnroutedSession(t *testing.T) {
	s := newScript(t)
	s.typeText("/route").enter()
	s.renders("the route editor")
	// esc returns to the transcript rather than stranding the user in a view with no exit.
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.renders("back from the route editor")
}

// The resume list replaces the screen too, and a fresh install has no sessions to resume — the
// emptiest possible input to a view built to show rows.
func TestTheResumeListRendersWithNothingToResume(t *testing.T) {
	s := newScript(t)
	s.typeText("/resume").enter()
	s.renders("the resume list")
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	s.renders("back from the resume list")
}

// The command palette opens on a keystroke and filters as you type. An unmatched filter is the
// interesting input: it must render an empty list, not a blank screen or an index out of range.
func TestThePaletteFiltersAndSurvivesNoMatches(t *testing.T) {
	// It shows while a slash command is being TYPED — pressing enter submits and clears it, and it
	// is suppressed while a turn runs. A test that types the whole command and hits enter asserts
	// against a screen the palette was never on; this one leaves the input mid-command.
	s := newScript(t)
	s.typeText("/co")
	if len(s.m.paletteMatches()) == 0 {
		t.Fatal("/co must match at least one command, or this asserts nothing")
	}
	s.renders("the palette", "/co")

	s2 := newScript(t)
	s2.typeText("/zzzzzznotacommand")
	if len(s2.m.paletteMatches()) != 0 {
		t.Fatal("this filter is supposed to match nothing")
	}
	s2.renders("a palette filter that matches nothing")
}

// A diff is rendered with colour per line. It is reached from /diff and from a tool result, and the
// colouring walks the text — an empty diff and a diff with no trailing newline are the two shapes
// that trip a line walker.
func TestDiffColouringHandlesTheAwkwardShapes(t *testing.T) {
	for _, d := range []string{
		"",
		"diff --git a/x b/x\n+added\n-removed\n context",
		"+no trailing newline",
	} {
		if got := colorizeDiff(d); strings.Count(got, "\n") != strings.Count(d, "\n") {
			t.Errorf("colouring changed the line count of %q", d)
		}
	}
}

// The plan panel nests steps and has to wrap them into its column. A step longer than the panel is
// the case that either wraps, truncates, or overflows into the transcript beside it.
func TestThePlanPanelWrapsALongStepIntoItsColumn(t *testing.T) {
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	s.steer("r1", "do the thing")
	// hasPanel() asks the APP for the plan, not the view — an event alone leaves the panel hidden
	// and the assertions below would pass against a screen that never had one.
	s.m.app.SetTodos(s.m.sid, []session.Todo{
		{Content: strings.Repeat("a very long step description that will not fit ", 4), Status: "in_progress"},
		{Content: "short", Status: "pending"},
	})
	if !s.m.hasPanel() {
		t.Fatal("the panel is not showing, so this test would assert nothing")
	}
	s.emit(event.TypeContextUsage, event.ContextUsageData{Tokens: 1000, Window: 65536, Percent: 1.5})
	s.renders("a panel with an over-long step")
}

// Compaction is a milestone the user should see, with the sizes it actually moved — it is the one
// event that silently deletes conversation, so saying nothing about it is the worst option.
func TestACompactionIsVisibleWithItsNumbers(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "a long task")
	s.emit(event.TypeCompaction, event.CompactionData{
		Summary: "earlier work summarized", ReplacesUpToSeq: 40, TokensBefore: 52428, TokensAfter: 18000,
	})
	s.renders("a compaction milestone")
}

// A tool result that is a wall of bytes must not become a wall of bytes on screen. textBody is what
// decides how much of it is shown, and binary content is the shape it exists for.
func TestABinaryToolResultDoesNotFloodTheFrame(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "read the binary")
	s.toolCall("read", "c1")
	s.toolResult("c1", string([]byte{0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00, 0x00, 0x00})+strings.Repeat("\x00", 4000))
	out := s.view()
	if len(strings.Split(out, "\n")) > s.m.height+2 {
		t.Errorf("the frame grew past the terminal height: %d rows", len(strings.Split(out, "\n")))
	}
	s.renders("a binary result")
}

// Switching to another session swaps the whole transcript. The subscription generation is what
// stops the old session's in-flight events from painting into the new one — a stale event landing
// after the switch would show the previous conversation's text under the new session's header.
func TestEventsFromTheOldSessionDoNotPaintIntoTheNewOne(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "first session work")
	stale := s.m.mainSub
	s.m.mainSub++ // the switch bumps the generation
	before := len(s.m.blocks)
	s.send(eventMsg{sid: s.m.sid, sub: stale, ev: event.Event{
		Seq: 500, Type: event.TypePartAppended, Actor: event.Actor{Kind: event.ActorAgent, ID: "default"},
		Data: mustJSON(event.PartAppendedData{MessageID: "m_old", Role: session.RoleAssistant,
			Part: session.Part{Kind: session.PartText, Text: "text from the session we left"}}),
	}})
	if len(s.m.blocks) != before {
		t.Errorf("an event from a switched-away subscription painted into the current view: %+v", s.m.blocks[before:])
	}
}

// The wheel still scrolls the transcript while a permission prompt is up — that is deliberate, so
// the user can page back and read the context of the decision they are being asked to make. It is
// also the only path that reaches wheelScrollTranscript, so without a modal in the frame the
// behaviour is untested however much scrolling a test does.
func TestTheTranscriptScrollsBehindAPermissionPrompt(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "review then delete")
	for i := 0; i < 40; i++ {
		s.assistantText("a line of context worth re-reading")
	}
	s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
		CallID: "c1", Name: "bash", Args: []byte(`{"command":"rm -rf build"}`), Reason: "destructive"})
	_ = s.view()

	at := s.m.vp.YOffset()
	if at == 0 {
		t.Fatal("the transcript is not scrollable, so this proves nothing")
	}
	for i := 0; i < 5; i++ {
		s.send(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	}
	if up := s.m.vp.YOffset(); up >= at {
		t.Errorf("the wheel did not scroll behind the modal: %d → %d", at, up)
	}
	// …and the prompt is still up: scrolling must not be mistaken for answering.
	if s.m.perm == nil {
		t.Error("scrolling dismissed the permission prompt")
	}
	s.renders("the prompt after scrolling behind it", "bash")
}

// A compaction does NOT shed the transcript — it adds one line saying it happened.
//
// ⚠ **The comment on fuzzSteps used to assert the opposite, and everything it concluded rested on
// that.** It read "the walk's compaction step clears the transcript, so length does not accumulate
// blocks … do not read a long run as a load test", and from there MAGI_FUZZ_STEPS looked like a
// free dial. It is not: the transcript accumulates, and the walk's per-step check re-renders a
// tail every step and the whole history every 25th, so the cost rises with it. Measured
// 2026-09-11 on the very seed that comment names — 500 steps ends at 144 blocks, not the 11 it
// claimed, and 2000 steps takes 63.9s against 250 steps' 4.7s.
//
// Appending is the RIGHT behaviour and that is why this pins it rather than fixing it. Compaction
// sheds the model's context, not the user's scrollback: somebody scrolling up is reading the
// conversation, and deleting it under them because the model no longer needs it would be the
// screen lying about what happened. What was wrong was only a sentence about it.
//
// So the fact is held here, where a change to it fails a test instead of quietly making a
// paragraph elsewhere false again.
func TestTheTranscriptSurvivesACompaction(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "a long task")
	s.assistantText("an answer worth keeping")
	s.toolCall("read", "c1")
	s.toolResult("c1", "some bytes")
	before := append([]block(nil), s.m.blocks...)
	if len(before) < 3 {
		t.Fatalf("the fixture must build a transcript to survive; it has %d blocks", len(before))
	}

	s.emit(event.TypeCompaction, event.CompactionData{
		Summary: "earlier work summarized", ReplacesUpToSeq: 40, TokensBefore: 52428, TokensAfter: 18000,
	})

	if got, want := len(s.m.blocks), len(before)+1; got != want {
		t.Fatalf("a compaction left %d blocks, want %d — it must add its milestone and take nothing",
			got, want)
	}
	for i, b := range before {
		if s.m.blocks[i].kind != b.kind || s.m.blocks[i].text != b.text {
			t.Errorf("block %d changed under the compaction: %v %q → %v %q",
				i, b.kind, b.text, s.m.blocks[i].kind, s.m.blocks[i].text)
		}
	}
	// And the one it added says what moved, which is the other half of why it is a block at all.
	last := s.m.blocks[len(s.m.blocks)-1]
	if last.kind != blockInfo || !strings.Contains(last.text, "compacted") {
		t.Errorf("the milestone is %v %q", last.kind, last.text)
	}
}
