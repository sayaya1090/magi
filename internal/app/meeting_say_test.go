package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// meetingApp is a companion that has already prepared for a meeting: one session, one model, and
// whatever the fake says next.
func meetingApp(t *testing.T, llm port.LLMProvider) (*App, session.SessionID) {
	t.Helper()
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 8000, Tools: true})
	a, dir := newApp(t, llm, Config{Permission: "allow", Models: reg})
	sid, err := a.CreateSession(context.Background(), command.CreateSession{
		Workdir: dir, Model: session.ModelRef{Provider: "openai", Model: "m"}})
	if err != nil {
		t.Fatal(err)
	}
	return a, sid
}

// A participant with no session cannot be asked to speak, and asking must cost nothing.
//
// The session is made by MeetingPrepare, so an empty one here means preparation failed or the room
// is holding a name it never opened a session for. Running the turn anyway would put a meeting
// prompt into whatever session id happened to be passed — including the empty one, which is not a
// session anybody is watching.
func TestAParticipantWithNoSessionIsNotAskedToSpeak(t *testing.T) {
	llm := &fakeLLM{}
	a, _ := meetingApp(t, llm)

	for _, child := range []session.SessionID{"", "   ", "\n\t"} {
		u, err := a.MeetingSayIn(context.Background(), child, "api", "the topic", "", false)
		if err == nil {
			t.Errorf("MeetingSayIn(%q) answered %+v instead of saying the participant has no session", child, u)
			continue
		}
		// The message is the point. Without the guard the turn goes on to the store, which refuses
		// an id it does not know — so the room still gets an error, and what it shows the person
		// is a sentence about a JSONL file rather than about a participant who never prepared.
		if !strings.Contains(err.Error(), "no session in the meeting") {
			t.Errorf("MeetingSayIn(%q) failed with %v, which is the store talking and not the meeting", child, err)
		}
	}
	llm.mu.Lock()
	defer llm.mu.Unlock()
	if llm.call != 0 {
		t.Errorf("the model was asked %d times for a participant that has no session", llm.call)
	}
}

// What the participant said comes back as its contribution, in the session it prepared in.
//
// Both halves matter: the transcript the room shows is built from what comes back, and the prompt
// has to land in the session that already holds this participant's reading — the whole reason the
// meeting stopped spawning a child per turn.
func TestWhatAParticipantSaysComesBackAsItsContribution(t *testing.T) {
	llm := &fakeLLM{steps: [][]port.ProviderEvent{
		textStep("**PASS** — my workspace only holds the billing job"),
	}}
	a, sid := meetingApp(t, llm)

	u, err := a.MeetingSayIn(context.Background(), sid, "api",
		"should the toolchain move", "design: it would cost a day", false)
	if err != nil {
		t.Fatal(err)
	}
	if u.Who != "api" {
		t.Errorf("the contribution is attributed to %q", u.Who)
	}
	if !u.Pass {
		t.Errorf("a dressed-up pass came back as a contribution: %+v", u)
	}
	if u.Text != "my workspace only holds the billing job" {
		t.Errorf("the reason came back as %q", u.Text)
	}

	// The turn happened where the participant had already read, and it was asked the meeting's
	// question with what had been said so far.
	evs, err := a.store.Read(context.Background(), sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var log strings.Builder
	for _, e := range evs {
		log.Write(e.Data)
	}
	for _, want := range []string{"should the toolchain move", "design: it would cost a day"} {
		if !strings.Contains(log.String(), want) {
			t.Errorf("the participant's own session never heard %q", want)
		}
	}
}

// activeLLM answers a turn, and reports what the console would have been told while it was doing it.
type activeLLM struct {
	fakeLLM
	a       **App
	duringT bool
}

func (f *activeLLM) StreamChat(ctx context.Context, r port.ChatRequest) (<-chan port.ProviderEvent, error) {
	if *f.a != nil {
		f.duringT = (*f.a).MeetingActive()
	}
	return f.fakeLLM.StreamChat(ctx, r)
}

// A meeting round is visible while it is being composed.
//
// Meeting turns deliberately never enter the run states, so Running() cannot see one. Without a
// second signal a daemon restarted itself in the middle of a contribution, and the console recorded
// that participant as having failed the round — the update landing on the one window where nobody
// was looking at a run.
func TestAMeetingRoundIsVisibleWhileItIsBeingComposed(t *testing.T) {
	var app *App
	llm := &activeLLM{a: &app, fakeLLM: fakeLLM{steps: [][]port.ProviderEvent{textStep("it would cost a day")}}}
	a, sid := meetingApp(t, llm)
	app = a

	if a.MeetingActive() {
		t.Fatal("a companion that is in no meeting reports a round in flight")
	}
	if _, err := a.MeetingSayIn(context.Background(), sid, "api", "the topic", "", true); err != nil {
		t.Fatal(err)
	}
	if !llm.duringT {
		t.Error("the round was invisible while it was being composed: an auto-update would have " +
			"restarted the daemon mid-contribution")
	}
	if a.MeetingActive() {
		t.Error("the round is still counted after it finished; the next idle check never comes true")
	}
}
