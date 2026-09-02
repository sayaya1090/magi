package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ownRig 는 `/api/own` 을 실물 소켓 없이 두드리는 자리.
type ownRig struct {
	api      *API
	mu       sync.Mutex
	attached []string
	spawned  int
}

func ownFixture(t *testing.T, tweak func(*API, *ownRig)) *ownRig {
	t.Helper()
	// 기다리는 시간을 0 으로 — 이 묶음이 재는 것은 **몇 번 다시 보는가**이지 몇 밀리초냐가 아니다.
	was := chatWait
	chatWait = 0
	t.Cleanup(func() { chatWait = was })
	cfg := t.TempDir()
	rig := &ownRig{}
	yes := true
	api := &API{
		Bridge:      NewBridge(),
		Attachments: NewAttachments(),
		ConfigDir:   cfg,
		Port:        3000,
		Work:        NewOwnWork(),
		Own: &OwnCompanion{
			ConfigDir: cfg,
			Binary:    "magi",
			Alive:     func(string) bool { return true },
			Spawn: func(string, string, []string) error {
				rig.mu.Lock()
				rig.spawned++
				rig.mu.Unlock()
				return nil
			},
		},
		ReadFleet: func(string) ([]Companion, error) {
			return []Companion{{
				Socket: DeckSocket(cfg), Workdir: DeckSpace(cfg), Session: "s_deck",
				Live: true, ToolServers: &yes, Transcript: &yes,
			}}, nil
		},
		// 진짜 부착은 Attachments 에 기록을 남기지만 이 픽스처는 Bolt 를 가로채므로 남는 것이
		// 없다. 「우리 것이 그대로인가」를 여기서 참으로 두고, 아니라고 답하는 갈래는 그 시험이
		// 따로 채운다.
		Ours: func(string) bool { return true },
		Bolt: func(socket, _, _ string) ([]string, error) {
			rig.mu.Lock()
			rig.attached = append(rig.attached, socket)
			rig.mu.Unlock()
			return []string{"ppt_read_slide"}, nil
		},
	}
	if tweak != nil {
		tweak(api, rig)
	}
	rig.api = api
	return rig
}

// poke 는 한 번 두드리고 그 답을 읽는다. **기다리지 않는다** — 즉시 오는 것이 계약이다.
func (r *ownRig) poke(t *testing.T) OwnReport {
	t.Helper()
	w := httptest.NewRecorder()
	r.api.own(w, httptest.NewRequest(http.MethodPost, "/api/own", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("두드리기 자체가 실패했다(%d): %s", w.Code, w.Body.String())
	}
	var got OwnReport
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("답이 OwnReport 가 아니다: %s", w.Body.String())
	}
	return got
}

