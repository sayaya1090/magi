package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sayaya1090/magi/internal/core/event"
)

// Sweep: every slash command, through the real key path.
//
// handleSlash sat at 24% — three quarters of the branches a user reaches by typing "/" had never
// been run in a test. It is the most-touched dispatcher in the UI and the one place where doing
// nothing looks exactly like doing something: the input is Reset() first, so a command that falls
// through leaves a cleared box and an unchanged screen, which reads as "it worked".
//
// So the property asserted here is not what each command produces — that differs per command and
// most of them need a session with history to produce anything interesting. It is that every one
// of them ANSWERS: a snack, an info block, a new block, a view taking over, or the quit flag.
// Silence is the failure.

// slashState is the part of the model a command can answer through.
type slashState struct {
	blocks  int
	snack   string
	snackN  int
	routing bool
	resume  bool
	quit    bool
	perm    string
}

func snapSlash(m *Model) slashState {
	return slashState{
		blocks: len(m.blocks), snack: m.snackbar, snackN: m.snackSeq,
		routing: m.routing, resume: m.resuming, quit: m.quitting, perm: m.app.Permission(),
	}
}

func (a slashState) answered(b slashState) bool {
	return b.blocks != a.blocks || b.snackN != a.snackN || b.routing != a.routing ||
		b.resume != a.resume || b.quit != a.quit || b.perm != a.perm
}

// Every command magi advertises. A few are given the argument they document, because the
// no-argument branch of those is itself a usage snack and would pass this trivially.
func TestEverySlashCommandAnswers(t *testing.T) {
	for _, cmd := range []string{
		"/help", "/?", "/tools", "/sessions", "/permission", "/diff", "/loop", "/context",
		"/cost", "/clear", "/model", "/agents", "/route", "/resume", "/rewind", "/rewind 2",
		"/fork", "/loopdiff", "/replay", "/compact", "/init", "/ultra", "/ultra do the thing",
		"/image", "/image nope.png", "/quit", "/exit", "/notacommand",
	} {
		t.Run(strings.ReplaceAll(strings.TrimPrefix(cmd, "/"), " ", "_"), func(t *testing.T) {
			s := newScript(t)
			// A little history, so the commands that read the transcript have something to read.
			s.steer("r1", "earlier work")
			s.assistantText("an earlier answer")

			s.emit(event.TypeTurnFinished, event.TurnFinishedData{})
			if s.m.running {
				t.Fatal("the fixture left a turn running; the session-changing commands would all refuse")
			}

			before := snapSlash(&s.m)
			s.typeText(cmd).enter()
			after := snapSlash(&s.m)

			if !after.answered(before) {
				t.Errorf("%q changed nothing — cleared the input and left the screen as it was", cmd)
			}
			if v := s.m.ta.Value(); v != "" {
				t.Errorf("%q left %q in the input box", cmd, v)
			}
			// Whatever it did, the frame is still a frame — unless the command was to leave,
			// which draws nothing on purpose: the alt screen is being handed back.
			raw := s.rawView()
			if strings.TrimSpace(ansiSeq.ReplaceAllString(raw, "")) == "" && !s.m.quitting {
				t.Fatalf("%q blanked the screen", cmd)
			}
			lines := strings.Split(raw, "\n")
			if len(lines) > s.m.height {
				t.Errorf("%q made the frame %d rows in a %d-row terminal", cmd, len(lines), s.m.height)
			}
			for i, line := range lines {
				trimmed := strings.TrimRight(ansiSeq.ReplaceAllString(line, ""), " ")
				if w := lipgloss.Width(trimmed); w > s.m.width {
					t.Errorf("%q: row %d draws %d cells in %d columns: %q", cmd, i, w, s.m.width, trimmed)
				}
			}
		})
	}
}

// An unknown command must SAY it is unknown. This is the branch that makes the sweep above worth
// running: without it, a typo is indistinguishable from a command that ran.
func TestAnUnknownCommandSaysSo(t *testing.T) {
	s := newScript(t)
	s.typeText("/nosuchthing").enter()
	if !strings.Contains(s.m.snackbar, "unknown command") || !strings.Contains(s.m.snackbar, "/nosuchthing") {
		t.Errorf("a typo was swallowed; the snackbar says %q", s.m.snackbar)
	}
}

// The commands that change the session refuse while a turn is running, rather than racing it —
// clearing the transcript mid-stream would wipe the live turn from view, and rewind/fork/replay/
// compact all mutate the event store the turn is appending to.
//
// The refusal comes from the central gate, ahead of handleSlash, which is why the wording is its
// own ("can't run X while working") and why the typed text SURVIVES: the command never reached the
// Reset() at the top of handleSlash, so it is still there to retry when the turn ends. The
// per-command `if m.running` arms inside handleSlash are backstops behind that gate; they are what
// keeps the guarantee if a future caller reaches the dispatcher another way.
func TestSessionChangingCommandsRefuseWhileRunning(t *testing.T) {
	for _, cmd := range []string{"/clear", "/rewind", "/fork", "/replay", "/compact"} {
		t.Run(strings.TrimPrefix(cmd, "/"), func(t *testing.T) {
			s := newScript(t)
			s.steer("r1", "a turn in flight")
			s.m.running = true
			blocks := len(s.m.blocks)

			s.typeText(cmd).enter()

			if !strings.Contains(s.m.snackbar, cmd) || !strings.Contains(s.m.snackbar, "while working") {
				t.Errorf("%q while running said %q — a refusal has to name what it refused", cmd, s.m.snackbar)
			}
			if len(s.m.blocks) < blocks {
				t.Errorf("%q destroyed %d blocks of a running turn", cmd, blocks-len(s.m.blocks))
			}
		})
	}
}

// /permission cycles ask→auto→allow→deny→ask and names the mode it landed on. A cycle that
// silently wraps to the same value would leave the user pressing it forever.
func TestPermissionCyclesThroughEveryMode(t *testing.T) {
	s := newScript(t)
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		s.typeText("/permission").enter()
		mode := s.m.app.Permission()
		seen[mode] = true
		if !strings.Contains(s.m.snackbar, mode) {
			t.Errorf("the snackbar %q does not name the new mode %q", s.m.snackbar, mode)
		}
	}
	for _, want := range []string{"ask", "auto", "allow", "deny"} {
		if !seen[want] {
			t.Errorf("five presses never reached %q; saw %v", want, seen)
		}
	}
}

// /clear empties the transcript AND everything derived from it. A search running over the old
// lines would otherwise keep hits pointing into a transcript that no longer exists.
func TestClearLeavesNothingPointingAtTheOldTranscript(t *testing.T) {
	s := newScript(t)
	s.steer("r1", "the needle is here")
	s.emit(event.TypeTurnFinished, event.TurnFinishedData{}) // steer starts a turn; /clear refuses during one
	for i := 0; i < 10; i++ {
		s.assistantText("more needle lines to search")
	}
	openSearch(s, "needle")
	if len(s.m.searchHits) == 0 {
		t.Fatal("nothing matched, so this proves nothing")
	}
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape}) // leave the search bar first
	s.typeText("/clear").enter()

	if len(s.m.blocks) != 0 {
		t.Errorf("/clear left %d blocks", len(s.m.blocks))
	}
	if len(s.m.cache) != 0 {
		t.Errorf("/clear left %d cached renders", len(s.m.cache))
	}
	_ = s.rawView()
	for _, h := range s.m.searchHits {
		if h < 0 || h >= len(s.m.contentPlain) {
			t.Errorf("a search hit at %d survived into a %d-line transcript", h, len(s.m.contentPlain))
		}
	}
}
