package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// TestCouncilGuidanceOmittedByDefault verifies that skills read during the session
// are NOT injected into council DeliberationRequest.Guidance by default (to avoid prompt bloat),
// and that MAGI_COUNCIL_GUIDANCE=1 reenables it.
func TestCouncilGuidanceOmittedByDefault(t *testing.T) {
	fc := &recordingCouncil{}
	a, sid, _ := newWorkflowApp(t, nil, &scriptPlatform{}, Config{Permission: "allow", Council: fc})
	a.cfg.Workflow = false
	ctx := context.Background()

	// Append a skill-read tool call & result to session events
	tcArgs, _ := json.Marshal(map[string]string{"name": "test_skill"})
	d1, _ := json.Marshal(event.PartAppendedData{
		Part: session.Part{
			Kind: session.PartToolCall,
			ToolCall: &session.ToolCall{
				CallID: "call_1",
				Name:   "skill",
				Args:   tcArgs,
			},
		},
	})
	a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{Kind: event.ActorAgent}, d1)

	d2, _ := json.Marshal(event.PartAppendedData{
		Part: session.Part{
			Kind: session.PartToolResult,
			ToolResult: &session.ToolResult{
				CallID:  "call_1",
				Content: json.RawMessage(`"TEST_SKILL_BODY_CONTENT"`),
			},
		},
	})
	a.appendFact(ctx, sid, event.TypePartAppended, event.Actor{Kind: event.ActorAgent}, d2)

	// 1. Default: Guidance must be empty
	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, 0, "is this done?", false); err != nil {
		t.Fatal(err)
	}
	if fc.got.Guidance != "" {
		t.Errorf("council Guidance must be empty by default, got: %q", fc.got.Guidance)
	}

	// 2. Re-enabled with MAGI_COUNCIL_GUIDANCE=1
	t.Setenv("MAGI_COUNCIL_GUIDANCE", "1")
	if _, err := a.councilAdvice(ctx, a.sessionInfo(ctx, sid), nil, 0, "is this done?", false); err != nil {
		t.Fatal(err)
	}
	if fc.got.Guidance == "" {
		t.Error("MAGI_COUNCIL_GUIDANCE=1 must re-enable Guidance injection")
	}
}
