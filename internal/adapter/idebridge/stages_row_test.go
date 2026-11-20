package idebridge

import (
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// The stages a tool reports ride its own call's row; each report replaces the last, and a progress note
// with no stages, or one for another call, leaves the row as it was.
func TestAToolsStagesRideItsOwnCallsRow(t *testing.T) {
	events := []event.Event{
		mk(1, "part.appended", map[string]any{"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{"callId": "run1", "name": "long_job"}}}, nil),
		mk(2, "part.appended", map[string]any{"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{"callId": "other", "name": "read"}}}, nil),
		mk(3, "tool.progress", map[string]any{"callId": "run1", "name": "long_job", "stages": []any{map[string]any{"id": "s0", "label": "a", "state": "running"}}}, nil),
		mk(4, "tool.progress", map[string]any{"callId": "run1", "name": "long_job", "stages": []any{map[string]any{"id": "s0", "label": "a", "state": "done"}, map[string]any{"id": "s1", "label": "b", "kind": "subagent", "state": "running", "done": 1, "total": 3, "running": 2}}}, nil),
		mk(5, "tool.progress", map[string]any{"callId": "other", "name": "read", "text": "no stages"}, nil),
	}
	rows := Rows(events)
	if len(rows) != 2 {
		t.Fatalf("stages made a row of their own: %+v", rows)
	}
	for _, r := range rows {
		switch r.CallID {
		case "run1":
			if len(r.Stages) != 2 || r.Stages[0].State != "done" || r.Stages[1] != (event.ProgressStage{ID: "s1", Label: "b", Kind: "subagent", State: "running", Done: 1, Total: 3, Running: 2}) {
				t.Errorf("run1 stages %+v", r.Stages)
			}
		case "other":
			if len(r.Stages) != 0 {
				t.Errorf("another call got stages: %+v", r.Stages)
			}
		}
	}
}
