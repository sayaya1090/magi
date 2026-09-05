package main

import (
	"net/http/httptest"
	"testing"
)

// DESIGN §5.9.1 의 표 — 재기동 사건 넷, **열 하나가 시험 하나다.** 09-05 까지는 이 열이 하나도 없어서
// 각 칸을 사람이 실물에서 밟고서야 알았다. 넷 다 같은 리그로 재고, 무는 것은 「그 사건 뒤에 `settle`
// 이 무엇을 다시 하고 무엇을 안 하는가」다.

type restartRig struct {
	api    *API
	bolts  []string // 붙인 (소켓|주인)
	opened int
	life   string
}

func newRestartRig() *restartRig {
	r := &restartRig{life: "1@t0"}
	r.api = &API{
		Bridge: NewBridge(), Bridges: NewBridges(), Port: 3000,
		Bolt: func(socket, _, _ string) ([]string, error) {
			r.bolts = append(r.bolts, socket)
			return []string{"list_slides"}, nil
		},
		Fresh: func(string) (string, error) { r.opened++; return "s_fresh", nil },
	}
	return r
}

func (r *restartRig) ready() OwnReport {
	return OwnReport{Phase: OwnReady, Socket: "/sock", Session: "s_deck", Life: r.life}
}

// 열 1 — **창 껐다 켬.** 헬퍼도 데몬도 그대로다. 같은 덱 키로 다시 오면 아무것도 다시 하지 않는다 —
// 다시 붙이면 첫 등록이 떨어지고, 다시 묶으면 스트림이 끊겼다 이어진다.
func TestRestartColumnPaneReopen(t *testing.T) {
	r := newRestartRig()
	r.api.settle("pid-deck-A", r.ready())
	before := len(r.bolts)
	// 창이 닫혔다 열렸다 — 헬퍼 쪽 상태는 하나도 안 바뀐다.
	got := r.api.settle("pid-deck-A", r.ready())
	if len(r.bolts) != before {
		t.Errorf("창을 껐다 켰는데 다시 붙였다: %v", r.bolts)
	}
	if got.Session != "s_deck" || len(got.Tools) != 1 {
		t.Errorf("묶여 있던 것을 그대로 안 줬다: %+v", got)
	}
}

// 열 2 — **헬퍼 재기동.** 맵이 전부 빈다. 데몬은 옛 이름의 등록을 든 채다. 새 헬퍼는 그 덱을
// 처음 보는 것처럼 붙이고 묶는다 — 옛 등록은 detach-then-attach 가 정리한다(§5.0.1).
func TestRestartColumnHelperRestart(t *testing.T) {
	r := newRestartRig()
	r.api.settle("pid-deck-A", r.ready())
	// 헬퍼가 다시 떴다: Bridges 가 빈 새 것이다.
	r.api.Bridges = NewBridges()
	got := r.api.settle("pid-deck-A", r.ready())
	if len(r.bolts) != 2 {
		t.Errorf("새 헬퍼가 그 덱을 안 붙였다: %v", r.bolts)
	}
	if got.Session == "" {
		t.Errorf("새 헬퍼가 대화를 안 묶었다: %+v", got)
	}
}

// 열 3 — **데몬 재기동.** 소켓 경로는 그대로, 생애만 다르다. 등록과 스트림은 그 데몬과 같이 죽었으니
// 다시 붙이고 다시 묶되, **대화는 그대로 든다** — 디스크에 있고 사람이 보던 이력이다.
func TestRestartColumnDaemonRestart(t *testing.T) {
	r := newRestartRig()
	r.api.settle("pid-deck-A", r.ready())
	r.life = "2@t1"
	got := r.api.settle("pid-deck-A", r.ready())
	if len(r.bolts) != 2 {
		t.Errorf("다시 뜬 데몬에 안 붙였다: %v", r.bolts)
	}
	if got.Session != "s_deck" {
		t.Errorf("데몬이 다시 떴다고 대화를 갈았다: %q", got.Session)
	}
	// 그리고 `/api/own` 이 그 사건을 **보는** 자리는 하나다 — 생애 비교.
	work := NewOwnWork()
	work.Done(OwnReport{Phase: OwnReady, Socket: "/sock", Session: "s_deck", Life: "1@t0"})
	r.api.Work = work
	r.api.Own = &OwnCompanion{ConfigDir: t.TempDir()}
	r.api.LifeOf = func(string) string { return "2@t1" }
	r.api.own(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/own?deck=pid-deck-A", nil))
	if work.Now().Phase == OwnReady {
		t.Error("생애가 바뀌었는데 옛 마련을 Ready 로 들고 있다")
	}
}

// 열 4 — **PowerPoint 재기동.** 저장 안 한 덱은 태그가 소멸해 **새 덱 키**로 온다. 옛 키의 묶음은
// 임자가 아니고(붙어 있지 않다), 새 키는 자기 대화를 새로 받는다 — 옛 대화를 물려받지 않는다.
func TestRestartColumnPowerPointRestart(t *testing.T) {
	r := newRestartRig()
	r.api.settle("pid-deck-OLD", r.ready())
	// 새 덱 키. 옛 키는 아무 손도 없이 s_deck 을 들고 있다.
	got := r.api.settle("pid-deck-NEW", r.ready())
	if got.Session == "s_deck" {
		t.Errorf("새 덱이 죽은 덱의 대화를 물려받았다 — 두 창이 한 줄이 된다: %+v", got)
	}
	if r.opened != 1 {
		t.Errorf("새 덱에 자기 대화를 안 열었다: opened=%d", r.opened)
	}
}
