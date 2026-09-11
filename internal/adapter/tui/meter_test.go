package tui

import (
	"strings"
	"testing"
	"time"
)

func TestFmtDur(t *testing.T) {
	cases := map[time.Duration]string{
		47 * time.Second:                 "47s",
		0:                                "0s",
		(3*time.Minute + 49*time.Second): "3m49s",
		(2 * time.Minute):                "2m00s",
		(1*time.Hour + 2*time.Minute):    "1h02m",
		(1*time.Hour + 2*time.Minute + 30*time.Second): "1h02m",
	}
	for d, want := range cases {
		if got := fmtDur(d); got != want {
			t.Errorf("fmtDur(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestHumanTokens(t *testing.T) {
	cases := map[int]string{0: "0", 847: "847", 1000: "1.0k", 10400: "10.4k", 1234567: "1.2M"}
	for n, want := range cases {
		if got := humanTokens(n); got != want {
			t.Errorf("humanTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestTurnMeter(t *testing.T) {
	// Both tokens present.
	got := turnMeter(3*time.Minute+49*time.Second, 28100, 10400)
	for _, w := range []string{"3m49s", "↑28.1k", "↓10.4k"} {
		if !strings.Contains(got, w) {
			t.Errorf("turnMeter missing %q: %q", w, got)
		}
	}
	// No usage reported → time only, no arrows.
	if got := turnMeter(5*time.Second, 0, 0); got != "5s" {
		t.Errorf("turnMeter with no tokens = %q, want %q", got, "5s")
	}
	// Output only.
	if got := turnMeter(time.Second, 0, 200); !strings.Contains(got, "↓200") || strings.Contains(got, "↑") {
		t.Errorf("turnMeter output-only = %q", got)
	}
}

// 새 턴은 0에서 시작한다 — 앞 턴의 숫자를 물려받지 않는다.
//
// ⚠ **얼리는 쪽만 붙들려 있었다.** event_fold_test 는 턴이 끝날 때 사용량이 얼어붙는 것과,
// 사용량 0으로 끝난 턴이 총계를 지우지 않는 것을 잰다. 그런데 **턴이 시작할 때 지우는 줄**은
// 아무 시험도 안 봤다 — 2026-09-11 실측: `m.turnIn, m.turnOut, m.turnDur = 0, 0, 0` 을 통째로
// 지워도 이 꾸러미가 전부 초록이었다.
//
// 그 줄이 없으면 화면이 조용히 거짓말한다. 도는 동안 머리글은 `turnMeter(경과, turnIn, turnOut)`
// 을 그리므로, 새 턴은 **첫 사용량 이벤트가 올 때까지 앞 턴의 토큰 수**를 제 것인 양 내건다.
// 그리고 사용량을 하나도 안 내는 백엔드에서는 그 숫자가 턴 내내 남고, 끝날 때 그대로 얼어붙어
// 앞 턴의 값이 이 턴의 기록이 된다 — 위의 「사용량 0으로 끝난 턴」 시험이 일부러 지키는 그
// 자리가, 지울 것이 없으면 반대로 작동한다.
//
// 세 셈을 다 본다. 토큰 둘과 얼린 시간은 한 줄에서 같이 지워지므로, 하나만 재면 그 줄이 반쪽만
// 남았을 때를 못 본다.
func TestANewTurnStartsFromZero(t *testing.T) {
	mm := newTestModel(t)
	m := &mm
	// 앞 턴이 남긴 것.
	m.turnIn, m.turnOut, m.turnDur = 28100, 10400, 3*time.Minute+49*time.Second
	m.turnSteps, m.turnCouncil = 7, 2
	m.turnUnverified, m.turnReceipted = true, true

	m.submit("the next thing")

	if m.turnIn != 0 || m.turnOut != 0 {
		t.Errorf("새 턴이 앞 턴의 토큰을 이어 든다: ↑%d ↓%d — 첫 사용량이 오기 전까지 화면이 "+
			"그 숫자를 이 턴의 것으로 내건다", m.turnIn, m.turnOut)
	}
	if m.turnDur != 0 {
		t.Errorf("얼린 시간이 안 지워졌다: %v — 끝날 때 앞 턴의 값이 이 턴의 기록이 된다", m.turnDur)
	}
	if m.turnSteps != 0 || m.turnCouncil != 0 {
		t.Errorf("걸음·회차가 안 지워졌다: steps=%d council=%d", m.turnSteps, m.turnCouncil)
	}
	if m.turnUnverified || m.turnReceipted {
		t.Error("앞 턴의 판정이 새 턴으로 넘어왔다")
	}
	if m.turnStart.IsZero() {
		t.Error("경과 시계가 안 켜졌다 — 머리글이 0초에 멈춘다")
	}
	if m.turnFiles == nil {
		t.Error("이 턴이 건드린 파일 집합이 nil 이다 — 첫 쓰기가 패닉을 낸다")
	}
}
