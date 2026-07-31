package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// makeSessions creates n sessions in the model's workdir and returns them newest-first, which is
// the order ListSessions answers in and therefore the order /resume numbers.
func makeSessions(t *testing.T, s *script, n int) []session.SessionID {
	t.Helper()
	var ids []session.SessionID
	for i := 0; i < n; i++ {
		id, err := s.m.app.CreateSession(context.Background(), command.CreateSession{Workdir: s.m.workdir})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
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