// settle 은 뒤에서 도는 일이 끝날 때까지 기다린다(시험 전용).
func (r *ownRig) settle(t *testing.T) OwnReport {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		now := r.api.Work.Now()
		if now.Phase == OwnReady || now.Phase == OwnFailed {
			return now
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("일이 안 끝난다: %+v", r.api.Work.Now())
	return OwnReport{}
}

// **답이 즉시 온다.** 데몬 냉시동을 요청 안에서 기다리면 판이 멎고, 멎은 화면은 고장으로 읽힌다 —
// 실물에서 120초 요청이 끊긴 자리다(2026-09-02).
func TestOwnAnswersAtOnceAndWorksBehind(t *testing.T) {
	release := make(chan struct{})
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		up := false
		a.Own.Alive = func(string) bool { return up }
		a.Own.Spawn = func(string, string, []string) error {
			<-release // 냉시동이 오래 걸리는 흉내
			up = true
			return nil
		}
	})
	done := make(chan OwnReport, 1)
	go func() { done <- rig.poke(t) }()
	select {
	case got := <-done:
		if got.Phase != OwnWorking {
			t.Fatalf("일하는 중이라고 안 한다: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("데몬을 기다리느라 답이 안 온다 — 판이 멎는다")
	}
	close(release)
	if got := rig.settle(t); got.Phase != OwnReady {
		t.Fatalf("끝내 못 붙었다: %+v", got)
	}
}

// 되는 날 — **고르는 화면 없이** 붙는다.
func TestOwnAttachesWithoutAPicker(t *testing.T) {
	rig := ownFixture(t, nil)
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnReady {
		t.Fatalf("붙어야 하는데: %+v", got)
	}
	if len(rig.attached) != 1 {
		t.Fatalf("한 번 붙어야 하는데 %d 번: %v", len(rig.attached), rig.attached)
	}
	if got.Session != "s_deck" {
		t.Fatalf("세션을 명단에서 안 가져왔다: %q", got.Session)
	}
	if got.Workdir == "" || got.Socket == "" || len(got.Tools) == 0 {
		t.Fatalf("자리와 도구를 안 실었다: %+v", got)
	}
}

// **덱을 둘 열어도 데몬은 하나.**
//
// 판이 둘이면 이 자리도 둘에서 두드려진다. 각자 띄우면 둘이 한 워크스페이스를 두고 다투고,
// 등록의 임자를 헬퍼 하나로 둔 규칙(§5.0.1)이 무의미해진다.
func TestTwoPanesDoNotStartTwoCompanions(t *testing.T) {
	release := make(chan struct{})
	rig := ownFixture(t, func(a *API, r *ownRig) {
		up := false
		a.Own.Alive = func(string) bool { return up }
		a.Own.Spawn = func(string, string, []string) error {
			r.mu.Lock()
			r.spawned++
			r.mu.Unlock()
			<-release
			up = true
			return nil
		}
	})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); rig.poke(t) }()
	}
	wg.Wait()
	close(release)
	rig.settle(t)

	rig.mu.Lock()
	spawned, attached := rig.spawned, len(rig.attached)
	rig.mu.Unlock()
	if spawned != 1 {
		t.Fatalf("데몬을 %d 번 띄웠다 — 한 워크스페이스를 두고 다툰다", spawned)
	}
	if attached != 1 {
		t.Fatalf("도구를 %d 번 붙였다 — 재부착이 첫 등록을 떨어뜨린다", attached)
	}
}

// 이미 다 됐으면 **다시 안 붙는다.** 붙은 것을 다시 붙이면 첫 등록이 떨어진다(§5.0.1).
func TestPokingAgainAfterReadyDoesNotReattach(t *testing.T) {
	rig := ownFixture(t, nil)
	rig.poke(t)
	rig.settle(t)
	got := rig.poke(t)
	if got.Phase != OwnReady {
		t.Fatalf("이미 됐는데 다른 답을 준다: %+v", got)
	}
	if len(rig.attached) != 1 {
		t.Fatalf("다시 붙었다: %v", rig.attached)
	}
}

// **「이미 있었다」와 「방금 마련했다」는 다른 소식이다.**
func TestOwnSaysWhetherItHadToStartTheCompanion(t *testing.T) {
	rig := ownFixture(t, nil)
	rig.poke(t)
	if got := rig.settle(t); got.Started {
		t.Fatalf("안 띄웠는데 띄웠다고 한다: %+v", got)
	}

	up := false
	rig2 := ownFixture(t, func(a *API, r *ownRig) {
		a.Own.Alive = func(string) bool { return up }
		a.Own.Spawn = func(string, string, []string) error {
			r.mu.Lock()
			r.spawned++
			r.mu.Unlock()
			up = true
			return nil
		}
	})
	rig2.poke(t)
	if got := rig2.settle(t); !got.Started {
		t.Fatalf("우리가 띄웠는데 안 적는다: %+v", got)
	}
}

// magi 를 못 찾으면 — **어디를 봤는지와 로그 자리를 준다.**
func TestOwnCarriesWhereItLookedWhenMagiIsMissing(t *testing.T) {
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		a.Own.Binary = ""
		a.Own.Self = "/opt/app/magi-ppt"
		a.Own.Exists = func(string) bool { return false }
		a.Own.Look = func(string) (string, error) { return "", errors.New("no") }
		a.Own.Alive = func(string) bool { return false }
	})
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnFailed {
		t.Fatalf("실패인데: %+v", got)
	}
	if !strings.Contains(got.Why, "PATH") {
		t.Fatalf("본 자리를 안 알려 준다: %v", got.Why)
	}
	if got.Log == "" {
		t.Fatalf("로그 자리를 안 준다: %+v", got)
	}
	if len(rig.attached) != 0 {
		t.Fatal("띄우지도 못했는데 붙이러 갔다")
	}
}

