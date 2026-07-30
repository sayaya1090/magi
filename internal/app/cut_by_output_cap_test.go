package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A reply the provider ended at the output-token cap closes its stream normally: no error, a
// proper finish, and whatever text arrived is persisted. Nothing distinguished it from a reply
// the model chose to end, so a half-written answer — or a tool call whose arguments were cut
// mid-JSON — was read as the whole of what the model meant to say. The cap is also the ONLY
// bound left once [limits] max_output_tokens is set, because consumeStream drops the reasoning
// spin guard in deference to it, so its firing has to be visible.
func TestACutReplyIsNotPassedOffAsAFinishedOne(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		{ // step 0: the cap ended it mid-sentence
			{Type: port.ProviderText, Text: "I will start by reading the confi"},
			{Type: port.ProviderFinish, FinishReason: "length"},
		},
		textStep("the finished answer"), // step 1: continues after being told
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "read the config"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForTerminal(t, a, sid)

	told := false
	for _, e := range got {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorSystem &&
			strings.Contains(string(e.Data), "output-token cap") {
			told = true
		}
	}
	if !told {
		t.Fatal("a reply cut at the output-token cap must say so — otherwise the prefix reads as the whole answer")
	}
}

// Silent on every other ending. "stop" and "tool_calls" are the ordinary way a reply ends, and a
// note on each one is a per-step tax on the context it warns about.
func TestTheCutNoteIsSilentOnAnOrdinaryEnding(t *testing.T) {
	for _, reason := range []string{"stop", "tool_calls", ""} {
		llm := &fakeLLM{steps: [][]port.ProviderEvent{
			{
				{Type: port.ProviderText, Text: "done reading."},
				{Type: port.ProviderFinish, FinishReason: reason},
			},
		}}
		a, wd := newApp(t, llm, Config{Permission: "allow"})
		ctx := context.Background()
		sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
		a.Submit(ctx, command.SubmitPrompt{
			SessionID: sid,
			Parts:     []session.Part{{Kind: session.PartText, Text: "read the config"}},
			Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
		})
		for _, e := range waitForTerminal(t, a, sid) {
			if e.Type == event.TypePromptSubmitted && strings.Contains(string(e.Data), "output-token cap") {
				t.Errorf("finish_reason %q is not a cut; the note is noise", reason)
			}
		}
	}
}
