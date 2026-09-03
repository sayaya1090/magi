package main

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/mcpserve"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/meeting"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// A meeting turn keeps TWO sessions, and they are not the same one.
//
// The whole of the design is that a participant decides in a context its own edit history never
// reaches. Both arrangements answer, so if the write-up were issued against the speaking session
// nothing would fail — every participant would simply start arguing with the sentence it deleted
// two rounds ago, which is invisible from the outside and shows up only as worse meetings.
//
// Walked on the real engine because the routing is in the engine, not in App: App is handed the
// session to write into, and which one it is handed is exactly the thing at risk.
func TestAMeetingTurnKeepsTheSpeakingAndMinutesSessionsApart(t *testing.T) {
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A model that answers one line to anything. Both halves of the turn have to actually RUN for
	// their sessions to exist, and what either of them says is not what this test is about.
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	a := app.New(st, sameAnswer("noted"), builtin.NewRegistry(), bus.New(), nil,
		app.Config{Permission: "allow", Models: reg})
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	ctx := context.Background()
	wd := t.TempDir()

	parent, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd,
		Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}})
	if err != nil {
		t.Fatal(err)
	}
	// card is a function field and MeetingTurn asks it who this companion is. nil panics — the
	// other smoke tests never reach it, which is why it is filled here rather than in a shared
	// fixture.
	d := daemonEngine{App: a, workdir: wd, card: func() mcpserve.Card { return mcpserve.Card{Name: "api"} },
		handover: handover{at: newWhere(parent), rooms: newSideSessions(), minutes: newSideSessions()}}

	// No model here: the turn will fail at generation, and that is fine — what is being checked is
	// which sessions the daemon OPENED for this meeting, not what came back from either.
	_, _ = d.MeetingTurn(ctx, "m-1", "which store", "", "", false)

	room, note := d.handover.roomFor("m-1"), d.handover.minutesFor("m-1")
	switch {
	case room == "":
		t.Fatal("no speaking session was opened for the meeting")
	case note == "":
		t.Fatal("no minutes session was opened — the write-up has nowhere to go but the speaking one")
	case room == note:
		t.Fatalf("both halves of the turn share session %s: the drafts land in the context the "+
			"participant decides in, which is what two registers exist to prevent", room)
	}
	// And they are marked apart on the log, which is what the console filters on and the IDE names.
	// Read back through the same door the screens use, so what this pins is what they see.
	kids, err := d.ChildrenOf(ctx, string(parent))
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[session.SessionID]string{}
	for _, m := range kids {
		kinds[m.ID] = m.Origin
	}
	if kinds[room] != meeting.Origin {
		t.Errorf("the speaking session is stamped %q, not %q", kinds[room], meeting.Origin)
	}
	// And opening it ran NOTHING. The first shape seeded a prompt saying "nothing to do yet",
	// which cost two model calls per participant per meeting — the model answered the do-nothing
	// prompt with a minutes table of its own invention, and magi's step-budget nudge drew a second
	// one. Both landed in the context this session exists to keep clean. Measured live, then fixed.
	//
	// Counted rather than named: what must not happen is a TURN, and a turn is prompts and answers.
	evs, err := st.Read(ctx, note, 0)
	if err != nil {
		t.Fatal(err)
	}
	turns := 0
	for _, e := range evs {
		if e.Type == event.TypePromptSubmitted {
			turns++
		}
	}
	// One: the write-up this turn asked for. Two or more means the session was seeded as well.
	if turns != 1 {
		t.Errorf("the minutes session has %d prompts in it; opening it should run nothing and the "+
			"write-up should be its first turn", turns)
	}
	if kinds[note] != meeting.MinutesOrigin {
		t.Errorf("the minutes session is stamped %q, not %q — every screen that tells meeting "+
			"children apart reads this", kinds[note], meeting.MinutesOrigin)
	}
}

// sameAnswer is a provider that says one thing to everything, with no tool calls.
type sameAnswer string

func (t sameAnswer) StreamChat(ctx context.Context, _ port.ChatRequest) (<-chan port.ProviderEvent, error) {
	ch := make(chan port.ProviderEvent, 2)
	ch <- port.ProviderEvent{Type: port.ProviderText, Text: string(t)}
	ch <- port.ProviderEvent{Type: port.ProviderFinish, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

// A pass does not open a minutes session, and does not spend a turn on one.
//
// Measured over five live meetings: eight of the nine revisions that came back SHORTER than the
// document they were handed were passes — one cut 1263 characters to 611. A turn in which nobody
// said anything deleted half of what the room had agreed. Asking at all is the defect: a pass has
// nothing to add, so the only thing the call can do is damage the record.
func TestAPassDoesNotTouchTheMinutes(t *testing.T) {
	st, err := jsonl.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	// A model that passes on everything. PASS as the first word is what readUtterance reads.
	a := app.New(st, sameAnswer("PASS nothing in my workspace touches this"), builtin.NewRegistry(),
		bus.New(), nil, app.Config{Permission: "allow", Models: reg})
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	ctx := context.Background()
	wd := t.TempDir()
	parent, err := a.CreateSession(ctx, command.CreateSession{Workdir: wd,
		Model: session.ModelRef{Provider: "openai", Model: "m"},
		Actor: event.Actor{Kind: event.ActorUser, ID: "cli"}})
	if err != nil {
		t.Fatal(err)
	}
	d := daemonEngine{App: a, workdir: wd, card: func() mcpserve.Card { return mcpserve.Card{Name: "api"} },
		handover: handover{at: newWhere(parent), rooms: newSideSessions(), minutes: newSideSessions()}}

	const doc = "## Decided\n- the room agreed something"
	got, err := d.MeetingTurn(ctx, "m-1", "which store", "", doc, false)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Pass {
		t.Fatalf("the fake passed and the turn did not read it as one: %+v", got)
	}
	if got.Minutes != "" {
		t.Errorf("a pass answered with a document (%d chars); the convener would write it over "+
			"what the room agreed", len(got.Minutes))
	}
	if note := d.handover.minutesFor("m-1"); note != "" {
		t.Errorf("a pass opened a minutes session (%s) — the call is the defect, not what it "+
			"answers", note)
	}
}
