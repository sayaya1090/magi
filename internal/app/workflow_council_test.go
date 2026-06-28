package app

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/council"
)

// In workflow mode the consensus council must NOT convene — the pipeline owns its own
// verify gate (documented). Run the pipeline with a council configured and assert it
// is never asked to deliberate.
func TestCouncilInactiveInWorkflow(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Decision: council.Done}}}
	a, sid, _ := newWorkflowApp(t, &workflowLLM{}, nil, Config{
		Permission: "allow", System: "base", Workflow: true, Council: fc,
	})
	if err := a.runWorkflow(context.Background(), a.sessionInfo(context.Background(), sid)); err != nil {
		t.Fatalf("workflow: %v", err)
	}
	if fc.calls != 0 {
		t.Errorf("council deliberated %d times in workflow mode; it must stay inactive", fc.calls)
	}
}