// 떴지만 **도구를 못 받는 빌드**면 — 그 사유를 그대로 전한다.
func TestOwnRelaysWhyACompanionCannotBeChosen(t *testing.T) {
	no := false
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		cfg := a.ConfigDir
		a.ReadFleet = func(string) ([]Companion, error) {
			return []Companion{{
				Socket: DeckSocket(cfg), Workdir: DeckSpace(cfg), Live: true, ToolServers: &no,
			}}, nil
		}
	})
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnFailed || !strings.Contains(got.Why, "도구 서버") {
		t.Fatalf("사유를 그대로 안 전한다: %+v", got)
	}
	if len(rig.attached) != 0 {
		t.Fatal("못 고르는 컴패니언에 붙이러 갔다")
	}
}

// 섰다고 확인한 자리가 **명단에 없으면** 조용히 넘어가지 않는다.
func TestOwnRefusesWhenItsOwnSocketIsNotInTheFleet(t *testing.T) {
	yes := true
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		a.ReadFleet = func(string) ([]Companion, error) {
			return []Companion{{Socket: "/somewhere/else.sock", Live: true, ToolServers: &yes}}, nil
		}
	})
	rig.poke(t)
	if got := rig.settle(t); got.Phase != OwnFailed || !strings.Contains(got.Why, "명단에 그 자리가 없습니다") {
		t.Fatalf("무슨 일인지 안 적는다: %+v", got)
	}
}

// 붙기는 했고 **대화만 못 열었을 때** — 둘을 한 칸으로 합치지 않는다.
//
// 도구는 도는데 채팅창만 비는 것과, 아무것도 안 되는 것은 등급이 다르다(§5.0.5).
func TestOwnSeparatesAFailedChatFromAFailedAttach(t *testing.T) {
	yes := true
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		cfg := a.ConfigDir
		a.ReadFleet = func(string) ([]Companion, error) {
			return []Companion{{
				Socket: DeckSocket(cfg), Workdir: DeckSpace(cfg), Session: "",
				Live: true, ToolServers: &yes, Transcript: &yes,
			}}, nil
		}
	})
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnReady {
		t.Fatalf("도구는 붙었는데 통째로 실패로 답했다: %+v", got)
	}
	if len(got.Tools) == 0 {
		t.Fatalf("붙은 도구를 안 실었다: %+v", got)
	}
}

// 다시 해 볼 때 **옛 실패 사유를 안 들고 있는다.**
func TestARetryDoesNotShowTheOldFailure(t *testing.T) {
	fail := true
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		a.Own.Alive = func(string) bool { return !fail }
	})
	rig.poke(t)
	if got := rig.settle(t); got.Phase != OwnFailed {
		t.Fatalf("실패해야 하는데: %+v", got)
	}
	fail = false
	rig.api.Work.Forget()
	got := rig.poke(t)
	if got.Why != "" {
		t.Fatalf("다시 해 보는 중인데 옛 사유가 남아 있다: %+v", got)
	}
	if got := rig.settle(t); got.Phase != OwnReady {
		t.Fatalf("이번엔 돼야 하는데: %+v", got)
	}
}

