package main

import (
	"net/http/httptest"
	"testing"
)

// **마련하는 일은 컴패니언당 한 번, 묶는 것은 덱마다.**
//
// 둘째 창이 열리면 `/api/own` 은 이미 `Ready` 라는 이유로 아무것도 안 하고 돌려보냈다 — 그 창은
// 다 붙은 컴패니언 옆에서 「아직 안 붙었다」를 그렸다(2026-09-05: 사람이 "똑같은데?" 라고 세 번
// 물었다).
func TestEachDeckJoinsEvenWhenTheWorkIsDone(t *testing.T) {
	bs := NewBridges()
	opened := 0
	api := &API{Bridge: NewBridge(), Bridges: bs,
		Fresh: func(string) (string, error) { opened++; return "sess-b", nil }}

	api.joinDeck("deck-a", "/sock", "sess-a")
	if _, sid, _ := bs.For("deck-a").Bound(); sid != "sess-a" {
		t.Fatalf("첫 덱이 안 묶였다: %q", sid)
	}
	// 둘째 덱은 **자기 대화**를 받는다 — 같은 것에 묶으면 두 창이 한 줄이 된다.
	api.joinDeck("deck-b", "/sock", "sess-a")
	if _, sid, _ := bs.For("deck-b").Bound(); sid != "sess-b" {
		t.Fatalf("둘째 덱이 남의 대화에 묶였다: %q", sid)
	}
	if opened != 1 {
		t.Errorf("새 대화를 %d번 열었다", opened)
	}
	// **이미 묶여 있으면 안 건드린다.** 다시 묶으면 스트림이 끊겼다 이어지고 그 사이 사건이
	// 아무 화면에도 안 닿는다.
	api.joinDeck("deck-b", "/sock", "sess-a")
	if opened != 1 {
		t.Errorf("이미 묶인 덱에 또 열었다: %d", opened)
	}
	// 덱 이름이 없으면 할 일이 없다 — 열쇠 없는 자리는 이 함수가 손댈 곳이 아니다.
	api.joinDeck("", "/sock", "sess-a")
	if opened != 1 {
		t.Errorf("이름 없는 요청에 대화를 열었다: %d", opened)
	}
}

// **배선까지 문다.** 위 시험은 `joinDeck` 을 직접 부르므로, 핸들러가 그것을 안 불러도 초록이다 —
// 돌연변이가 그 구멍을 짚었다(2026-09-05). 둘째 창이 실제로 지나는 길은 `/api/own` 이다.
func TestTheOwnDoorJoinsTheAskingDeck(t *testing.T) {
	bs := NewBridges()
	work := NewOwnWork()
	work.Done(OwnReport{Phase: OwnReady, Socket: "/sock", Session: "sess-a", Tools: []string{"t"}})
	api := &API{
		Bridge: NewBridge(), Bridges: bs, Work: work,
		Own:   &OwnCompanion{ConfigDir: t.TempDir()},
		Ours:  func(string) bool { return true },
		Fresh: func(string) (string, error) { return "sess-b", nil },
	}
	w := httptest.NewRecorder()
	api.own(w, httptest.NewRequest("POST", "/api/own?deck=deck-z", nil))
	if _, sid, _ := bs.For("deck-z").Bound(); sid == "" {
		t.Fatal("이미 Ready 라는 이유로 묻는 덱을 안 묶었다 — 그 창은 「아직 안 붙었다」를 그린다")
	}
}
