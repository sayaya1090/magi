package app

import (
	"github.com/sayaya1090/magi/internal/port"
	"strings"
	"testing"
)

type configuredCodingTasks struct{ port.CodingTasks }

func TestCodingWorkflowIsRequiredOnlyForCodingClientsWithWorkspaceService(t *testing.T) {
	for _, role := range []string{"", "coding", "browser", "figma", "mail", "slack", "powerpoint", "word", "excel", "docker", "kubernetes"} {
		a := &App{cfg: Config{ClientRole: role, CodingTasks: &configuredCodingTasks{}}}
		guide := a.codingWorkflowGuide()
		want := role == "" || role == "coding"
		if (guide != "") != want {
			t.Fatalf("role %q guide=%q", role, guide)
		}
		if want {
			for _, term := range []string{"MUST call coding_task", "mode=git", "does NOT move", "Do not call council complete=true", "mode=copy", "do not nest worktrees"} {
				if !strings.Contains(guide, term) {
					t.Errorf("missing contract %q", term)
				}
			}
		}
	}
	a := &App{cfg: Config{ClientRole: "coding"}}
	if a.codingWorkflowGuide() != "" {
		t.Fatal("instructed an unavailable task service")
	}
}

func TestCodingTaskRunnerDoesNotRecursivelyPrepareWorkspaces(t *testing.T) {
	a := &App{cfg: Config{ClientRole: "coding", CodingTasks: &configuredCodingTasks{}, CodingTaskExecution: true}}
	guide := a.codingWorkflowGuide()
	if !strings.Contains(guide, "ALREADY inside") || strings.Contains(guide, "MUST call coding_task") {
		t.Fatal(guide)
	}
}