// **안 돌아오는 일에 사람이 갇히지 않는다.**
//
// `doing` 을 내리는 것은 `Done` 하나뿐이라, 일이 어딘가에서 안 돌아오면 그 깃발이 영영 참으로
// 남는다. 그러면 헬퍼가 사는 내내 **모든 작업창이** 「준비하는 중」을 보고, 파워포인트를 다시
// 켜도 헬퍼는 그대로라 안 낫는다. 리뷰가 짚은 블로커다(2026-09-02).
func TestAJobThatNeverReturnsDoesNotTrapEveryonePane(t *testing.T) {
	stuck := make(chan struct{})
	defer close(stuck)
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		a.Own.Alive = func(string) bool { <-stuck; return true } // 영영 안 돌아온다
	})
	if got := rig.poke(t); got.Phase != OwnWorking {
		t.Fatalf("첫 두드림이 일하는 중이 아니다: %+v", got)
	}
	// 시계를 앞으로 돌린다 — 실물에서 3분을 기다리는 시험은 아무도 안 돌린다.
	rig.api.Work.now = func() time.Time { return time.Now().Add(stuckAfter + time.Second) }

	// 이번엔 되는 손으로 갈아 끼우고 다시 두드린다.
	rig.api.Own.Alive = func(string) bool { return true }
	rig.poke(t)
	if got := rig.settle(t); got.Phase != OwnReady {
		t.Fatalf("걸린 일을 넘겨받지 못했다 — 사람이 갇힌다: %+v", got)
	}
}

// 걸리지 **않은** 일은 넘겨받지 않는다 — 그러면 데몬이 둘 뜬다.
func TestAJobStillRunningIsNotTakenOver(t *testing.T) {
	release := make(chan struct{})
	rig := ownFixture(t, func(a *API, r *ownRig) {
		up := false
		a.Own.Alive = func(string) bool { return up }
		a.Own.Spawn = func(string, string, []string) error {
			r.mu.Lock()
			r.spawned++
			r.mu.Unlock()
			<-release
			up = true
			return nil
		}
	})
	rig.poke(t)
	for i := 0; i < 5; i++ {
		if got := rig.poke(t); got.Phase != OwnWorking {
			t.Fatalf("돌고 있는 일을 다른 답으로 덮었다: %+v", got)
		}
	}
	close(release)
	rig.settle(t)
	rig.mu.Lock()
	defer rig.mu.Unlock()
	if rig.spawned != 1 {
		t.Fatalf("데몬을 %d 번 띄웠다", rig.spawned)
	}
}

// **`Ready` 가 굳지 않는다.**
//
// `Begin` 은 이미 `Ready` 면 새 일을 안 시작한다. 그런데 그 사이 데몬이 죽으면 그 빗장이 다시
// 마련하는 길까지 막아, 작업창은 「대화 연결됨」인데 덱 도구는 하나도 없고 돌아갈 길도 없다.
func TestADeadCompanionIsProvisionedAgain(t *testing.T) {
	ours := true
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		a.Ours = func(string) bool { return ours }
	})
	rig.poke(t)
	if got := rig.settle(t); got.Phase != OwnReady {
		t.Fatalf("먼저 붙어야 한다: %+v", got)
	}
	// 그대로면 다시 안 붙는다 — 재부착은 첫 등록을 떨어뜨린다.
	rig.poke(t)
	if len(rig.attached) != 1 {
		t.Fatalf("멀쩡한데 다시 붙었다: %v", rig.attached)
	}
	// 죽으면 다시 마련한다.
	ours = false
	rig.poke(t)
	rig.settle(t)
	if len(rig.attached) != 2 {
		t.Fatalf("죽었는데 다시 안 붙었다 — 도구 없는 「연결됨」에 갇힌다: %v", rig.attached)
	}
}

// **패닉이 나도 깃발은 내려간다.** 안 내려가면 헬퍼가 사는 내내 모두가 「준비하는 중」이다.
func TestAPanicWhileProvisioningDoesNotTrapThePane(t *testing.T) {
	boom := true
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		a.Own.Alive = func(string) bool {
			if boom {
				panic("내부 오류")
			}
			return true
		}
	})
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnFailed {
		t.Fatalf("패닉이 났는데 실패로 안 적는다: %+v", got)
	}
	if !strings.Contains(got.Why, "골라 주세요") {
		t.Fatalf("갈 곳을 안 알려 준다: %v", got.Why)
	}
	// 그리고 다시 해 볼 수 있어야 한다.
	boom = false
	rig.poke(t)
	if got := rig.settle(t); got.Phase != OwnReady {
		t.Fatalf("패닉 뒤에 다시 못 한다: %+v", got)
	}
}

