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

// The output cap and the spin guard were treated as one thing. They are not: a cap bounds how BIG
// one response gets, the guard notices there is still no ACTION and says to take one. Deferring to
// the cap kept the bound and dropped the recovery — the reply ends mid-thought, carries no tool
// call, and the next step starts the same way. Observed twice in one external run (extract-elf and
// large-scale-text-editing, 2026-07-31): both reasoned into the cap step after step, never called a
// tool, never reached the council, landed unverified with the deliverable never written.
func TestTheSpinGuardSurvivesAnOutputCap(t *testing.T) {
	t.Setenv("MAGI_SPIN_CAP", "") // the default, not a test override
	for _, cap := range []int{0, 4096} {
		a, _ := newApp(t, &fakeLLM{}, Config{Permission: "allow", MaxOutputTokens: cap})
		if a.cfg.MaxOutputTokens != cap {
			t.Fatalf("cap not carried: %d", a.cfg.MaxOutputTokens)
		}
	}
	// The guard's own threshold is what decides, and it must not be zeroed by anything but its
	// own env override — zero disables it, and disabled is what removed the nudge.
	if reasoningSpinCap() <= 0 {
		t.Fatal("the default spin cap must be armed")
	}
	t.Setenv("MAGI_SPIN_CAP", "0")
	if reasoningSpinCap() != 0 {
		t.Error("only MAGI_SPIN_CAP may disable the guard")
	}
}

// A cut reply that made no tool call needs different advice from one that did. "Continue in
// smaller pieces" tells a model that spent its whole budget on text to write more text.
func TestACutThatNeverActedIsToldToAct(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		{ // reasoned into the cap, no tool call
			{Type: port.ProviderText, Text: "Let me think about the ELF header layout in detail"},
			{Type: port.ProviderFinish, FinishReason: "length"},
		},
		textStep("done"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "extract the values"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	var told string
	for _, e := range waitForTerminal(t, a, sid) {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorSystem {
			if strings.Contains(string(e.Data), "output-token cap") {
				told = string(e.Data)
			}
		}
	}
	if told == "" {
		t.Fatal("a cut reply must still be reported")
	}
	if !strings.Contains(told, "before it made a single tool call") {
		t.Errorf("a cut that never acted must be told to act, not to keep writing:\n%s", told)
	}
	if strings.Contains(told, "in smaller pieces") {
		t.Errorf("that is the advice for a cut that DID act:\n%s", told)
	}
}
