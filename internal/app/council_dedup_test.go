package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// TestCouncilDeduplicationOmitsToolEvidenceFromActions verifies that when EvidencePrefix
// already carries chronological turn events (tool results, prior council feedback), those
// tool results and objection summaries are omitted from the snapshot Actions block by default
// to eliminate 15-20 KB of un-cached prompt bloat, and that MAGI_COUNCIL_DEDUP=0 restores them.
func TestCouncilDeduplicationOmitsToolEvidenceFromActions(t *testing.T) {
	fc := &recordingCouncil{}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()

	// 1. Append a user prompt
	pdata, _ := json.Marshal(event.PromptSubmittedData{
		Parts: []session.Part{{Kind: session.PartText, Text: "run tests"}},
	})
	a.appendFact(ctx, sid, event.TypePromptSubmitted, event.Actor{Kind: event.ActorUser, ID: "tester"}, pdata)

	// 2. Append a tool call & result
	tcArgs, _ := json.Marshal(map[string]string{"command": "pytest -q"})
	d1, _ := json.Marshal(event.PartAppendedData{
		Part: session.Part{
			Kind: session.PartToolCall,
			ToolCall: &session.ToolCall{
				CallID: "call_test_1",
				Name:   "bash",
				Args:   tcArgs,
			},
		},
	})
	a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{Kind: event.ActorAgent}, d1)

	d2, _ := json.Marshal(event.PartAppendedData{
		Part: session.Part{
			Kind: session.PartToolResult,
			ToolResult: &session.ToolResult{
				CallID:  "call_test_1",
				Content: json.RawMessage(`"5 passed in 0.12s"`),
			},
		},
	})
	a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{Kind: event.ActorAgent}, d2)

	// 3. Append a prior council rejection verdict
	vd, _ := json.Marshal(event.CouncilVerdictData{
		Member:   "Melchior",
		Lens:     "verification",
		Decision: "continue",
		Feedback: "edge cases were not tested",
	})
	a.appendFact(ctx, sid, event.TypeCouncilVerdict, event.Actor{Kind: event.ActorSystem, ID: "council"}, vd)

	// Verify default behavior: EvidencePrefix has history, Actions omits duplicate tool evidence
	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, 0, "is this ready?", false); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(fc.got.EvidencePrefix, "5 passed in 0.12s") {
		t.Fatalf("EvidencePrefix must carry the chronological tool result:\n%s", fc.got.EvidencePrefix)
	}
	if !strings.Contains(fc.got.EvidencePrefix, "edge cases were not tested") {
		t.Fatalf("EvidencePrefix must carry the prior council feedback:\n%s", fc.got.EvidencePrefix)
	}

	// Actions block must NOT duplicate the tool output or the prior objection block
	if strings.Contains(fc.got.Actions, "5 passed in 0.12s") {
		t.Errorf("Actions block must NOT duplicate tool results already present in EvidencePrefix:\n%s", fc.got.Actions)
	}
	if strings.Contains(fc.got.Actions, "WHAT THIS COUNCIL SAID EARLIER THIS TURN") {
		t.Errorf("Actions block must NOT duplicate prior objections already present in EvidencePrefix:\n%s", fc.got.Actions)
	}

	// Verify legacy behavior when MAGI_COUNCIL_DEDUP=0
	t.Setenv("MAGI_COUNCIL_DEDUP", "0")
	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, 0, "is this ready?", false); err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(fc.got.Actions, "5 passed in 0.12s") {
		t.Errorf("MAGI_COUNCIL_DEDUP=0 must restore tool output in Actions block:\n%s", fc.got.Actions)
	}
	if !strings.Contains(fc.got.Actions, "WHAT THIS COUNCIL SAID EARLIER THIS TURN") {
		t.Errorf("MAGI_COUNCIL_DEDUP=0 must restore prior objections in Actions block:\n%s", fc.got.Actions)
	}
}
