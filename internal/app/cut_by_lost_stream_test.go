package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// waitForFinish watches to the END of the turn. waitForTerminal stops at the first error event,
// which is exactly the event under test here — using it would report "the turn ended" for every
// run that merely logged one.
func waitForFinish(t *testing.T, a *App, sid session.SessionID) []event.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, cancelSub, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelSub()
	var got []event.Event
	for e := range ch {
		got = append(got, e)
		if e.Type == event.TypeTurnFinished {
			return got
		}
	}
	t.Fatal("the stream ended before the turn finished")
	return got
}

// cutStream is what the adapter emits when a reply arrives and the connection then ends with
// neither finish_reason nor [DONE].
func cutStream(text string) []port.ProviderEvent {
	return []port.ProviderEvent{
		{Type: port.ProviderText, Text: text},
		{Type: port.ProviderError, Err: fmt.Errorf("%w: no finish_reason and no [DONE] arrived, "+
			"so the reply above is cut off rather than complete", port.ErrStreamCut)},
	}
}

// A dropped stream used to end the RUN. magi exited 1 and the trial recorded a non-zero agent
// exit — observed live (fix-ocaml-gc, 2026-08-01) fifteen minutes in, mid-diagnosis, with the
// model perfectly healthy on either side of the drop.
//
// It is not a failed request. The reply arrived and the connection ended without declaring an
// end, which makes what was recorded a prefix — the same fact finish_reason "length" states, from
// a different cause. So the turn LANDS, the way any turn lands, with the prefix and a note saying
// which of the two cut it. The exit code follows: it came from the loop returning an error, not
// from the error event, so a turn that finishes exits 0 with the work it did.
func TestALostStreamLandsTheTurnInsteadOfEndingTheRun(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		cutStream("I will start by reading the confi"),
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "read the config"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForFinish(t, a, sid)

	// Reaching here at all is the first assertion: waitForFinish fails if turn.finished never
	// arrives, and before this fix the turn aborted on the error instead of finishing.
	told, capNote := false, false
	for _, e := range got {
		if e.Type == event.TypePromptSubmitted && e.Actor.Kind == event.ActorSystem {
			if strings.Contains(string(e.Data), "connection to the model ended mid-reply") {
				told = true
			}
			if strings.Contains(string(e.Data), "output-token cap") {
				capNote = true
			}
		}
	}
	if !told {
		t.Error("nothing told the next step its own last reply was a prefix")
	}
	if capNote {
		t.Error("a lost connection is not the output-token cap, and saying so misreads the cause")
	}
	// The error is still on the record. Continuing past it must not make it invisible.
	logged := false
	for _, e := range got {
		if e.Type == event.TypeError && strings.Contains(string(e.Data), "ended without finishing") {
			logged = true
		}
	}
	if !logged {
		t.Error("the dropped stream was swallowed — it has to stay in the record")
	}
}

// Every OTHER provider error still ends the step. The rule is about one shape — a reply that
// arrived and was cut — not about ignoring the provider.
func TestAnOrdinaryProviderErrorStillEndsTheStep(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		{{Type: port.ProviderError, Err: errors.New("401 unauthorized")}},
		textStep("this must never run"),
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
		if e.Type == event.TypePartDelta && strings.Contains(string(e.Data), "this must never run") {
			t.Fatal("a rejected request was treated as a recoverable cut and the turn carried on")
		}
	}
}

// The note is silent when nothing was cut — it is a per-step tax on the context it warns about.
func TestTheLostStreamNoteIsSilentOnACleanReply(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{textStep("done reading.")}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "read the config"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	for _, e := range waitForFinish(t, a, sid) {
		if e.Type == event.TypePromptSubmitted && strings.Contains(string(e.Data), "connection to the model ended") {
			t.Error("a reply that ended cleanly was reported as cut")
		}
	}
}

// The guard's OWN abort must land the turn too. The repetition backstop exists to stop a model
// spinning; ending the run over its intervention loses the task the guard was saving — measured
// on TB 2.1 (regex-log, 2026-08-16): the guard fired 19 seconds into the first reply, magi exited
// 1, and the trial recorded NonZeroAgentExitCode with nothing to show for it.
func TestAGuardAbortedStreamLandsTheTurnInsteadOfEndingTheRun(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{{
		{Type: port.ProviderText, Text: "the the the the "},
		{Type: port.ProviderError, Err: fmt.Errorf("%w: a degenerate repetition loop (a 4-byte unit repeated)",
			port.ErrStreamAborted)},
	}}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "write the regex"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	got := waitForFinish(t, a, sid) // fails outright if the run ended on the abort

	logged, recovered := false, false
	for _, e := range got {
		if e.Type != event.TypeError {
			continue
		}
		var d event.ErrorData
		if json.Unmarshal(e.Data, &d) != nil || !strings.Contains(d.Message, "repetition loop") {
			continue
		}
		logged = true
		recovered = d.Recovered
	}
	if !logged {
		t.Error("the guard's abort was swallowed — it has to stay in the record")
	}
	if !recovered {
		t.Error("the abort must be marked recovered, or every reader of the log quits mid-turn")
	}
}

// The mark is what readers key on, and the readers that treat an error as a turn boundary must
// ask for it. An ordinary provider failure stays unmarked, so nothing about the existing endings
// changes.
func TestOnlyRecoveredErrorsAreMarked(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		{{Type: port.ProviderError, Err: errors.New("401 unauthorized")}},
	}}
	a, wd := newApp(t, llm, Config{Permission: "allow"})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: wd})
	a.Submit(ctx, command.SubmitPrompt{
		SessionID: sid,
		Parts:     []session.Part{{Kind: session.PartText, Text: "go"}},
		Actor:     event.Actor{Kind: event.ActorUser, ID: "tui"},
	})
	for _, e := range waitForTerminal(t, a, sid) {
		if e.Type != event.TypeError {
			continue
		}
		if errorRecovered(e) {
			t.Errorf("a plain provider failure must not be marked recovered: %s", e.Data)
		}
	}
}
