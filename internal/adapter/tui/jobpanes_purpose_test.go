package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

func TestBackgroundJobPaneShowsPurposeInsteadOfBareIDOrCdPrefix(t *testing.T) {
	p := &agentPane{
		job:  "bg_1",
		role: "bg_1",
		task: "cd '/Users/sayaya/Library/Application Support/magi/coding-tasks/tasks/6dd898d2ee6c9682a0d1916c/workspace' && ./gradlew :intellij:test",
		sub:  1,
		done: true, exited: true, dur: 12 * time.Second,
	}
	got := p.desc(40)
	if strings.Contains(got, "cd '") {
		t.Fatalf("expected leading cd boilerplate to be stripped from desc, got %q", got)
	}
	if !strings.Contains(got, "bg_1") || !strings.Contains(got, "./gradlew :intellij:test") {
		t.Fatalf("expected job id and meaningful command in desc, got %q", got)
	}

	mm := newTestModel(t)
	m := &mm
	m.width, m.height = 120, 40
	m.panelW = defaultPanelWidth
	m.doneRoster = []*agentPane{p}
	panel := m.statusPanel(2)
	if !strings.Contains(panel, "bg_1 · ./gradl") {
		t.Fatalf("expected right-hand Background panel to show task purpose alongside bg_1, got: %q", panel)
	}
}

func TestBackgroundJobHonorsExplicitDescriptionAndRetiresAfterTurn(t *testing.T) {
	s := newScript(t)
	raw, _ := json.Marshal(map[string]any{
		"command":    "cd /tmp && echo 플러그인 단위 테스트",
		"background": true,
	})
	res, err := (builtin.Bash{}).Execute(context.Background(), raw, port.ToolEnv{
		Workdir: s.m.workdir, ScratchTmp: t.TempDir(),
	})
	if err != nil || res.IsError {
		t.Fatalf("start background job: err=%v res=%s", err, res.Content)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.m.syncJobPanes()
		allDone := len(s.m.panes) > 0
		for _, p := range s.m.panes {
			if !p.done {
				allDone = false
			}
		}
		if allDone {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	found := false
	for _, p := range s.m.panes {
		if strings.Contains(p.desc(40), "플러그인 단위 테스트") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected pane desc to contain explicit description, panes=%+v", s.m.panes)
	}

	// Starting a new turn closes finished panes; subsequent polls must not resurrect them.
	s.m.closePanes()
	if s.m.syncJobPanes() || len(s.m.panes) != 0 || len(s.m.doneRoster) != 0 {
		t.Fatalf("finished background jobs retired by closePanes must not reappear on next poll: panes=%d roster=%d",
			len(s.m.panes), len(s.m.doneRoster))
	}
}
