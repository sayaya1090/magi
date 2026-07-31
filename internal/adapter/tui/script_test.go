package tui

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A scripted session: the missing test layer.
//
// The package has 224 tests and 68% statement coverage, and two composition bugs still shipped in
// one day — an in-flight answer rendering below a question that had not been asked yet, and an
// answered question glued to the bottom of the transcript. Neither is visible from a unit test,
// because neither lives in a unit: they live in the ORDER of steer → event fold → transcript
// assembly. Every piece had a test; nothing ran the pieces in sequence.
//
// What was uncovered says the same thing. 46 functions sat at 0%, and they are the entry points and
// whole views — submit/submitAs/sendPrompt, Init/startSub/waitEvent, routeView/resumeView/
// paletteView, the job panes. Six test files touched Update() at all, most of them once.
//
// So this drives the real Update() with the real message types, in the order a session produces
// them, and asserts on STRUCTURE — order, presence, the block↔line map. Not bytes: the output
// carries theme colour and wraps to width, so byte equality would pin the palette instead of the
// behaviour and would break on every restyle.
type script struct {
	t   *testing.T
	m   Model
	seq int64
}

func newScript(t *testing.T) *script {
	t.Helper()
	s := &script{t: t, m: newTestModel(t)}
	s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	return s
}

// send pushes one message through Update, exactly as the runtime would.
func (s *script) send(msg tea.Msg) *script {
	s.t.Helper()
	next, _ := s.m.Update(msg)
	if mm, ok := next.(Model); ok {
		s.m = mm
	}
	return s
}

// emit delivers a domain event on the primary subscription. The sid/sub tags matter: Update drops
// events for a session it is not showing and for a switched-away subscription, so a harness that
// got them wrong would silently assert on an empty transcript.
func (s *script) emit(t event.Type, data any) *script {
	s.t.Helper()
	s.seq++
	b, err := json.Marshal(data)
	if err != nil {
		s.t.Fatalf("marshal %s: %v", t, err)
	}
	return s.send(eventMsg{
		ev:  event.Event{Seq: s.seq, Type: t, Data: b, Actor: event.Actor{Kind: event.ActorAgent, ID: "default"}},
		sid: s.m.sid, sub: s.m.mainSub,
	})
}

// emitAs is emit with a specific actor — the difference between a user prompt, magi's own nudge,
// and a subagent result is exactly what several folds branch on.
func (s *script) emitAs(t event.Type, actor event.Actor, data any) *script {
	s.t.Helper()
	s.seq++
	b, _ := json.Marshal(data)
	return s.send(eventMsg{
		ev:  event.Event{Seq: s.seq, Type: t, Data: b, Actor: actor},
		sid: s.m.sid, sub: s.m.mainSub,
	})
}

func (s *script) typeText(text string) *script {
	s.t.Helper()
	for _, r := range text {
		s.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return s
}

func (s *script) enter() *script { return s.send(tea.KeyPressMsg{Code: tea.KeyEnter}) }

// ansiSeq matches the SGR escapes the renderer emits. Styling is applied per RUN, sometimes per
// CHARACTER, so the visible text is not contiguous in the raw string: searching it for "Reading the
// file." finds nothing even when the line is right there on screen. Every structural assertion
// therefore reads the STRIPPED frame; rawView keeps the escapes for the few checks that are about
// styling or cell width.
var ansiSeq = regexp.MustCompile("\x1b\\[[0-9;]*[a-zA-Z]")

// view renders the frame the user would see, with the styling removed.
func (s *script) view() string { return ansiSeq.ReplaceAllString(s.rawView(), "") }

// rawView is the frame exactly as written to the terminal, escapes included.
func (s *script) rawView() string {
	s.t.Helper()
	s.send(renderTickMsg{})
	return s.m.View().Content
}

// order asserts the rendered frame contains each marker, in this order. The failure prints the
// frame, because "which line came first" is unreadable from a boolean.
func (s *script) order(what string, markers ...string) {
	s.t.Helper()
	out := s.view()
	prev, prevMark := -1, ""
	for _, mk := range markers {
		at := strings.Index(out, mk)
		if at < 0 {
			s.t.Fatalf("%s: %q never rendered:\n%s", what, mk, out)
		}
		if at < prev {
			s.t.Errorf("%s: %q rendered before %q:\n%s", what, mk, prevMark, out)
		}
		prev, prevMark = at, mk
	}
}

// assistant text/tool-call/tool-result helpers: the three parts every real step produces.
func (s *script) assistantText(text string) *script {
	return s.emit(event.TypePartAppended, event.PartAppendedData{
		MessageID: "m_a", Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartText, Text: text},
	})
}

func (s *script) toolCall(name, id string) *script {
	return s.emit(event.TypePartAppended, event.PartAppendedData{
		MessageID: "m_a", Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
			CallID: id, Name: name, Args: json.RawMessage(`{"command":"ls"}`)}},
	})
}

func (s *script) toolResult(id, out string) *script {
	return s.emit(event.TypePartAppended, event.PartAppendedData{
		MessageID: "m_a", Role: session.RoleAssistant,
		Part: session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{
			CallID: id, Content: mustJSON(out)}},
	})
}

