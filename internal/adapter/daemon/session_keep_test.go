package daemon

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
)

// **대화를 여는 것과 거기로 가는 것은 다른 일이다.**
//
// `session-new` 는 늘 둘을 같이 했다 — 콘솔의 단추는 그 둘을 뜻하니 맞다. 그런데 한 데몬이 대화를
// 여럿 섬기는 클라이언트에게는 앞의 것만 뜻이고, 옮기면 **방금까지 일하던 대화에 「컴패니언이
// 떠났다」가 적힌다**(2026-09-05: PowerPoint 창 둘이 서로를 그렇게 버렸다).
type keepEng struct {
	fakeEngine
	moved   int
	kept    int
	purpose string
	rows    []session.SessionMeta
}

func (e *keepEng) SessionsHere(context.Context) ([]session.SessionMeta, error) { return e.rows, nil }

func (e *keepEng) NewSession(context.Context) (session.SessionID, error) {
	e.moved++
	return "s-moved", nil
}

func (e *keepEng) NewSessionKeeping(_ context.Context, purpose string) (session.SessionID, error) {
	e.kept++
	e.purpose = purpose
	return "s-kept", nil
}

// **무엇을 위해 열었는지는 여는 쪽의 손잡이다.** `name` 으로 들어와 그대로 기록되고, `sessions` 가
// `for` 로 되돌려 준다 — 헬퍼가 다시 떠서 자기 묶음을 잊어도 「문서 X 의 대화」를 목록에서 집어
// 낼 수 있게(2026-09-06 엑셀 실측: 재기동마다 빈 대화가 새로 섰다).
func TestAConversationRemembersWhatItWasOpenedFor(t *testing.T) {
	e := &keepEng{}
	got := answerSessionNew(context.Background(), e, Request{Method: "session-new", Keep: true, Name: " wb-book-1 "})
	if got.Err != "" || e.purpose != "wb-book-1" {
		t.Fatalf("연 목적이 엔진에 안 갔다: %+v purpose=%q", got, e.purpose)
	}
	e.rows = []session.SessionMeta{{ID: "s-kept", For: "wb-book-1"}, {ID: "s-other"}}
	list := answerSessions(context.Background(), e, Request{Method: "sessions"})
	if len(list.Sessions) != 2 || list.Sessions[0].For != "wb-book-1" || list.Sessions[1].For != "" {
		t.Fatalf("목록이 목적을 안 되돌린다: %+v", list.Sessions)
	}
}

func TestOpeningAConversationNeedNotMoveTheCompanion(t *testing.T) {
	e := &keepEng{}
	got := answerSessionNew(context.Background(), e, Request{Method: "session-new", Keep: true})
	if got.Err != "" || got.Session != "s-kept" {
		t.Fatalf("옮기지 말라고 했는데 옮겼다: %+v (moved=%d kept=%d)", got, e.moved, e.kept)
	}
	if e.moved != 0 {
		t.Errorf("옮기는 길을 탔다: %d", e.moved)
	}
	// **안 적으면 여태 뜻 그대로다.** 옛 클라이언트와 콘솔의 단추가 그 길로 온다.
	got = answerSessionNew(context.Background(), e, Request{Method: "session-new"})
	if got.Session != "s-moved" {
		t.Errorf("옛 뜻이 바뀌었다: %+v", got)
	}
}

// 그 문을 못 여는 데몬에는 **사실대로 말한다** — 조용히 옮기면 이 인자가 있으나 마나다.
func TestADaemonThatCannotKeepSaysSo(t *testing.T) {
	e := &onlyMoves{}
	got := answerSessionNew(context.Background(), e, Request{Method: "session-new", Keep: true})
	if got.Err == "" {
		t.Fatalf("못 하는데 했다고 답했다: %+v", got)
	}
	if e.moved != 0 {
		t.Errorf("거절해야 하는데 옮겼다: %d", e.moved)
	}
}

type onlyMoves struct {
	fakeEngine
	moved int
}

func (e *onlyMoves) SessionsHere(context.Context) ([]session.SessionMeta, error) { return nil, nil }

func (e *onlyMoves) NewSession(context.Context) (session.SessionID, error) {
	e.moved++
	return "s-moved", nil
}
