package office

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

// DESIGN §5.9.1 의 표 — 재기동 사건 넷, **열 하나가 시험 하나다.** 09-05 까지는 이 열이 하나도 없어서
// 각 칸을 사람이 실물에서 밟고서야 알았다. 넷 다 같은 리그로 재고, 무는 것은 「그 사건 뒤에 `settle`
// 이 무엇을 다시 하고 무엇을 안 하는가」다.

type restartRig struct {
	api    *API
	bolts  []string // 붙인 (소켓|주인)
	opened int
	life   string
	// known 은 데몬이 「그 문서 것」으로 적어 둔 대화 — `sessions` 의 `for`. 시험이 채운다: 헬퍼가
	// 열고 사람이 말을 걸어 디스크에 적힌 뒤의 진실이다. 비어 있으면 데몬은 모른다.
	known map[string]string
	fors  []string // Fresh 가 받은 문서 키 — 대화를 누구 것으로 열었나
}

func newRestartRig() *restartRig {
	r := &restartRig{life: "1@t0"}
	r.api = &API{App: Word,
		Bridge: NewBridge(), Bridges: NewBridges(), Port: 3000,
		Bolt: func(socket, _, _ string) ([]string, error) {
			r.bolts = append(r.bolts, socket)
			return []string{"list_paragraphs"}, nil
		},
		Fresh: func(_, deck string) (string, error) {
			r.opened++
			r.fors = append(r.fors, deck)
			return fmt.Sprintf("s_fresh%d", r.opened), nil
		},
		Resume: func(_, deck string) (string, bool) { sid, ok := r.known[deck]; return sid, ok },
	}
	r.known = map[string]string{}
	return r
}

func (r *restartRig) ready() OwnReport {
	return OwnReport{Phase: OwnReady, Socket: "/sock", Session: "s_deck", Life: r.life}
}

// 열 1 — **창 껐다 켬.** 헬퍼도 데몬도 그대로다. 같은 덱 키로 다시 오면 아무것도 다시 하지 않는다 —
// 다시 붙이면 첫 등록이 떨어지고, 다시 묶으면 스트림이 끊겼다 이어진다.
func TestRestartColumnPaneReopen(t *testing.T) {
	r := newRestartRig()
	r.api.settle("wb-deck-A", r.ready())
	before := len(r.bolts)
	// 창이 닫혔다 열렸다 — 헬퍼 쪽 상태는 하나도 안 바뀐다.
	got := r.api.settle("wb-deck-A", r.ready())
	if len(r.bolts) != before {
		t.Errorf("창을 껐다 켰는데 다시 붙였다: %v", r.bolts)
	}
	if got.Session != "s_fresh1" || len(got.Tools) != 1 {
		t.Errorf("묶여 있던 것을 그대로 안 줬다: %+v", got)
	}
}

