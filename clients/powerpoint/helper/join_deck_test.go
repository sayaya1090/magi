package main

import (
	"net/http/httptest"
	"testing"
)

// **마련은 컴패니언당 한 번, 묶는 것은 덱마다.**
//
// 둘째 창이 열리면 `/api/own` 은 이미 `Ready` 라는 이유로 아무것도 안 하고 돌려보냈다 — 그 창은
// 다 붙은 컴패니언 옆에서 「아직 안 붙었다」를 그렸다(2026-09-05: 사람이 "똑같은데?" 라고 세 번
// 물었다). 이제 `settle` 이 폴마다 그 덱을 묶는다 — 멱등이라 이미 맞으면 아무것도 안 한다.
func TestEachDeckSettlesEvenWhenTheWorkIsDone(t *testing.T) {
	bs := NewBridges()
	opened := 0
	bolts := 0
	api := &API{
		Bridge: NewBridge(), Bridges: bs, Port: 3000,
		Bolt:  func(string, string, string) ([]string, error) { bolts++; return []string{"t"}, nil },
		Fresh: func(string) (string, error) { opened++; return "sess-b", nil },
	}
	ready := OwnReport{Phase: OwnReady, Socket: "/sock", Session: "sess-a", Life: "1@t0"}

	got := api.settle("deck-a", ready)
	if got.Session != "sess-a" || len(got.Tools) != 1 {
		t.Fatalf("첫 덱이 안 묶였다: %+v", got)
	}
	// 둘째 덱은 **자기 대화**를 받는다 — 같은 것에 묶으면 두 창이 한 줄이 된다.
	got = api.settle("deck-b", ready)
	if got.Session != "sess-b" {
		t.Fatalf("둘째 덱이 남의 대화에 묶였다: %q", got.Session)
	}
	if opened != 1 || bolts != 2 {
		t.Errorf("새 대화 %d번, 붙임 %d번 — 각각 1, 2 여야 한다", opened, bolts)
	}
	// **이미 맞으면 아무것도 안 한다.** 다시 붙이면 첫 등록이 떨어지고, 다시 묶으면 스트림이
	// 끊겼다 이어진다.
	api.settle("deck-a", ready)
	api.settle("deck-b", ready)
	if opened != 1 || bolts != 2 {
		t.Errorf("멱등이 아니다: 대화 %d, 붙임 %d", opened, bolts)
	}
}

// **데몬이 다시 뜨면 다시 붙인다.** 소켓 경로는 그대로인데 생애가 다르다 — 등록도 스트림도 그
// 데몬과 같이 죽었다. 이것이 표(DESIGN §5.9.1)의 「데몬 재기동」 행이고, 09-05 까지 안 고쳐져 있던
// 셋 중 하나다.
func TestADaemonRestartIsSeenByItsLife(t *testing.T) {
	bs := NewBridges()
	bolts := 0
	api := &API{
		Bridge: NewBridge(), Bridges: bs, Port: 3000,
		Bolt: func(string, string, string) ([]string, error) { bolts++; return []string{"t"}, nil },
	}
	first := OwnReport{Phase: OwnReady, Socket: "/sock", Session: "sess-a", Life: "1@t0"}
	api.settle("deck-a", first)
	api.settle("deck-a", first)
	if bolts != 1 {
		t.Fatalf("같은 생애에 두 번 붙였다: %d", bolts)
	}
	// 같은 소켓, 다른 생애 — 다시 뜬 데몬.
	reborn := OwnReport{Phase: OwnReady, Socket: "/sock", Session: "sess-new", Life: "2@t1"}
	got := api.settle("deck-a", reborn)
	if bolts != 2 {
		t.Fatalf("다시 뜬 데몬에 안 붙였다: %d", bolts)
	}
	// **대화는 그대로 든다** — 디스크에 있고, 사람이 보던 이력이다. 데몬의 새 「지금」으로 갈아타면
	// 이 창의 대화가 사라진 것처럼 보인다.
	if got.Session != "sess-a" {
		t.Errorf("데몬이 다시 떴다고 대화를 갈았다: %q", got.Session)
	}
}

// **배선까지 문다.** `settle` 을 직접 부르는 시험은 핸들러가 그것을 안 불러도 초록이다 — 하루에 두
// 번 겪은 모양이다(`imageBlock`·`joinDeck`, 2026-09-05). 둘째 창이 실제로 지나는 길은 `/api/own`.
func TestTheOwnDoorSettlesTheAskingDeck(t *testing.T) {
	bs := NewBridges()
	work := NewOwnWork()
	work.Done(OwnReport{Phase: OwnReady, Socket: "/sock", Session: "sess-a", Life: "1@t0"})
	api := &API{
		Bridge: NewBridge(), Bridges: bs, Work: work, Port: 3000,
		Own:    &OwnCompanion{ConfigDir: t.TempDir()},
		LifeOf: func(string) string { return "1@t0" },
		Bolt:   func(string, string, string) ([]string, error) { return []string{"t"}, nil },
		Fresh:  func(string) (string, error) { return "sess-b", nil },
	}
	w := httptest.NewRecorder()
	api.own(w, httptest.NewRequest("POST", "/api/own?deck=deck-z", nil))
	if _, sid, _ := bs.For("deck-z").Bound(); sid == "" {
		t.Fatalf("이미 Ready 라는 이유로 묻는 덱을 안 묶었다 — 그 창은 「아직 안 붙었다」를 그린다: %s", w.Body.String())
	}
	// 그리고 **생애가 바뀌면 마련부터 다시다.** 폴이 그것을 알아보는 자리가 이 핸들러 하나다.
	api.LifeOf = func(string) string { return "2@t1" }
	api.own(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/own?deck=deck-z", nil))
	if work.Now().Phase == OwnReady && work.Now().Life == "1@t0" {
		t.Error("데몬이 다시 떴는데 옛 마련을 그대로 들고 있다")
	}
}