// mustJSON is the tool-result content shape: the log stores it as raw JSON, not as a bare string.
func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// The ordinary shape of a turn, driven end to end. It asserts nothing clever — it is the baseline
// that has to keep working while the interesting scenarios below bend it.
func TestAWholeTurnRendersInOrder(t *testing.T) {
	s := newScript(t)
	s.typeText("count the rows").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "count the rows"}}})
	s.assistantText("Reading the file.")
	s.toolCall("bash", "c1")
	s.toolResult("c1", "305")
	s.assistantText("There are 305 rows.")
	s.emit(event.TypeTurnFinished, event.TurnFinishedData{})

	s.order("a plain turn", "count the rows", "Reading the file.", "There are 305 rows.")
}

// The first of the two bugs, as a session rather than a unit: the user types while the answer is
// still streaming. The streaming text belongs to the FIRST question, so it must not render under
// the second one.
func TestTypingWhileAnAnswerStreams(t *testing.T) {
	s := newScript(t)
	s.typeText("count the rows").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "count the rows"}}})
	// mid-stream: deltas are arriving…
	s.emit(event.TypePartDelta, event.PartDeltaData{MessageID: "m_a", PartID: "p1", Kind: session.PartText, Text: "readingnow"})
	// …and the user steers.
	s.typeText("headercheck").enter()

	s.order("a steer during a stream", "count the rows", "readingnow", "headercheck")
}

// The second bug: once the model says it answered the steer, the question stops waiting, moves
// above the answer, and becomes an ordinary block that scrolls with the rest.
func TestASteerThatGetsAnsweredStopsWaiting(t *testing.T) {
	s := newScript(t)
	s.typeText("count the rows").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "count the rows"}}})
	s.typeText("headercheck").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r2", Parts: []session.Part{{Kind: session.PartText, Text: "headercheck"}}})
	s.assistantText("the header is on line one")
	s.emitAs(event.TypeInterjectionAnswered, event.Actor{Kind: event.ActorSystem, ID: "interject"},
		event.InterjectionAnsweredData{MessageID: "r2"})

	s.order("an answered steer", "headercheck", "the header is on line one")
	for _, b := range s.m.blocks {
		if b.kind == blockUser && b.queued {
			t.Errorf("nothing is waiting any more, yet a bubble still says it is: %q", b.text)
		}
	}
}

// magi's own injected prompts are not the user speaking. They render as an info line, and they must
// never be mistaken for a request bubble — a nudge that looked like a user turn would put words in
// the user's mouth in their own transcript.
func TestMagisOwnPromptsAreNotUserTurns(t *testing.T) {
	s := newScript(t)
	s.typeText("count the rows").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "count the rows"}}})
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"},
		event.PromptSubmittedData{MessageID: "n1", Parts: []session.Part{{Kind: session.PartText, Text: "STOP reasoning now and act."}}})

	users := 0
	for _, b := range s.m.blocks {
		if b.kind == blockUser {
			users++
		}
	}
	if users != 1 {
		t.Errorf("one user spoke, %d user bubbles rendered: %+v", users, s.m.blocks)
	}
	if !strings.Contains(s.view(), "loop") {
		t.Errorf("the injected note must still be visible, tagged with who injected it:\n%s", s.view())
	}
}

// Every block must be findable by the line it starts on — that map is what turns a click into a
// block, and it is only correct while it ascends. It broke silently when queued bubbles began
// rendering after the live section, and nothing but a rendered frame can catch that.
func TestTheBlockLineMapAscendsThroughAWholeTurn(t *testing.T) {
	s := newScript(t)
	s.typeText("count the rows").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "count the rows"}}})
	s.assistantText("working")
	s.toolCall("bash", "c1")
	s.toolResult("c1", "305")
	s.typeText("one more thing").enter()
	s.emit(event.TypePartDelta, event.PartDeltaData{MessageID: "m_a", PartID: "p1", Kind: session.PartText, Text: "still going"})
	_ = s.view()

	if len(s.m.blockLineStart) != len(s.m.blocks) {
		t.Fatalf("every block needs a start line: %d starts for %d blocks", len(s.m.blockLineStart), len(s.m.blocks))
	}
	for i := 1; i < len(s.m.blockLineStart); i++ {
		if s.m.blockLineStart[i] < s.m.blockLineStart[i-1] {
			t.Fatalf("block %d starts at line %d, before block %d at %d — clicks map to the wrong block",
				i, s.m.blockLineStart[i], i-1, s.m.blockLineStart[i-1])
		}
	}
}

// A resize re-wraps every cached block. The cache is keyed by width, and a stale entry would render
// the previous width's wrapping into the new frame — visible as ragged or clipped text.
func TestAResizeDoesNotServeStaleWrapping(t *testing.T) {
	s := newScript(t)
	s.typeText("count the rows").enter()
	s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
		event.PromptSubmittedData{MessageID: "r1", Parts: []session.Part{{Kind: session.PartText, Text: "count the rows"}}})
	s.assistantText(strings.Repeat("a long sentence that must re-wrap when the terminal changes. ", 4))
	wide := s.rawView()
	s.send(tea.WindowSizeMsg{Width: 50, Height: 40})
	narrow := s.rawView()

	if wide == narrow {
		t.Error("the frame is identical at 100 and 50 columns — the width-keyed cache did not invalidate")
	}
	// Measured the way a terminal measures it: lipgloss.Width ignores the escape sequences, which
	// are bytes in the string but occupy no cells. Counting runes would flag every styled line.
	for _, line := range strings.Split(narrow, "\n") {
		if w := lipgloss.Width(line); w > 50 {
			t.Errorf("a line is %d cells wide in a 50-column terminal:\n%q", w, line)
			break
		}
	}
}
