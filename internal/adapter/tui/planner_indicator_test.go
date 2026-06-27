package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

func planPhaseEvent(t *testing.T, status, detail string) event.Event {
	t.Helper()
	d, _ := json.Marshal(event.WorkflowPhaseData{Phase: "plan", Status: status, Detail: detail})
	return event.Event{Type: event.TypeWorkflowPhase, Data: d}
}

// The planner's decision (solo / parallel N) reaches the header indicator via the
// workflow.phase event.
func TestPlannerModeIndicator(t *testing.T) {
	mm := newTestModel(t)
	m := &mm

	m.applyEvent(planPhaseEvent(t, "parallel", "3 explorers · auth/concurrency/errors are independent"))
	if m.plannerMode != "parallel" {
		t.Errorf("plannerMode = %q after parallel", m.plannerMode)
	}
	// The reason is surfaced as a visible transcript line.
	last := m.blocks[len(m.blocks)-1]
	if last.kind != blockInfo || !strings.Contains(last.text, "independent") {
		t.Errorf("planner reason not shown as a line: %+v", last)
	}

	m.applyEvent(planPhaseEvent(t, "solo", "single file change"))
	if m.plannerMode != "solo" {
		t.Errorf("plannerMode = %q after solo", m.plannerMode)
	}
	if last := m.blocks[len(m.blocks)-1]; !strings.Contains(last.text, "single file change") {
		t.Errorf("solo reason not shown: %+v", last)
	}

	// A non-plan phase (e.g. workflow) does not change the planner indicator.
	d, _ := json.Marshal(event.WorkflowPhaseData{Phase: "verify", Status: "pass"})
	m.applyEvent(event.Event{Type: event.TypeWorkflowPhase, Data: d})
	if m.plannerMode != "solo" {
		t.Errorf("non-plan phase should not change plannerMode, got %q", m.plannerMode)
	}
}
