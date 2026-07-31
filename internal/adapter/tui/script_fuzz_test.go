package tui

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Sweep seven: sequences nobody wrote down.
//
// The scripted scenarios cover the orders someone thought of, and both bugs found on day one were
// orders nobody had. So this walks a long random sequence of the things a session really does —
// prompts, steers, deltas, tool calls and results, council rounds, resizes, scrolls, modals,
// errors, finishes — and after every single step checks the invariants the whole view rests on.
//
// Seeded, so a failure is reproducible: the seed and the step are printed with it.
func TestRandomSessionsKeepTheViewCoherent(t *testing.T) {
	for _, seed := range []int64{1, 2, 3, 7, 11, 42} {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			s := newScript(t)
			ids, calls := 0, 0

			steps := []struct {
				what string
				do   func()
			}{
				{"user prompt", func() {
					ids++
					id := fmt.Sprintf("r%d", ids)
					s.typeText(fmt.Sprintf("request %d", ids)).enter()
					s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tui"},
						event.PromptSubmittedData{MessageID: id, Parts: []session.Part{{Kind: session.PartText, Text: fmt.Sprintf("request %d", ids)}}})
				}},
				{"system note", func() {
					s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"},
						event.PromptSubmittedData{MessageID: "n", Parts: []session.Part{{Kind: session.PartText, Text: "act now"}}})
				}},
				{"delta", func() {
					s.emit(event.TypePartDelta, event.PartDeltaData{MessageID: "m", PartID: "p", Kind: session.PartText, Text: "tok "})
				}},
				{"reasoning delta", func() {
					s.emit(event.TypePartDelta, event.PartDeltaData{MessageID: "m", PartID: "pr", Kind: session.PartReasoning, Text: "think "})
				}},
				{"assistant text", func() { s.assistantText("a settled answer") }},
				{"tool call", func() { calls++; s.toolCall("bash", fmt.Sprintf("c%d", calls)) }},
				{"tool result", func() {
					if calls > 0 {
						s.toolResult(fmt.Sprintf("c%d", rng.Intn(calls)+1), "some output")
					}
				}},
				{"orphan result", func() { s.toolResult("", "output with no call") }},
				{"council round", func() {
					s.emit(event.TypeCouncilConvened, event.CouncilConvenedData{
						Round: 1, Members: []string{"Melchior"}, Rule: "majority", Task: "t", Actions: "- bash → ok"})
					s.emit(event.TypeCouncilVerdict, event.CouncilVerdictData{Round: 1, Member: "Melchior", Decision: "done"})
					s.emit(event.TypeCouncilDecided, event.CouncilDecidedData{Round: 1, Decision: "done"})
				}},
				{"todos", func() {
					s.emit(event.TypeTodosChanged, event.TodosChangedData{Todos: []session.Todo{
						{Content: "a step", Status: "in_progress"}}})
				}},
				{"context usage", func() {
					s.emit(event.TypeContextUsage, event.ContextUsageData{Tokens: rng.Intn(60000), Window: 65536})
				}},
				{"permission", func() {
					s.emit(event.TypePermissionRequested, event.PermissionRequestedData{
						CallID: "cx", Name: "bash", Args: []byte(`{"command":"x"}`), Reason: "why"})
				}},
				{"answer permission", func() { s.send(tea.KeyPressMsg{Code: tea.KeyEscape}) }},
				{"resize", func() {
					s.send(tea.WindowSizeMsg{Width: 30 + rng.Intn(90), Height: 12 + rng.Intn(30)})
				}},
				{"tiny terminal", func() {
					// A split pane, a phone-sized ssh window, a terminal being dragged smaller: the
					// header alone is two rows and the input box is three, so a very short screen
					// leaves the transcript a negative number of rows unless something clamps it.
					// Eight rows is the floor this layout honours with a modal open: header(2) +
					// input box(3) + a modal shrunk to its irreducible prompt. Below that the
					// frame would have to hide something the user needs, and the fuzz does not
					// pretend otherwise rather than asserting a bar nothing can clear.
					s.send(tea.WindowSizeMsg{Width: 20 + rng.Intn(25), Height: 8 + rng.Intn(8)})
				}},
				{"question modal", func() {
					s.emit(event.TypeQuestionRequested, event.QuestionRequestedData{
						CallID: "q1", Question: "which one?", Options: []string{"a", "b"}})
				}},
				{"diagnostic", func() {
					s.emit(event.TypeDiagnostic, event.DiagnosticData{Source: "council", Detail: "unparsed reply"})
				}},
				{"scroll", func() {
					if rng.Intn(2) == 0 {
						s.send(tea.MouseWheelMsg{Button: tea.MouseWheelUp})
					} else {
						s.send(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
					}
				}},
				{"interject answered", func() {
					s.emitAs(event.TypeInterjectionAnswered, event.Actor{Kind: event.ActorSystem, ID: "interject"},
						event.InterjectionAnsweredData{MessageID: fmt.Sprintf("r%d", 1+rng.Intn(max(ids, 1)))})
				}},
				{"compaction", func() {
					s.emit(event.TypeCompaction, event.CompactionData{Summary: "s", TokensBefore: 100, TokensAfter: 10})
				}},
				{"error", func() { s.emit(event.TypeError, event.ErrorData{Message: "the provider refused"}) }},
				{"finish", func() { s.emit(event.TypeTurnFinished, event.TurnFinishedData{}) }},
			}

			for step := 0; step < 200; step++ {
				pick := steps[rng.Intn(len(steps))]
				pick.do()
				raw := s.rawView()
				where := fmt.Sprintf("seed %d, step %d (%s)", seed, step, pick.what)

				// One recorded start line per block, ascending — this is what turns a click into
				// a block, and it is the invariant the queued-tail rendering nearly broke.
				if len(s.m.blockLineStart) != len(s.m.blocks) {
					t.Fatalf("%s: %d start lines for %d blocks", where, len(s.m.blockLineStart), len(s.m.blocks))
				}
				for i := 1; i < len(s.m.blockLineStart); i++ {
					if s.m.blockLineStart[i] < s.m.blockLineStart[i-1] {
						t.Fatalf("%s: block %d starts at %d, before block %d at %d",
							where, i, s.m.blockLineStart[i], i-1, s.m.blockLineStart[i-1])
					}
				}
				// Nothing may be wider than the terminal it is drawn in. Report the WIDEST row, not
				// the first over-wide one: the frame is joined vertically, so one long row pads
				// every other to match and the first offender is almost never the culprit.
				// Measured on the row's own CONTENT: the vertical join pads every row to the widest
				// one, so raw widths are all equal and the first row always looks like the culprit.
				// Trailing blanks are padding; what is left is what the row actually drew.
				lines := strings.Split(raw, "\n")
				widest, at := 0, -1
				for i, line := range lines {
					trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
					if w := lipgloss.Width(trimmed); w > widest {
						widest, at = w, i
					}
				}
				if widest > s.m.width {
					t.Fatalf("%s: line %d draws %d cells in a %d-column terminal:\n%q",
						where, at, widest, s.m.width, lines[at])
				}
				// A session with content never renders an empty screen.
				if len(s.m.blocks) > 0 && strings.TrimSpace(ansiSeq.ReplaceAllString(raw, "")) == "" {
					t.Fatalf("%s: %d blocks and a blank frame", where, len(s.m.blocks))
				}
				// The frame must fit the terminal vertically too. One row too many scrolls the
				// screen, which on an alt-screen UI means the top of the frame is simply gone.
				if rows := len(lines); s.m.height > 0 && rows > s.m.height {
					t.Fatalf("%s: the frame is %d rows in a %d-row terminal (chrome=%d modalRoom=%d perm=%d quest=%d vp=%d palette=%d resuming=%v):\n%s",
						where, rows, s.m.height, s.m.chromeHeight(), s.m.modalRoom(),
						lipglossHeightOrZero(s.m.perm != nil, s.m.permView), lipglossHeightOrZero(s.m.quest != nil, s.m.questView),
						s.m.vp.Height(), len(s.m.paletteMatches()), s.m.resuming,
						ansiSeq.ReplaceAllString(raw, ""))
				}
			}
		})
	}
}

// lipglossHeightOrZero measures a modal only when it is up.
func lipglossHeightOrZero(up bool, render func() string) int {
	if !up {
		return 0
	}
	return lipgloss.Height(render())
}
