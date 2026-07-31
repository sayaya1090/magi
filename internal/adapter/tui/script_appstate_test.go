package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/port"
)

// Sweep three: the surfaces that only exist once the APP has state. The previous sweep proved the
// difference matters — a panel test that fed the view instead of the app rendered no panel and
// asserted nothing — so each of these builds the real state first and refuses to run without it.

// Resuming is how a user comes back to work, and the list is the whole screen while it is up. It
// was at 0%: never rendered in a test, on a path every returning user walks.
func TestTheResumeListOverRealSessions(t *testing.T) {
	s := newScript(t)
	ctx := context.Background()
	// Two more sessions in the same workdir, each with a prompt so they have something to show.
	// CreateSession only — Submit would start a real turn goroutine that outlives the test and
	// races t.TempDir's cleanup. The list needs sessions to exist, not to have run.
	for range []int{0, 1} {
		if _, err := s.m.app.CreateSession(ctx, command.CreateSession{Workdir: s.m.workdir}); err != nil {
			t.Fatal(err)
		}
	}
	metas, err := s.m.app.ListSessions(ctx, s.m.workdir)
	if err != nil || len(metas) < 2 {
		t.Fatalf("the store has %d sessions; the list needs something to list (%v)", len(metas), err)
	}

	s.typeText("/resume").enter()
	if !s.m.resuming {
		t.Fatal("/resume did not open the list, so this asserts nothing")
	}
	s.renders("the resume list")

	// Arrow keys move the selection and must stay inside the list.
	before := s.m.resumeSel
	s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	if s.m.resumeSel == before {
		t.Error("down did not move the selection")
	}
	for i := 0; i < len(metas)+5; i++ {
		s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if s.m.resumeSel < 0 || s.m.resumeSel >= len(s.m.resumeList) {
		t.Errorf("the selection left the list: %d of %d", s.m.resumeSel, len(s.m.resumeList))
	}
	s.renders("the resume list after paging down")

	s.send(tea.KeyPressMsg{Code: tea.KeyEscape})
	if s.m.resuming {
		t.Error("esc must leave the resume list")
	}
}

// A background job opens a pane of its own, and the strip has to carry it while it runs and let it
// go when it exits. Started through the real tool, so the registry the pane reads is the one the
// agent writes.
func TestABackgroundJobGetsAPaneAndThenLetsGo(t *testing.T) {
	s := newScript(t)
	raw, _ := json.Marshal(map[string]any{"command": "sleep 0.4", "background": true})
	res, err := (builtin.Bash{}).Execute(context.Background(), raw, port.ToolEnv{
		Workdir: s.m.workdir, ScratchTmp: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	if len(s.m.app.BackgroundJobs()) == 0 {
		t.Fatal("no job was registered, so the pane has nothing to show")
	}

	if !s.m.syncJobPanes() {
		t.Error("a new job must open a pane")
	}
	if len(s.m.panes) == 0 {
		t.Fatal("the job registered but no pane appeared")
	}
	s.renders("a running job's pane")

	// It exits on its own; the strip must notice rather than showing it as running forever.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.m.syncJobPanes()
		done := true
		for _, j := range s.m.app.BackgroundJobs() {
			if j.Running {
				done = false
			}
		}
		if done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.m.syncJobPanes()
	for _, j := range s.m.app.BackgroundJobs() {
		if j.Running {
			t.Fatalf("the job never exited: %+v", j)
		}
	}
	s.renders("after the job exited")
	// The status line is what tells the user which of the two it is.
	for _, p := range s.m.panes {
		if st := s.m.paneStatusPlain(p); strings.TrimSpace(st) == "" {
			t.Error("a pane with no status says nothing about whether its job is alive")
		}
	}
}

// Picking a row actually resumes: the view swaps to that session's transcript. Opening the list is
// the easy half — this is the half that changes what the user is looking at, and getting it wrong
// leaves them in a session they did not choose with the previous one's history on screen.
func TestResumingARowSwitchesTheSession(t *testing.T) {
	s := newScript(t)
	ctx := context.Background()
	if _, err := s.m.app.CreateSession(ctx, command.CreateSession{Workdir: s.m.workdir}); err != nil {
		t.Fatal(err)
	}
	was := s.m.sid

	s.typeText("/resume").enter()
	if !s.m.resuming {
		t.Fatal("/resume did not open the list")
	}
	// Land on a row that is not the session we are already in, or the switch proves nothing.
	for i := 0; i < len(s.m.resumeList); i++ {
		if s.m.resumeList[s.m.resumeSel].ID != was {
			break
		}
		s.send(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if s.m.resumeList[s.m.resumeSel].ID == was {
		t.Skip("only one session in this workdir; nothing to switch to")
	}
	target := s.m.resumeList[s.m.resumeSel].ID
	s.send(tea.KeyPressMsg{Code: tea.KeyEnter})

	if s.m.resuming {
		t.Error("picking a row must close the list")
	}
	if s.m.sid != target {
		t.Errorf("resumed into %q, expected %q", s.m.sid, target)
	}
	// The previous session's transcript must not still be on screen under the new session.
	if s.m.sid != was && len(s.m.blocks) > 0 {
		for _, b := range s.m.blocks {
			if strings.Contains(b.text, "first session work") {
				t.Errorf("the session we left is still rendered: %q", b.text)
			}
		}
	}
	s.renders("after resuming another session")
}
