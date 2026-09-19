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

// Nobody answers: the prompt resolves by policy, and the transcript says that is what happened. A
// decision taken by default reads exactly like one somebody made unless the log distinguishes them.
//
// Not in every mode. "ask" is the one that waits — see the test below — so the modes here are the
// two where a prompt exists for a reason other than the operator asking to be asked: auto's
// residue, and a guardrail forcing one over the top of allow.
func TestAnUnansweredPromptResolvesByPolicyAndSaysSo(t *testing.T) {
	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"go build ./..."}`)}
	actor := event.Actor{Kind: event.ActorUser, ID: "u"}
	for _, c := range []struct {
		perm string
		want bool
	}{
		{"allow", true}, // the daemon's own default: nobody came, carry on
		{"auto", false}, // edits were already approved; a command with nobody to vouch for it is not
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

// Which modes wait is read at the prompt, not frozen when the process started.
//
// The mode changes while a companion runs — Shift+Tab in an attached terminal, /permission, or
// SetPermission over the socket. Frozen at startup, one switched from auto to ask would go on
// resolving prompts by timer, which is the one thing ask exists to prevent; one switched the other
// way would hang on a prompt it had been told to give up on.
func TestTheWaitFollowsTheModeAsItStandsNow(t *testing.T) {
	a := New(nil, nil, nil, nil, nil, Config{AnswerWait: 3 * time.Minute, Permission: "auto"})
	// A session nobody has spoken in: attended, so the mode decides on its own.
	const sid = session.SessionID("s_attended")
	if got := a.answerBound(sid); got != 3*time.Minute {
		t.Errorf("auto on a daemon is unbounded: %v", got)
	}
	a.SetPermission("ask")
	if got := a.answerBound(sid); got != 0 {
		t.Errorf("switched to ask, a prompt is still answered by a timer after %v", got)
	}
	a.SetPermission("auto")
	if got := a.answerBound(sid); got != 3*time.Minute {
		t.Errorf("switched back to auto, the bound did not come back: %v", got)
	}

	// allow is bounded too, and that is not a contradiction: it does not prompt on its own, but a
	// guardrail can force one over the top of it, and hanging a companion whose operator asked for
	// "allow" on a question they never asked to be asked is the wrong way to be careful.
	a.SetPermission("allow")
	if got := a.answerBound(sid); got != 3*time.Minute {
		t.Errorf("a policy-forced prompt under allow would hang: %v", got)
	}

	// A terminal has nobody elsewhere, so no mode is bounded there.
	term := New(nil, nil, nil, nil, nil, Config{Permission: "auto"})
	if got := term.answerBound(sid); got != 0 {
		t.Errorf("a terminal bounded its own prompt after %v", got)
	}
}

// Under "ask", nobody answering is not an answer — the prompt waits.
//
// Resolving it by default after a few minutes answers the question on the operator's behalf, which
// is the one thing the mode exists to prevent. The companion sits in the fleet's waiting state,
// badged on the console and pushed to a phone, until somebody comes.
func TestUnderAskAPromptWaitsForAPerson(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{Permission: "ask", Interactive: true, AnswerWait: 100 * time.Millisecond})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	tc := &session.ToolCall{CallID: "c1", Name: "bash", Args: json.RawMessage(`{"command":"rm -rf /"}`)}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() {
		done <- a.requestPermission(ctx, sid, event.Actor{Kind: event.ActorUser, ID: "u"}, tc, true, "")
	}()

	select {
	case g := <-done:
		t.Fatalf("the prompt resolved itself to %v instead of waiting for a person", g)
	case <-time.After(400 * time.Millisecond): // four times the bound it no longer has
	}
	// Still asking, which is what a dashboard shows and what a person answers.
	if _, ok := a.Waiting(sid); !ok {
		t.Error("it stopped reporting that it needs somebody")
	}
	// And it is a wait, not a wedge: interrupting the turn lets go of it.
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelling the turn did not release the prompt")
	}
}

// **기한이 지났을 때 하는 말은, 아는 것만이어야 한다** — 「붙어 있는 UI 가 없다」는 이 경로가 확인하지
// 않는 사실이다.
//
// ⚠ 이 문구가 비싸게 틀렸다(실측 2026-09-19). 사람이 VS Code 패널 앞에 앉아 **이 질문이 그린 선택지
// 버튼 셋을 보면서** 3분 안에 답을 안 했다. 그때 도구가 모델에게 돌려준 것이 「no UI is attached」였고,
// 카운슬이 **그것을 근거로 추론했다** — 한 멤버는 「현재 환경에서는 UI가 없기 때문에 ask_user 도구가
// 실제 응답을 받을 수 없습니다」라며 요구사항을 충족으로 판정하고, 나머지 둘은 같은 증거로 미충족을
// 냈다. 닫는 호출이 그 모순을 적고 턴은 다시 돌았다. 「제때 아무도 안 답했다」가 「이 환경엔 UI 가
// 없다」로 바뀌었고, 실행은 뒤엣것을 믿고 움직였다.
//
// 그리고 이 기한은 **붙어 있는 UI 를 위해** 있는 것이다(`cmd/magi`: daemonAnswerWait — 「how long a
// daemon holds an AUTO-mode prompt open for an attached UI」). 그러니 그 절은 추측이었고, 하필 그
// 반대가 참이었다.
func TestTheExpiredPromptSaysOnlyWhatItKnows(t *testing.T) {
	a, wd := newApp(t, &fakeLLM{}, Config{AnswerWait: 30 * time.Millisecond, Permission: "auto", Interactive: true})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: wd})
	ask := a.askUserFn(context.Background(), session.Session{ID: sid}, 0,
		&session.ToolCall{CallID: "q_late", Name: "ask_user"})

	_, err := ask(port.Question{Text: "묻고 기다린다", Options: []string{"예", "아니오"}})
	if err == nil {
		t.Fatal("기한이 지났는데 오류 없이 돌아왔다 — 침묵이 답으로 읽힌다")
	}
	got := err.Error()
	// 아는 것: 기다렸고, 얼마나 기다렸고, 이제 스스로 정하라는 것.
	if !strings.Contains(got, "nobody answered within") {
		t.Errorf("무엇이 일어났는지 안 말한다: %q", got)
	}
	if !strings.Contains(got, "decide for yourself") {
		t.Errorf("모델이 다음에 무엇을 할지 안 말한다 — 침묵을 답으로 읽을 수 있다: %q", got)
	}
	// 모르는 것: 화면이 붙어 있는지. 이 경로에 그것을 보는 코드가 없다.
	if strings.Contains(got, "no UI") || strings.Contains(got, "UI is attached") {
		t.Errorf("확인하지 않은 사실을 단언한다 — 붙어 있는 화면 앞에서 이 문장이 카운슬의 결론을 "+
			"바꿨다: %q", got)
	}
}
