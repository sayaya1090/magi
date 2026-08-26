package tui

import (
	"context"
	"strings"
	"testing"

	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/session"
)

// /sessions lists this directory's sessions and stops at ten. Two things about that are worth
// pinning, and neither shows up as an error when it is wrong.
//
// The cut has to be MARKED — the same rule the tool results, the evidence block and the compaction
// summary follow, because a reader cannot ask about what it does not know is missing. And it has to
// cut the right end: the store returns newest first, so the ten kept must be the newest ten. A
// truncation that silently kept the OLDEST ten would look identical — a list of ten sessions.
func TestSessionsListKeepsTheNewestTenAndSaysItCut(t *testing.T) {
	s := newScript(t)
	ctx := context.Background()

	var made []string
	for i := 0; i < 12; i++ {
		id, err := s.m.app.CreateSession(ctx, command.CreateSession{Workdir: s.m.workdir})
		if err != nil {
			t.Fatal(err)
		}
		made = append(made, string(id))
	}

	out := s.m.sessionsList()
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "sessions in this directory") {
		t.Fatalf("unexpected header: %q", lines[0])
	}
	// Ten rows plus the marker row.
	if got := len(lines) - 1; got != 11 {
		t.Errorf("listed %d rows for 12 sessions; want 10 + a marker\n%s", got, out)
	}
	if !strings.Contains(out, "…") {
		t.Errorf("twelve sessions were cut to ten with nothing saying so:\n%s", out)
	}
	// The newest ten, not the oldest ten — the two are indistinguishable by row count alone.
	newest := made[len(made)-1]
	oldest := made[0]
	if !strings.Contains(out, newest) {
		t.Errorf("the newest session is missing from the list:\n%s", out)
	}
	if strings.Contains(out, oldest) {
		t.Errorf("the list kept the OLDEST session, so it cut the wrong end:\n%s", out)
	}
}

// A directory with no sessions says so rather than printing a header over nothing.
func TestSessionsListSaysWhenThereAreNone(t *testing.T) {
	s := newScript(t)
	if got := s.m.sessionsList(); got != "sessions: (none)" {
		t.Errorf("an empty directory listed %q", got)
	}
}

// Under the cap every session is listed and there is no marker — a "…" on a complete list would
// claim something was left out when nothing was.
func TestSessionsListUnderTheCapIsComplete(t *testing.T) {
	s := newScript(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.m.app.CreateSession(ctx, command.CreateSession{Workdir: s.m.workdir}); err != nil {
			t.Fatal(err)
		}
	}
	out := s.m.sessionsList()
	if got := len(strings.Split(out, "\n")) - 1; got != 3 {
		t.Errorf("three sessions listed %d rows:\n%s", got, out)
	}
	if strings.Contains(out, "…") {
		t.Errorf("a complete list is marked as cut:\n%s", out)
	}
}

// The list says what each session was about, not only which hash it has.
//
// Ten ids and ten timestamps answer "how many sessions are here" and nothing else, and the one
// question somebody runs /sessions to answer is which of these was the work they mean.
// SessionMeta has carried the title all along — the resume picker has shown it since it existed —
// and this list read the same struct and dropped the field.
//
// Through a stub rather than a real turn: what is under test is the rendering of a SessionMeta,
// and Submit starts a run that outlives the test and races its own TempDir cleanup.
type listStub struct {
	Engine
	metas []session.SessionMeta
}

func (l listStub) ListSessions(context.Context, string) ([]session.SessionMeta, error) {
	return l.metas, nil
}

func TestSessionsListNamesTheWorkNotOnlyTheID(t *testing.T) {
	s := newScript(t)
	s.m.app = listStub{metas: []session.SessionMeta{
		{ID: "s-alpha", Title: "rename the token file", Created: time.Now()},
		{ID: "s-beta", Created: time.Now()},
	}}

	out := s.m.sessionsList()
	if !strings.Contains(out, "rename the token file") {
		t.Errorf("the list does not say what the session was about:\n%s", out)
	}
	// The id stays. Two sessions can be about the same thing, and the id is what tells them apart
	// and what /resume is addressed by.
	if !strings.Contains(out, "s-alpha") {
		t.Errorf("the id was dropped along with the change:\n%s", out)
	}
	// And a session nobody has spoken in says so rather than trailing off after its timestamp —
	// a blank tail is indistinguishable from a title that failed to load.
	if !strings.Contains(out, "(no messages)") {
		t.Errorf("an untitled session trails off after its timestamp:\n%s", out)
	}
}
