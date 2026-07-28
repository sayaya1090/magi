package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A top-level turn that runs NO tool and ends with only reasoning (empty answer
// text) delivered nothing. Before the fix it finished silently as a confident
// done — no deliverable, Unverified=false — because the council gate requires
// usedTools and the empty-turn nudge was subagent-only. The orchestrator is now
// nudged once to produce a real result. Regression guard for the harmony-format
// weak-model "reasoning-only stop" observed in the field (hard-battery bigfile).
func TestTopLevelReasoningOnlyNudged(t *testing.T) {
	reasoningOnly := []port.ProviderEvent{
		{Type: port.ProviderReasoning, Text: "I need to count rows... let's list files."},
		{Type: port.ProviderFinish},
	}
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		reasoningOnly,               // step 0: reasoning-only, no tool, no answer text
		textStep("the real answer"), // step 1: after the nudge, deliver the result
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "count the rows"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForTerminal(t, a, sid)

	// The orchestrator injected an empty-result nudge instead of finishing silently...
	nudged := false
	for _, e := range got {
		if e.Type == event.TypePromptSubmitted && e.Actor.ID == "orchestrator" &&
			strings.Contains(string(e.Data), "without giving a result") {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("a top-level reasoning-only turn must be nudged to produce a result, not finish silently as done")
	}
	// ...and the nudged answer landed as the delivered result.
	delivered := false
	for _, e := range got {
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "the real answer") {
			delivered = true
		}
	}
	if !delivered {
		t.Errorf("expected the nudged answer to be delivered, got events %v", typesOf(got))
	}
}

// A top-level turn that USED tools and then goes silent on its final step (empty
// answer text) must be nudged too — not only the no-tool-at-all case. Before the
// fix the empty-turn nudge required !usedTools, so a turn that ran tools and then
// stopped with reasoning-only/empty text slipped past it into the council gate,
// which (seeing the tool work) could vote "done" and finish with no deliverable
// text — the user got silence. Field repro: hard-battery loop task on a harmony
// weak model (tool calls, then a reasoning-only final step, council done, no
// answer). Subagents already got this nudge; top level must match.
func TestTopLevelToolUseEmptyTextNudged(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		toolStep("list", `{"path":"."}`), // step 0: real tool work (usedTools=true)
		{{Type: port.ProviderFinish}},    // step 1: empty final step — no tool, no text
		textStep("the delivered answer"), // step 2: after the nudge, produce the result
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "list the dir and report"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForTerminal(t, a, sid)

	nudged := false
	for _, e := range got {
		if e.Type == event.TypePromptSubmitted && e.Actor.ID == "orchestrator" &&
			strings.Contains(string(e.Data), "without giving a result") {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("a top-level turn that used tools then went empty must be nudged, not finish silently with no deliverable text")
	}
	delivered := false
	for _, e := range got {
		if e.Type == event.TypePartAppended && strings.Contains(string(e.Data), "the delivered answer") {
			delivered = true
		}
	}
	if !delivered {
		t.Errorf("expected the nudged answer to be delivered, got events %v", typesOf(got))
	}
}

// A turn the council genuinely approves finishes verified: Unverified=false, no
// reason — the common case must not be mislabeled by the propagation above.
func TestApprovedFinishNotUnverified(t *testing.T) {
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	// The agent works, then DECLARES the task finished; the council reads the record and accepts.
	llm := workingLLM(toolStep("council", `{"complete":true}`), textStep("the answer"))
	a, wd := newApp(t, llm, Config{Council: fc, Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "do the thing"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForTerminal(t, a, sid)

	for _, e := range got {
		if e.Type == event.TypeTurnFinished {
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) == nil && d.Unverified {
				t.Fatalf("a council-approved finish must be verified, got Unverified=true reason=%q", d.Reason)
			}
		}
	}
}
