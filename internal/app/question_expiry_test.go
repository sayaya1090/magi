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
	"github.com/sayaya1090/magi/internal/port"
)

// A question that ran out of time is put away on every screen showing it.
//
// Measured 2026-10-27: after the three-minute wait the selection modal stayed open, and every
// button on it answered "보내지 못했습니다" — the daemon had already dropped the question
// ("not waiting for an answer … or the prompt has expired") but never said so. Only an ANSWER
// published question.answered, which is the event every client closes the modal on.
func TestAnExpiredQuestionTellsTheScreensToPutItAway(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{AnswerWait: 30 * time.Millisecond, Permission: "auto", Interactive: true})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	evs, stop, err := a.Subscribe(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	ask := a.askUserFn(context.Background(), session.Session{ID: sid}, 0,
		&session.ToolCall{CallID: "q_late", Name: "ask_user"})
	if _, err := ask(port.Question{Text: "묻고 기다린다", Options: []string{"예", "아니오"}}); err == nil {
		t.Fatal("the wait did not expire")
	}

	var asked string
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-evs:
			switch e.Type {
			case event.TypeQuestionRequested:
				var d event.QuestionRequestedData
				_ = json.Unmarshal(e.Data, &d)
				asked = d.CallID
			case event.TypeQuestionAnswered:
				var d event.QuestionAnsweredData
				_ = json.Unmarshal(e.Data, &d)
				if asked == "" || d.CallID != asked || !strings.HasPrefix(d.CallID, "q_late") {
					t.Fatalf("closed %q, but the open question was %q", d.CallID, asked)
				}
				// And the late button press is still refused: closing the modal is not reopening it.
				if err := a.RespondQuestion(context.Background(), command.RespondQuestion{
					SessionID: sid, CallID: asked, Answer: "예"}); err == nil {
					t.Error("an answer to an expired question reported success")
				}
				return
			}
		case <-deadline:
			t.Fatalf("the question expired (asked %q) and no screen was told to put it away", asked)
		}
	}
}
