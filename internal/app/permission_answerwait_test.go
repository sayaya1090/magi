package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// A daemon can be asked, because a UI can attach to it — but the UI may also never come, or come
// and leave. So an interactive engine with an AnswerWait resolves by policy rather than standing in
// front of one question until somebody remembers it exists.
//
// The terminal keeps waiting forever (AnswerWait 0): the person is sitting in front of the prompt.
func TestAnAnsweredPromptBeatsTheDeadline(t *testing.T) {
	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"go build ./..."}`)}
	actor := event.Actor{Kind: event.ActorUser, ID: "u"}
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true, AnswerWait: 5 * time.Second})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	got := make(chan bool, 1)
	go func() { got <- a.requestPermission(context.Background(), sid, actor, tc, true, "") }()

	// Wait for the prompt to register, then answer it the way an attached UI would.
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := a.Waiting(sid); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the prompt never registered as pending — nothing could have answered it")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := a.RespondPermission(context.Background(), command.RespondPermission{
		SessionID: sid, CallID: "c1", Decision: "allow"}); err != nil {
		t.Fatalf("answering: %v", err)
	}
	select {
	case g := <-got:
		if !g {
			t.Error("an allowed prompt came back denied")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the answer did not reach the waiting prompt")
	}
	// And it is no longer pending, so a dashboard stops saying somebody is needed.
	if ask, ok := a.Waiting(sid); ok {
		t.Errorf("still reported as waiting on %+v after being answered", ask)
	}
}

// Nobody answers: the prompt must resolve by policy, and the transcript must say that is what
// happened. A decision taken by default reads exactly like one somebody made unless the log
// distinguishes them.
func TestAnUnansweredPromptResolvesByPolicyAndSaysSo(t *testing.T) {
	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"go build ./..."}`)}
	actor := event.Actor{Kind: event.ActorUser, ID: "u"}
	for _, c := range []struct {
		perm string
		want bool
	}{
		{"allow", true}, // the daemon's own default: nobody came, carry on
		{"ask", false},  // no answer is not an answer
	} {
		a, wd := newApp(t, &fakeLLM{}, Config{Permission: c.perm, Interactive: true, AnswerWait: 150 * time.Millisecond})
		sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
		done := make(chan bool, 1)
		start := time.Now()
		go func() { done <- a.requestPermission(context.Background(), sid, actor, tc, true, "") }()
		select {
		case g := <-done:
			if g != c.want {
				t.Errorf("perm=%q unanswered → %v, want %v", c.perm, g, c.want)
			}
			if time.Since(start) < 100*time.Millisecond {
				t.Errorf("perm=%q resolved in %s — it did not wait for anybody", c.perm, time.Since(start))
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("perm=%q BLOCKED past its AnswerWait", c.perm)
		}

		msgs, _, err := a.SessionState(context.Background(), sid)
		if err != nil {
			t.Fatal(err)
		}
		var all strings.Builder
		for _, m := range msgs {
			for _, p := range m.Parts {
				all.WriteString(p.Text)
			}
		}
		for _, want := range []string{"no UI answered", "bash", c.perm} {
			if !strings.Contains(all.String(), want) {
				t.Errorf("perm=%q: the transcript does not say %q: %q", c.perm, want, all.String())
			}
		}
	}
}

// With no AnswerWait the prompt waits, which is what a terminal needs. Proving a negative with a
// timer is weak, so this proves the pair: the same call resolves quickly WITH a deadline and is
// still pending after several times that long WITHOUT one.
func TestNoDeadlineMeansWaitForThePerson(t *testing.T) {
	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"go build ./..."}`)}
	actor := event.Actor{Kind: event.ActorUser, ID: "u"}
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- a.requestPermission(ctx, sid, actor, tc, true, "") }()
	select {
	case <-done:
		t.Fatal("a prompt with no deadline resolved on its own")
	case <-time.After(600 * time.Millisecond):
	}
	if _, ok := a.Waiting(sid); !ok {
		t.Error("the prompt is not reported as pending, so no viewer could show it")
	}
	cancel() // the run being torn down is the other way out
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("cancelling the turn did not release the prompt")
	}
}

// Waiting reports the OLDEST open prompt: with two pending, the one holding everything up is the
// one that has been waiting longest, and a card can only show one.
func TestWaitingReportsTheOldestPrompt(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	old := time.Now().Add(-time.Hour)
	a.mu.Lock()
	a.noteAskingLocked(sid, "new", Ask{Kind: "question", What: "which one?", Since: time.Now()})
	a.noteAskingLocked(sid, "old", Ask{Kind: "permission", What: "bash", Since: old})
	a.mu.Unlock()

	ask, ok := a.Waiting(sid)
	if !ok {
		t.Fatal("two prompts are open and Waiting reported none")
	}
	if ask.What != "bash" {
		t.Errorf("Waiting reported %q, want the older prompt (bash)", ask.What)
	}
	// A session with nothing pending says so, rather than making something up.
	if _, ok := a.Waiting("s_nothing_here"); ok {
		t.Error("an unknown session reported a pending prompt")
	}
}

// Two UIs on one daemon can be looking at the same prompt. Which one wins is a race magi cannot
// arbitrate; which one is TOLD it won is not.
//
// The channel holds one answer and the tool takes it, so a second delivery finds it full. That used
// to return nil — so the person whose choice was discarded watched the opposite happen with no
// reason to doubt their own screen. It is a browser and a terminal on one workspace, which is what
// this whole arrangement is for.
func TestASecondAnswerIsToldItWasTooLate(t *testing.T) {
	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"rm -rf build"}`)}
	actor := event.Actor{Kind: event.ActorUser, ID: "u"}
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})

	got := make(chan bool, 1)
	go func() { got <- a.requestPermission(context.Background(), sid, actor, tc, true, "") }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := a.Waiting(sid); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the prompt never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	first := a.RespondPermission(context.Background(), command.RespondPermission{
		SessionID: sid, CallID: "c1", Decision: "allow"})
	if first != nil {
		t.Fatalf("the first answer was refused: %v", first)
	}
	second := a.RespondPermission(context.Background(), command.RespondPermission{
		SessionID: sid, CallID: "c1", Decision: "deny"})
	if second == nil {
		t.Error("the second UI was told its 'deny' was applied, and the tool ran anyway")
	} else if !strings.Contains(second.Error(), "already") {
		t.Errorf("the refusal does not say what happened: %v", second)
	}

	select {
	case allowed := <-got:
		if !allowed {
			t.Error("the first answer did not decide it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the prompt never resolved")
	}

	// Same for a question: an answer nobody used must not report success.
	qsid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	a.mu.Lock()
	st := a.stateLocked(qsid)
	if st.questions == nil {
		st.questions = map[string]chan string{}
	}
	st.questions["q1"] = make(chan string, 1)
	a.mu.Unlock()
	if err := a.RespondQuestion(context.Background(), command.RespondQuestion{
		SessionID: qsid, CallID: "q1", Answer: "main"}); err != nil {
		t.Fatalf("the first answer was refused: %v", err)
	}
	if err := a.RespondQuestion(context.Background(), command.RespondQuestion{
		SessionID: qsid, CallID: "q1", Answer: "release"}); err == nil {
		t.Error("a second answer to one question reported success")
	}
}