// 열 2 — **헬퍼 재기동.** 맵이 전부 빈다. 데몬은 옛 이름의 등록을 든 채다. 새 헬퍼는 그 덱을
// 처음 보는 것처럼 붙이고 묶는다 — 옛 등록은 detach-then-attach 가 정리한다(§5.0.1).
func TestRestartColumnHelperRestart(t *testing.T) {
	r := newRestartRig()
	r.api.settle("wb-deck-A", r.ready())
	// 헬퍼가 다시 떴다: Bridges 가 빈 새 것이다.
	r.api.Bridges = NewBridges()
	got := r.api.settle("wb-deck-A", r.ready())
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
	r.api.settle("wb-deck-A", r.ready())
	r.life = "2@t1"
	got := r.api.settle("wb-deck-A", r.ready())
	if len(r.bolts) != 2 {
		t.Errorf("다시 뜬 데몬에 안 붙였다: %v", r.bolts)
	}
	// 옛 대화는 죽은 데몬과 같이 갔다(실측 2026-09-04: 옛 sid 로 보내면 502 no conversation).
	// 새 생애에는 새 대화를 열어 묶는다 — 사람이 /api/fresh 를 손으로 부르게 하지 않는다.
	if got.Session != "s_fresh2" {
		t.Errorf("데몬이 다시 떴는데 죽은 생애의 대화를 그대로 물었다: %q", got.Session)
	}
	if _, sid, life, _ := r.api.Bridges.For("wb-deck-A").BoundTo(); sid != "s_fresh2" || life != "2@t1" {
		t.Errorf("덱의 묶음이 새 생애·새 대화가 아니다: sid=%q life=%q", sid, life)
	}
	// 그리고 `/api/own` 이 그 사건을 **보는** 자리는 하나다 — 생애 비교.
	work := NewOwnWork()
	work.Done(OwnReport{Phase: OwnReady, Socket: "/sock", Session: "s_deck", Life: "1@t0"})
	r.api.Work = work
	r.api.Own = quietOwn(t)
	r.api.LifeOf = func(string) string { return "2@t1" }
	r.api.own(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/own?deck=wb-deck-A", nil))
	// own 이 띄운 마련 고루틴이 시험의 TempDir 에 적는다 — 끝나기 전에 시험이 나가면 TempDir 정리가
	// 「directory not empty」로 죽는다(CI -race 2026-09-06). 마련이 끝날 때까지 기다린다.
	for deadline := time.Now().Add(3 * time.Second); work.Now().Phase == OwnWorking && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	if work.Now().Phase == OwnReady {
		t.Error("생애가 바뀌었는데 옛 마련을 Ready 로 들고 있다")
	}
}

// 열 4 — **PowerPoint 재기동.** 저장 안 한 덱은 태그가 소멸해 **새 덱 키**로 온다. 옛 키의 묶음은
// 임자가 아니고(붙어 있지 않다), 새 키는 자기 대화를 새로 받는다 — 옛 대화를 물려받지 않는다.
func TestRestartColumnPowerPointRestart(t *testing.T) {
	r := newRestartRig()
	r.api.settle("wb-deck-OLD", r.ready())
	// 새 덱 키. 옛 키는 아무 손도 없이 s_deck 을 들고 있다.
	got := r.api.settle("wb-deck-NEW", r.ready())
	if got.Session == "s_fresh1" {
		t.Errorf("새 덱이 죽은 덱의 대화를 물려받았다 — 두 창이 한 줄이 된다: %+v", got)
	}
	if r.opened != 2 {
		t.Errorf("새 덱에 자기 대화를 안 열었다: opened=%d", r.opened)
	}
}

// 열 2 의 짝 — **헬퍼 재기동 뒤 대화는 되찾는다.** 실물(2026-09-06 엑셀): 헬퍼를 껐다 켜자 창은 되붙었는데
// 대화가 새로 서서 세 턴의 전사가 사라졌다. 데몬은 대화마다 「누구 것으로 열렸나」를 알고(`for`), 헬퍼는
// 새로 열기 전에 그것을 묻는다 — 기억을 파일에 남기지 않고 진실을 본다(DESIGN §5.9.2).
func TestRestartColumnHelperRestartFindsTheDocumentsConversation(t *testing.T) {
	r := newRestartRig()
	r.api.settle("wb-deck-A", r.ready())
	// 사람이 말을 걸어 데몬이 그 대화를 문서 것으로 적었다.
	r.known["wb-deck-A"] = "s_fresh1"
	r.api.Bridges = NewBridges()
	got := r.api.settle("wb-deck-A", r.ready())
	if got.Session != "s_fresh1" {
		t.Errorf("헬퍼가 다시 떴다고 문서의 대화를 버리고 새로 열었다: %q", got.Session)
	}
	if r.opened != 1 {
		t.Errorf("되찾을 수 있는데 새로 열었다: opened=%d", r.opened)
	}
	if len(r.bolts) != 2 {
		t.Errorf("되찾은 대화 몫으로 도구를 다시 안 붙였다: %v", r.bolts)
	}
	if _, sid, _, _ := r.api.Bridges.For("wb-deck-A").BoundTo(); sid != "s_fresh1" {
		t.Errorf("묶음이 되찾은 대화가 아니다: %q", sid)
	}
}

// 열 3 의 짝 — **데몬 재기동 뒤도 같은 길이다.** 말이 오간 대화는 디스크에 있어 목록에 서고, 새 생애의
// 데몬도 그것을 `for` 로 답한다. 옛 판은 「죽은 생애의 대화는 남의 것」이라 늘 새로 열었다 — 그건
// 아무도 말하지 않은 대화(디스크에 없는 것)에만 맞는 말이었다.
func TestRestartColumnDaemonRestartFindsTheDocumentsConversation(t *testing.T) {
	r := newRestartRig()
	r.api.settle("wb-deck-A", r.ready())
	r.known["wb-deck-A"] = "s_fresh1"
	r.life = "2@t1"
	got := r.api.settle("wb-deck-A", r.ready())
	if got.Session != "s_fresh1" || r.opened != 1 {
		t.Errorf("데몬이 다시 떴다고 디스크에 있는 문서의 대화를 버렸다: %q opened=%d", got.Session, r.opened)
	}
	if _, sid, life, _ := r.api.Bridges.For("wb-deck-A").BoundTo(); sid != "s_fresh1" || life != "2@t1" {
		t.Errorf("묶음이 (되찾은 대화, 새 생애)가 아니다: sid=%q life=%q", sid, life)
	}
}

// **대화는 문서 이름으로 열린다.** 되찾기의 열쇠가 이것이라, 안 적으면 다음 헬퍼는 영영 못 찾는다.
func TestAConversationIsOpenedInTheDocumentsName(t *testing.T) {
	r := newRestartRig()
	r.api.settle("wb-deck-A", r.ready())
	if len(r.fors) != 1 || r.fors[0] != "wb-deck-A" {
		t.Fatalf("대화를 문서 것으로 안 열었다: %v", r.fors)
	}
}

// **남의 문서의 대화는 안 빌린다.** 되찾기는 이 문서 것만 본다 — 열쇠가 문서 키다.
func TestAnotherDocumentsConversationIsNotBorrowed(t *testing.T) {
	r := newRestartRig()
	r.known["wb-deck-OLD"] = "s_theirs"
	got := r.api.settle("wb-deck-NEW", r.ready())
	if got.Session == "s_theirs" || r.opened != 1 {
		t.Errorf("다른 문서의 대화를 물려받았다: %q opened=%d", got.Session, r.opened)
	}
}
