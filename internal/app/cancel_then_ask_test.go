package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A request made after the stop is not part of what the stop clears.
//
// Measured on a live companion before this was true: press stop, type "reply with exactly: pong",
// and the log reads prompt → abandoned → "cancelled — 1 queued request(s) also cleared; your
// newest request runs next" → turn.finished, with nothing ever answering pong. The sweep ran when
// the turn finally tore down and read the log AS IT WAS THEN, so a request typed in the meantime
// was indistinguishable from the queue the person had just reset — and the note it wrote promised
// exactly the thing it had discarded.
func TestCancelDoesNotClearWhatArrivedAfterIt(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	sid := session.SessionID("s_after_stop")
	seedSession(t, a, sid)

	say := func(id, text string) {
		pd, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: id, Parts: []session.Part{{Kind: session.PartText, Text: text}}})
		if err := a.appendFact(ctx, sid, event.TypePromptSubmitted,
			event.Actor{Kind: event.ActorUser, ID: "cli"}, pd); err != nil {
			t.Fatal(err)
		}
	}
	// What was standing when the person pressed stop: the running seed and one queued request.
	say("mSeed", "the long task")
	say("mQueued", "and also this")
	a.mu.Lock()
	a.stateLocked(sid).activeSeedMsgID = "mSeed"
	a.mu.Unlock()

	_ = a.Interrupt(ctx, command.Interrupt{SessionID: sid})
	// …and now, while the turn is still tearing down, the person asks for something new.
	say("mNew", "reply with exactly: pong")
	a.abandonSeedOnCancel(ctx, sid)

	evs, _ := a.store.Read(ctx, sid, 0)
	ab := abandonedPromptIDs(evs)
	if !ab["mSeed"] || !ab["mQueued"] {
		t.Fatalf("stop must clear what stood when it was pressed (seed=%v queued=%v)",
			ab["mSeed"], ab["mQueued"])
	}
	if ab["mNew"] {
		t.Fatal("the request made after the stop was swept away with the queue — it is the next intent, not the queue")
	}
	// And it is what the next turn answers, which is what the note promises.
	entries := userPromptEntries(evs)
	if idx := seedPromptIdx(evs, nil); idx < 0 || entries[idx].MsgID != "mNew" {
		t.Errorf("the next turn should seed on the new request, not on what the stop cleared")
	}
}

// A cancel that nobody asked for — a deadline, a shutdown — clears this turn's seed and its queue,
// and does NOT go walking through the log: there is no person's queue to reset, and anything else
// unanswered is somebody's request that should still run.
func TestUnpressedCancelLeavesTheRestOfTheLogAlone(t *testing.T) {
	a, _ := storeApp(t)
	ctx := context.Background()
	sid := session.SessionID("s_timeout")
	seedSession(t, a, sid)

	for _, m := range []struct{ id, txt string }{{"mSeed", "the task"}, {"mOther", "unrelated, still waiting"}} {
		pd, _ := json.Marshal(event.PromptSubmittedData{
			MessageID: m.id, Parts: []session.Part{{Kind: session.PartText, Text: m.txt}}})
		if err := a.appendFact(ctx, sid, event.TypePromptSubmitted,
			event.Actor{Kind: event.ActorUser, ID: "cli"}, pd); err != nil {
			t.Fatal(err)
		}
	}
	a.mu.Lock()
	a.stateLocked(sid).activeSeedMsgID = "mSeed"
	a.mu.Unlock()

	a.abandonSeedOnCancel(ctx, sid) // no Interrupt: the context died on its own

	evs, _ := a.store.Read(ctx, sid, 0)
	ab := abandonedPromptIDs(evs)
	if !ab["mSeed"] {
		t.Error("the turn's own seed is over either way")
	}
	if ab["mOther"] {
		t.Error("a timeout swept a request nobody cancelled")
	}
}

// End to end: press stop mid-turn, ask for something else, and the something else runs.
//
// The abandonment above is only half of it. The other half is that nothing was left to answer the
// new request: Steer had looked while the stopped turn was still tearing down, seen a run in
// flight, and left the prompt to it — and that run was on its way out. The session went idle with
// the request sitting unanswered in the log, which is what "it just gets swallowed" looks like.
func TestARequestMadeAfterStopStillRuns(t *testing.T) {
	llm := &blockingLLM{started: make(chan struct{}), release: make(chan struct{})}
	store, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fc := &fakeCouncil{delibs: []council.Deliberation{{Round: 1, Decision: council.Done}}}
	a := closeAfter(t, New(store, llm, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Council: fc}))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.Close(ctx)
	})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir()})

	// 액터는 실제 경로와 같게 — 데몬은 늘 붙인다(붙지 않은 프롬프트는 사용자 요청으로 세어지지 않는다).
	_ = a.Submit(ctx, command.SubmitPrompt{SessionID: sid, Actor: event.Actor{Kind: event.ActorUser, ID: "cli"},
		Parts: []session.Part{{Kind: session.PartText, Text: "the long task"}}})
	<-llm.started // the turn is in flight, held open by the provider

	_ = a.Interrupt(ctx, command.Interrupt{SessionID: sid})
	// The person types the next thing while the stopped turn is still tearing down.
	_ = a.Steer(ctx, command.SubmitPrompt{SessionID: sid, Actor: event.Actor{Kind: event.ActorUser, ID: "cli"},
		Parts: []session.Part{{Kind: session.PartText, Text: "reply with exactly: pong"}}})
	close(llm.release)

	deadline := time.Now().Add(5 * time.Second)
	for {
		evs, _ := a.store.Read(ctx, sid, 0)
		open := unansweredUserPromptIDs(evs)
		answered := false
		for _, e := range evs {
			if e.Type == event.TypePartAppended && e.Actor.Kind == event.ActorAgent {
				answered = true
			}
		}
		if len(open) == 0 && answered {
			return // the new request was picked up and answered
		}
		if time.Now().After(deadline) {
			t.Fatalf("the request made after the stop was never answered (still open: %v)", open)
		}
		time.Sleep(30 * time.Millisecond)
	}
}
