package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// An OSC title-spoof and a screen-clear: the two shapes the existing guards name. Neither is
// visible in the rendered text, which is the point — a leak looks like nothing at all until the
// terminal acts on it.
const (
	oscSpoof    = "\x1b]0;PWNED\x07"
	screenClear = "\x1b[2J"
)

// assertNoEscapes fails if either sequence survived into the frame, and fails just as loudly if
// the surrounding text did not: a guard that eats the message is not a guard, it is a bug.
func assertNoEscapes(t *testing.T, what, frame string) {
	t.Helper()
	if strings.Contains(frame, "]0;PWNED") {
		t.Errorf("%s: an OSC title-spoof reached the terminal", what)
	}
	if strings.Contains(frame, screenClear) {
		t.Errorf("%s: a screen-clear reached the terminal", what)
	}
	plain := ansiSeq.ReplaceAllString(frame, "")
	if !strings.Contains(plain, "before") || !strings.Contains(plain, "after") {
		t.Errorf("%s: the text around the escape was eaten:\n%s", what, plain)
	}
}

// magi strips terminal control sequences out of untrusted content so it cannot move the cursor,
// clear the screen or spoof the title. Two choke points do that — clipLine for the tool-body
// transcript, oneLine for previews and headers — and four render paths reached the frame through
// neither.
//
// Two of the four carry content magi did not author: an assistant reply is whatever the model
// wrote, and an error message can carry a server's response body verbatim. The other two are the
// user's own paste and the info lines a resumed session rebuilds from stored system messages.
func TestUntrustedTextCannotDriveTheTerminal(t *testing.T) {
	applyTheme(true)
	payload := "before " + oscSpoof + screenClear + " after"

	t.Run("assistant reply", func(t *testing.T) {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
		s.assistantText(payload)
		assertNoEscapes(t, "the model's own text", s.rawView())
	})

	t.Run("error message", func(t *testing.T) {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
		s.emit(event.TypeError, event.ErrorData{Message: payload})
		assertNoEscapes(t, "a provider error body", s.rawView())
	})

	t.Run("info line", func(t *testing.T) {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
		s.m.blocks = append(s.m.blocks, block{kind: blockInfo, text: payload})
		s.m.cache = s.m.cache[:0]
		assertNoEscapes(t, "an info line", s.rawView())
	})

	t.Run("user bubble", func(t *testing.T) {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
		s.m.blocks = append(s.m.blocks, block{kind: blockUser, text: payload})
		s.m.cache = s.m.cache[:0]
		assertNoEscapes(t, "a pasted prompt", s.rawView())
	})

	// The paths that were already guarded stay guarded — this is the regression half.
	t.Run("tool result", func(t *testing.T) {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
		s.toolCall("bash", "c1")
		s.toolResult("c1", payload)
		assertNoEscapes(t, "a tool result", s.rawView())
	})

	t.Run("system prompt", func(t *testing.T) {
		s := newScript(t)
		s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
		s.emitAs(event.TypePromptSubmitted, event.Actor{Kind: event.ActorSystem, ID: "loop"},
			event.PromptSubmittedData{MessageID: "n", Parts: []session.Part{{Kind: session.PartText, Text: payload}}})
		assertNoEscapes(t, "a system note", s.rawView())
	})
}

// The block guard keeps the newlines that make a block a block. Stripping them would collapse
// every markdown reply to one line, which is why stripControl itself cannot be used here: it
// drops every control character, and a newline is one.
func TestTheBlockGuardKeepsTheStructureOfWhatItCleans(t *testing.T) {
	got := stripControlBody("# head\n\n- one\x1b[2J\n- two\ttabbed\n")
	if strings.Contains(got, screenClear) {
		t.Errorf("the escape survived: %q", got)
	}
	for _, want := range []string{"# head", "- one", "- two\ttabbed"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q is missing from the cleaned text: %q", want, got)
		}
	}
	if n := strings.Count(got, "\n"); n != 4 {
		t.Errorf("the block has %d newlines, not the 4 it went in with: %q", n, got)
	}
	// Nothing to strip means the string comes back untouched, not rebuilt.
	const clean = "an ordinary\nreply\n"
	if got := stripControlBody(clean); got != clean {
		t.Errorf("clean text was altered: %q", got)
	}
}

// Markdown still renders as markdown: the guard runs on the INPUT, before glamour adds the
// escapes that do the styling, so its own output is not eaten by its own cleaning.
func TestTheAssistantGuardDoesNotStripMagisOwnStyling(t *testing.T) {
	applyTheme(true)
	s := newScript(t)
	s.send(tea.WindowSizeMsg{Width: 100, Height: 40})
	s.assistantText("# head\n\n- one\n- two\n")
	raw := s.rawView()
	plain := ansiSeq.ReplaceAllString(raw, "")
	for _, want := range []string{"head", "one", "two"} {
		if !strings.Contains(plain, want) {
			t.Errorf("%q did not survive rendering:\n%s", want, plain)
		}
	}
	if raw == plain {
		t.Error("the frame carries no styling at all — the guard ate magi's own escapes")
	}
}