// **도구가 하나도 안 붙었으면 「준비됐습니다」가 아니다.**
//
// 붙었다는 증거는 ack 가 아니라 도구 이름이다(§5.0.1). 이름이 없는데 `ready` 로 답하면 작업창은
// 「준비됐습니다 — 도구 0 개」를 적고, 그 문장이 이 저장소가 최악이라고 적은 그 모양이다.
func TestNoToolsIsNotReady(t *testing.T) {
	rig := ownFixture(t, func(a *API, r *ownRig) {
		a.Bolt = func(socket, _, _ string) ([]string, error) {
			r.mu.Lock()
			r.attached = append(r.attached, socket)
			r.mu.Unlock()
			return nil, nil
		}
	})
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnFailed {
		t.Fatalf("도구 0개인데 준비됐다고 한다: %+v", got)
	}
	if !strings.Contains(got.Why, "도구가 하나도") {
		t.Fatalf("무슨 일인지 안 적는다: %v", got.Why)
	}
}

// 띄운 뒤에 명단을 못 읽어도 **방금 띄운 사실과 자리는 싣는다.**
//
// 그 둘이 사람이 유일하게 할 수 있는 일이다: 지금 그 워크스페이스에 데몬이 하나 돌고 있다.
func TestAFleetFailureAfterASpawnStillSaysWhatItStarted(t *testing.T) {
	up := false
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		a.Own.Alive = func(string) bool { return up }
		a.Own.Spawn = func(string, string, []string) error { up = true; return nil }
		a.ReadFleet = func(string) ([]Companion, error) { return nil, errors.New("명단을 못 훑었습니다") }
	})
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnFailed {
		t.Fatalf("실패여야 한다: %+v", got)
	}
	if !got.Started {
		t.Fatal("방금 띄워 놓고 안 띄웠다고 적는다 — 사람은 도는 데몬이 있는 줄 모른다")
	}
	if got.Workdir == "" {
		t.Fatalf("어디에 띄웠는지 안 알려 준다: %+v", got)
	}
}

// **대화 이름이 아직 없으면 한 번 더 물어본다.**
//
// 실물에서 본 것이다(2026-09-02). 데몬은 소켓에 선 다음에 자기 기록을 쓰므로, 그 틈에 명단을
// 읽으면 세션 칸이 비어 있다. 앞 판본은 그 순간의 답을 그대로 `Ready` 로 굳혔고 — 도구 28개는
// 멀쩡히 붙어 있는데 사람이 말을 걸면 「아직 대화가 없습니다」가 돌아왔다. 작업창에는
// 「준비됐습니다」라고 적혀 있었고, 되돌릴 길은 헬퍼를 죽이는 것뿐이었다.
func TestAChatNameThatArrivesLateIsStillPickedUp(t *testing.T) {
	reads := 0
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		cfg := a.ConfigDir
		yes := true
		a.ReadFleet = func(string) ([]Companion, error) {
			reads++
			// 처음 세 번은 기록에 세션이 아직 없다.
			sid := ""
			if reads > 3 {
				sid = "s_late"
			}
			return []Companion{{
				Socket: DeckSocket(cfg), Workdir: DeckSpace(cfg), Session: sid,
				Live: true, ToolServers: &yes, Transcript: &yes,
			}}, nil
		}
	})
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnReady {
		t.Fatalf("붙어야 한다: %+v", got)
	}
	if got.Session != "s_late" {
		t.Fatalf("늦게 온 대화 이름을 못 잡았다 — 사람이 말을 걸면 「대화가 없습니다」다: %+v", got)
	}
	if got.Chat != "" {
		t.Fatalf("대화가 열렸는데 못 열었다고 적는다: %q", got.Chat)
	}
	if len(rig.attached) != 1 {
		t.Fatalf("다시 보느라 도구를 여러 번 붙였다: %v", rig.attached)
	}
}

