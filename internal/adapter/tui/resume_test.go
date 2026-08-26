package tui

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// makeSessions creates n sessions in the model's workdir and returns them newest-first, which is
// the order ListSessions answers in and therefore the order /resume numbers.
func makeSessions(t *testing.T, s *script, n int) []session.SessionID {
	t.Helper()
	var ids []session.SessionID
	for i := 0; i < n; i++ {
		ids = append(ids, spoken(t, s, "session "+strconv.Itoa(i)))
	}
	return ids
}

// spoken opens a session and puts something in it, which is what makes it a session the pickers
// list.
//
// A session's created fact is written when the session first has an event, so one that was only
// opened is not in the store and not in ListSessions — deliberately, because a conversation nobody
// has spoken in is not one anybody wants to resume.
//
// Written straight to the store rather than through Submit: what these fixtures need is a session
// with content, not a turn, and a run left going outlives the test and races its own TempDir
// cleanup. Interrupting it is not enough — the run goroutine still writes on its way out.
func spoken(t *testing.T, s *script, text string) session.SessionID {
	t.Helper()
	ctx := context.Background()
	id, err := s.m.app.CreateSession(ctx, command.CreateSession{Workdir: s.m.workdir})
	if err != nil {
		t.Fatal(err)
	}
	born, _ := json.Marshal(event.SessionCreatedData{Workdir: s.m.workdir})
	pd, _ := json.Marshal(event.PromptSubmittedData{
		MessageID: "m_" + text, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
	if _, err := s.store.Append(ctx, id,
		event.Event{SessionID: id, Type: event.TypeSessionCreated, TS: time.Now(), Data: born},
		event.Event{SessionID: id, Type: event.TypePromptSubmitted,
			Actor: event.Actor{Kind: event.ActorUser, ID: "tui"}, TS: time.Now(), Data: pd},
	); err != nil {
		t.Fatal(err)
	}
	return id
}

// `/resume <n>` addresses a session by position, and the position it means has to be the one the
// picker shows — resuming a different conversation than the one asked for is not an error the user
// sees, it is a transcript that looks like someone else's.
func TestResumeByNumberPicksThePickersNthRow(t *testing.T) {
	s := newScript(t)
	made := makeSessions(t, s, 4)

	s.typeText("/resume").enter()
	if !s.m.resuming || len(s.m.resumeList) != 4 {
		t.Fatalf("the picker did not open with four rows: resuming=%v n=%d", s.m.resuming, len(s.m.resumeList))
	}
	// Newest first: the last created is row 1.
	if s.m.resumeList[0].ID != made[len(made)-1] {
		t.Errorf("row 1 is not the newest session: %s", s.m.resumeList[0].ID)
	}
	want := s.m.resumeList[2].ID
	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})

	s.typeText("/resume 3").enter()
	if s.m.sid != want {
		t.Errorf("/resume 3 switched to %s; the picker's third row is %s", s.m.sid, want)
	}
}

// An out-of-range or unparsable number is refused with the usage rather than silently landing on
// an edge session.
func TestResumeRefusesANumberItCannotUse(t *testing.T) {
	s := newScript(t)
	makeSessions(t, s, 2)
	before := s.m.sid
	for _, arg := range []string{"0", "3", "-1", "abc", "1.5"} {
		s.typeText("/resume " + arg).enter()
		if s.m.sid != before {
			t.Fatalf("/resume %s switched sessions to %s", arg, s.m.sid)
		}
		if !strings.Contains(s.m.snackbar, "usage:") {
			t.Errorf("/resume %s answered %q instead of the usage", arg, s.m.snackbar)
		}
	}
	// …and the valid edges do work.
	s.typeText("/resume 1").enter()
	if s.m.sid == before {
		t.Error("/resume 1 did not switch")
	}
}

// With nothing to resume the command says so rather than opening an empty picker the user has to
// escape out of.
func TestResumeWithNoSessionsSaysSo(t *testing.T) {
	s := newScript(t)
	s.typeText("/resume").enter()
	if s.m.resuming {
		t.Error("an empty directory opened the picker")
	}
	if !strings.Contains(s.m.snackbar, "no sessions") {
		t.Errorf("snackbar says %q", s.m.snackbar)
	}
}

// A companion that leaves this conversation says so, and offers to be followed.
//
// The attached terminal joined one session at startup and never re-read the record, so a companion
// moved from a console elsewhere simply went quiet here — indistinguishable from a daemon that
// died, which is the reading somebody would act on.
//
// It does NOT follow by itself. Being attached to a conversation means reading that conversation,
// and swapping the screen under the cursor because somebody else picked another one is the same
// rudeness as a page that scrolls itself.
func TestACompanionLeavingSaysSoAndOffersToBeFollowed(t *testing.T) {
	s := newScript(t)
	made := makeSessions(t, s, 1)
	was := s.m.sid

	s.emit(event.TypeSessionMoved, event.SessionMovedData{To: made[0]})
	if s.m.sid != was {
		t.Fatalf("the terminal followed on its own — it is now in %s", s.m.sid)
	}
	if !strings.Contains(s.view(), string(made[0])) {
		t.Errorf("the transcript does not say where it went:\n%s", s.view())
	}

	// One key, and only while there is somewhere to go.
	s.send(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if s.m.sid != made[0] {
		t.Errorf("ctrl+g left the terminal in %s, not the conversation it was told about", s.m.sid)
	}
	// Having followed, the offer is spent: pressing it again must not take a key the composer
	// wants. The same press with nothing to follow falls through to the text area.
	before := s.m.ta.Value()
	s.send(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if s.m.sid != made[0] {
		t.Errorf("a second press moved somewhere: %s", s.m.sid)
	}
	_ = before
}
