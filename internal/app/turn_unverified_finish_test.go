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

// A turn whose council never approves (round-cap deadlock) must land its
// turn.finished with Unverified=true and a reason — the TUI reads that field to
// distinguish an abandoned/unverified turn from a genuine done. Regression: the
// deadlock recorded an UNVERIFIED council note but emitted a clean turn.finished
// (Unverified=false), so the UI painted an abandoned task as a confident "done".
func TestDeadlockFinishMarkedUnverified(t *testing.T) {
	// Council that always says "keep working" → it never approves.
	fc := &fakeCouncil{delibs: []council.Deliberation{
		{Decision: council.Continue, Feedback: "the review is not complete yet"},
	}}
	// tool work (so the gate applies) then two finishes: round 1 → Continue,
	// round 2 → round-cap deadlock finish.
	llm := workingLLM(textStep("answer 1"), textStep("answer 2"))
	a, wd := newApp(t, llm, Config{Council: fc, CouncilMaxRounds: 1, Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})

	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "review the whole project"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForTerminal(t, a, sid)

	// The council recorded the finish as unverified...
	unverifiedNote := false
	for _, e := range got {
		if e.Type == event.TypeCouncilDecided && strings.Contains(string(e.Data), "UNVERIFIED") {
			unverifiedNote = true
		}
	}
	if !unverifiedNote {
		t.Fatal("setup: expected a council UNVERIFIED deadlock note")
	}

	// ...and the turn.finished the TUI reads carries it.
	var tf *event.TurnFinishedData
	for _, e := range got {
		if e.Type == event.TypeTurnFinished {
			var d event.TurnFinishedData
			if json.Unmarshal(e.Data, &d) == nil {
				tf = &d
			}
		}
	}
	if tf == nil {
		t.Fatal("no turn.finished emitted")
	}
	if !tf.Unverified {
		t.Fatal("deadlock finish must set turn.finished Unverified=true so the UI does not show a clean done")
	}
	if strings.TrimSpace(tf.Reason) == "" {
		t.Error("an unverified finish should carry a human-readable reason")
	}
}

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
	llm := workingLLM(textStep("the answer"))
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