// 끝내 안 생겨도 **굳지 않는다** — 다음 물음에 다시 잡는다.
//
// 「이 빌드는 전사를 못 준다」와 「아직 안 생겼다」는 다른 말인데, 앞 판본은 둘을 같은 자리에
// 적고 영영 그대로 뒀다.
func TestAChatThatAppearsAfterReadyIsPickedUpOnTheNextPoke(t *testing.T) {
	sid := ""
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		cfg := a.ConfigDir
		yes := true
		a.ReadFleet = func(string) ([]Companion, error) {
			return []Companion{{
				Socket: DeckSocket(cfg), Workdir: DeckSpace(cfg), Session: sid,
				Live: true, ToolServers: &yes, Transcript: &yes,
			}}, nil
		}
	})
	rig.poke(t)
	got := rig.settle(t)
	if got.Phase != OwnReady {
		t.Fatalf("도구는 붙어야 한다: %+v", got)
	}
	if got.Chat == "" {
		t.Fatal("대화를 못 열었는데 그 사실을 안 적는다")
	}
	// 이제 데몬이 기록을 마저 썼다.
	sid = "s_now"
	after := rig.poke(t)
	if after.Session != "s_now" {
		t.Fatalf("생긴 대화를 안 잡는다 — 사람은 영영 말을 못 건다: %+v", after)
	}
	if after.Chat != "" {
		t.Fatalf("열렸는데 못 열었다고 적는다: %q", after.Chat)
	}
	// **도구를 다시 붙이지는 않는다** — 재부착은 첫 등록을 떨어뜨린다(§5.0.1).
	if len(rig.attached) != 1 {
		t.Fatalf("대화를 고치면서 도구를 다시 붙였다: %v", rig.attached)
	}
}

// ── 새 대화 ─────────────────────────────────────────────────────────────────
//
// 파워포인트 컴패니언은 워크스페이스가 하나라 대화도 하나이고, 그 하나가 **영원히 쌓인다.**
// 실물에서 봤다(2026-09-02): 한 번 헤맨 대화가 그 다음 부탁까지 끌고 가서, 사람이 19번 장을
// 보고 있는데 모델이 8번 장에 정렬을 걸고 6~17번을 헤맸다.
//
// 채팅을 쓰는 사람은 누구나 「새 대화」를 안다. PC 를 잘 다루지 못하는 사람에게는 **그것이
// 유일하게 아는 복구 수단**이다.
func freshRig(t *testing.T, tweak func(*API)) *ownRig {
	t.Helper()
	rig := ownFixture(t, func(a *API, _ *ownRig) {
		a.Fresh = func(string) (string, error) { return "s_fresh", nil }
		if tweak != nil {
			tweak(a)
		}
	})
	rig.poke(t)
	rig.settle(t)
	return rig
}

func (r *ownRig) askFresh(t *testing.T) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	r.api.fresh(w, httptest.NewRequest(http.MethodPost, "/api/fresh", nil))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("답이 JSON 이 아니다(%d): %s", w.Code, w.Body.String())
	}
	return w.Code, body
}

func TestANewConversationMovesTheWindowToIt(t *testing.T) {
	rig := freshRig(t, nil)
	code, body := rig.askFresh(t)
	if code != http.StatusOK {
		t.Fatalf("새 대화가 실패했다(%d): %v", code, body)
	}
	if body["session"] != "s_fresh" {
		t.Fatalf("새 대화 이름을 안 준다: %v", body)
	}
	// **창도 그 이름으로 옮겨 앉아야 한다.** 안 그러면 새 대화의 이벤트가 전부 남의 것으로
	// 걸러져서, 사람은 「새 대화」를 눌렀는데 아무 말도 안 보이는 화면을 본다.
	_, sid, _ := rig.api.Bridge.Bound()
	if sid != "s_fresh" {
		t.Fatalf("창이 옛 대화에 그대로 앉아 있다: %q", sid)
	}
	// 마련해 둔 기록도 새 이름이어야 한다 — 안 그러면 다음 물음이 옛 이름을 도로 물린다.
	if got := rig.api.Work.Now(); got.Session != "s_fresh" {
		t.Fatalf("기록이 옛 이름을 들고 있다: %+v", got)
	}
}

