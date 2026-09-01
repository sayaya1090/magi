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
	if got := rig.settle(t); got.Phase != OwnFailed || !strings.Contains(got.Why, "명단에서") {
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
