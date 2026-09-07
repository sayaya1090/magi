package office

import (
	"testing"
	"time"
)

// **프로그램이 없으면 컴패니언도 없다.** 헬퍼가 마련한 컴패니언은 Office 를 다 꺼도 남아 있었고 사람은 그것을 좀비로
// 읽었다(2026-09-07). 프로그램 프로세스가 유예(IdleAfter)만큼 계속 없으면 shutdown 문을 두드리고 기록을 잊는다 —
// 다음 own 이 처음처럼 다시 마련한다. 잠깐 없는 것(껐다 바로 켬)은 유예가 덮고, 못 재는 OS 에서는 안 내린다.
func TestTheCompanionGoesDownWhenTheProgramIsGone(t *testing.T) {
	running, known := true, true
	var stopped []string
	api := &API{App: PPT, Work: NewOwnWork(), IdleAfter: 60 * time.Second,
		Running: func() (bool, bool) { return running, known },
		Stop:    func(socket string) error { stopped = append(stopped, socket); return nil },
	}
	t0 := time.Unix(1_700_000_000, 0)
	// 아직 마련하지 않았으면 볼 것이 없다.
	if api.idleTick(t0) {
		t.Fatal("마련한 컴패니언이 없는데 내렸다")
	}
	api.Work.Begin()
	api.Work.Done(OwnReport{Phase: OwnReady, Socket: "/sock", Life: "1@t0"})
	// 프로그램이 떠 있으면 아무것도 안 한다.
	if api.idleTick(t0) || api.idleTick(t0.Add(5*time.Minute)) {
		t.Fatal("프로그램이 떠 있는데 내렸다")
	}
	// 없어졌다 — 유예 안에는 안 내린다(껐다 바로 켜는 사람).
	running = false
	if api.idleTick(t0.Add(6*time.Minute)) || api.idleTick(t0.Add(6*time.Minute+30*time.Second)) {
		t.Fatal("유예가 안 끝났는데 내렸다")
	}
	// 그 사이 다시 켜지면 유예는 처음부터.
	running = true
	api.idleTick(t0.Add(6*time.Minute + 50*time.Second))
	running = false
	if api.idleTick(t0.Add(7*time.Minute + 20*time.Second)) {
		t.Fatal("다시 켜진 뒤의 유예를 처음부터 세지 않았다")
	}
	// 유예가 다 지나면 내리고 잊는다.
	if !api.idleTick(t0.Add(8*time.Minute + 30*time.Second)) {
		t.Fatal("유예가 지났는데 안 내렸다")
	}
	if len(stopped) != 1 || stopped[0] != "/sock" {
		t.Fatalf("shutdown 을 두드린 소켓: %v", stopped)
	}
	if api.Work.Now().Phase == OwnReady {
		t.Fatal("내린 뒤에도 마련했다고 기억한다 — 다음 own 이 죽은 소켓에 붙는다")
	}
	// 못 재는 OS(둘째 값 거짓)에서는 절대 안 내린다 — 모르는 것을 「없다」로 읽지 않는다.
	api.Work.Begin()
	api.Work.Done(OwnReport{Phase: OwnReady, Socket: "/sock2", Life: "2@t1"})
	known = false
	for i := 0; i < 20; i++ {
		if api.idleTick(t0.Add(time.Duration(10+i) * time.Minute)) {
			t.Fatal("못 재는 OS 에서 내렸다")
		}
	}
	if len(stopped) != 1 {
		t.Fatalf("두 번 내렸다: %v", stopped)
	}
}

// **Office 가 하나도 없으면 헬퍼도 끝낸다**(officeWatch). 사용자(2026-09-07): 「헬퍼랑 데몬은 각 오피스가 켜진 게
// 있을 때만 켜져 있고, 인스턴스가 없으면 종료되고」. 다음에 Office 를 켜면 COM 추가 기능이 다시 띄운다.
func TestTheHelperEndsWhenNoOfficeProgramIsLeft(t *testing.T) {
	gone, known := false, true
	w := &officeWatch{After: 60 * time.Second, Gone: func() (bool, bool) { return gone, known }}
	t0 := time.Unix(1_700_000_000, 0)

	// 하나라도 떠 있으면 아무 때나 거짓이다.
	if w.tick(t0) || w.tick(t0.Add(time.Hour)) {
		t.Fatal("Office 가 떠 있는데 끝내려 했다")
	}
	// 없어졌다 — 유예 안에는 안 끝낸다(껐다 바로 켜는 사람).
	gone = true
	if w.tick(t0.Add(2*time.Hour)) || w.tick(t0.Add(2*time.Hour+59*time.Second)) {
		t.Fatal("유예가 안 끝났는데 끝내려 했다")
	}
	// 그 사이 다시 켜지면 유예는 처음부터.
	gone = false
	w.tick(t0.Add(2*time.Hour + 30*time.Second))
	gone = true
	if w.tick(t0.Add(2*time.Hour + 40*time.Second)) {
		t.Fatal("다시 켜진 뒤의 유예를 처음부터 세지 않았다")
	}
	if !w.tick(t0.Add(3 * time.Hour)) {
		t.Fatal("유예가 지났는데 안 끝냈다")
	}
	// **못 재면 안 끝낸다** — 모르는 것을 「없다」로 읽으면 사람이 쓰는 헬퍼를 끈다.
	known = false
	w.since = time.Time{}
	for i := 0; i < 20; i++ {
		if w.tick(t0.Add(time.Duration(4+i) * time.Hour)) {
			t.Fatal("못 재는 자리에서 끝냈다")
		}
	}
}