// **덱은 안 건드린다.** 「새 대화」가 슬라이드를 지우는 것으로 읽히면 아무도 못 누른다.
func TestANewConversationSaysTheDeckIsUntouched(t *testing.T) {
	rig := freshRig(t, nil)
	_, body := rig.askFresh(t)
	note, _ := body["note"].(string)
	if !strings.Contains(note, "슬라이드는 그대로") {
		t.Fatalf("덱이 무사하다는 말을 안 한다: %q", note)
	}
	// 도형·장에 손대는 길이 이 핸들러에 없다는 것은 Bolt 호출 수로 잰다.
	if len(rig.attached) != 1 {
		t.Fatalf("새 대화가 도구를 다시 붙였다: %v", rig.attached)
	}
}

// 안 붙어 있으면 **열 자리가 없다고 말한다** — 조용히 성공으로 답하지 않는다.
func TestANewConversationNeedsSomethingToOpenItOn(t *testing.T) {
	api := &API{Bridge: NewBridge(), Attachments: NewAttachments(), ConfigDir: t.TempDir(), Work: NewOwnWork()}
	w := httptest.NewRecorder()
	api.fresh(w, httptest.NewRequest(http.MethodPost, "/api/fresh", nil))
	if w.Code != http.StatusConflict {
		t.Fatalf("붙지도 않았는데 %d: %s", w.Code, w.Body.String())
	}
}

// 데몬이 거절하면 **그 사유를 그대로** 전한다.
func TestANewConversationCarriesWhyItFailed(t *testing.T) {
	rig := freshRig(t, func(a *API) {
		a.Fresh = func(string) (string, error) { return "", errors.New("이 빌드는 새 대화를 못 엽니다") }
	})
	code, body := rig.askFresh(t)
	if code != http.StatusBadGateway {
		t.Fatalf("실패인데 %d: %v", code, body)
	}
	if why, _ := body["error"].(string); !strings.Contains(why, "못 엽니다") {
		t.Fatalf("사유를 그대로 안 전한다: %v", body["error"])
	}
	// 실패했으면 **옛 대화를 놓지 않는다** — 놓으면 사람은 쓰던 대화까지 잃는다.
	if _, sid, _ := rig.api.Bridge.Bound(); sid != "s_deck" {
		t.Fatalf("실패했는데 옛 대화를 놓았다: %q", sid)
	}
}

// 이 자리도 **토큰과 루프백을 지난다.**
func TestFreshIsBehindTheSameGuard(t *testing.T) {
	api := &API{
		Bridge: NewBridge(), Attachments: NewAttachments(), ConfigDir: t.TempDir(),
		Token: "s3cret", Own: &OwnCompanion{ConfigDir: t.TempDir()}, Work: NewOwnWork(),
	}
	mux := http.NewServeMux()
	api.Route(mux)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/fresh", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("토큰 없이 지나갔다: %d %s", w.Code, w.Body.String())
	}
}

// 자기 컴패니언을 마련하도록 안 세운 헬퍼는 **그렇다고 말한다.**
func TestOwnSaysSoWhenTheHelperHasNoOwnCompanion(t *testing.T) {
	api := &API{Bridge: NewBridge(), Attachments: NewAttachments(), ConfigDir: t.TempDir()}
	w := httptest.NewRecorder()
	api.own(w, httptest.NewRequest(http.MethodPost, "/api/own", nil))
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("세워지지 않았는데 %d: %s", w.Code, w.Body.String())
	}
}

// 이 자리도 **토큰과 루프백을 지난다.**
func TestOwnIsBehindTheSameGuard(t *testing.T) {
	api := &API{
		Bridge: NewBridge(), Attachments: NewAttachments(), ConfigDir: t.TempDir(),
		Token: "s3cret", Own: &OwnCompanion{ConfigDir: t.TempDir()}, Work: NewOwnWork(),
	}
	mux := http.NewServeMux()
	api.Route(mux)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/own", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("토큰 없이 지나갔다: %d %s", w.Code, w.Body.String())
	}
}
