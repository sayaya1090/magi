package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// landing 은 **말만 하고 끝나는 턴**을 잡으려고 있다. 그 판정이 맞는지는 문장이 아니라 이 문을
// 실제로 통과시켜 봐야 안다 — 계획을 신고로 받아 주면 이 플러그인은 있으나 마나다.
func loadLanding(t *testing.T, reg *builtin.Registry, log *syncLog) *Host {
	t.Helper()
	src := filepath.Join("..", "..", "..", "..", "plugins", "landing")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled landing plugin not present")
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   reg,
		ContextReg: &fakeContextReg{},
		DataDir:    t.TempDir(),
		Logf:       log.logf,
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load landing: %v", err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
	return h
}

func TestProbeLandingRefusesAPlanAndAcceptsWork(t *testing.T) {
	reg := builtin.NewRegistry()
	log := &syncLog{}
	loadLanding(t, reg, log)

	tool, ok := reg.Get("land")
	if !ok {
		t.Fatal("land 툴이 안 걸렸다 — 이 플러그인의 유일한 게이트다")
	}
	wd := t.TempDir()

	// 하나 — **빈 신고는 끝이 아니다.** 실물에서 본 턴이 정확히 이 모양이었다: 정찰만 하고
	// "얹겠습니다" 한 줄.
	got, isErr := execTool(t, tool, `{"did":[]}`, wd)
	if !isErr {
		t.Errorf("빈 did 를 받아 줬다: %s", got)
	}
	if !strings.Contains(got, "아직 끝이 아닙니다") {
		t.Errorf("거절이 왜인지를 안 말한다: %s", got)
	}

	// 둘 — **손잡이 없는 줄은 소감이다.** 다시 집을 수 있는 이름이 없으면 아무도 확인 못 한다.
	got, isErr = execTool(t, tool, `{"did":["표지를 정리했습니다"]}`, wd)
	if !isErr {
		t.Errorf("손잡이 없는 신고를 받아 줬다: %s", got)
	}
	if !strings.Contains(got, "손잡이") {
		t.Errorf("무엇이 없는지를 안 말한다: %s", got)
	}

	// 셋 — **계획은 한 일이 아니다.** 숫자가 있어도 미래형이면 거절한다. 이 갈래가 없으면
	// "5장부터 얹겠습니다" 가 손잡이를 달고 통과한다.
	got, isErr = execTool(t, tool, `{"did":["슬라이드 5부터 7까지 만들겠습니다"]}`, wd)
	if !isErr {
		t.Errorf("계획 문장을 신고로 받아 줬다: %s", got)
	}
	if !strings.Contains(got, "계획") {
		t.Errorf("계획이라고 말하지 않는다: %s", got)
	}

	// 넷 — **진짜 신고는 받는다.** 거절만 하는 문은 문이 아니라 벽이다.
	got, isErr = execTool(t, tool,
		`{"did":["슬라이드 7(id 269#2126229183) 에 4x3 표를 넣고 대체 텍스트를 달았다"],`+
			`"verified":"read_slide 로 되읽어 alt 가 비지 않은 것을 봤다","left":""}`, wd)
	if isErr {
		t.Fatalf("옳은 신고를 거절했다: %s", got)
	}
	if !strings.Contains(got, "착지") {
		t.Errorf("받았다는 말이 없다: %s", got)
	}
}

// 안 지나고 끝난 턴은 **세고 알린다.** 되살릴 수는 없다 — `turn_finished` 는 비차단이다.
// 그러니 최소한 조용하지는 않아야 하고, 그 「조용하지 않음」이 이 플러그인이 실제로 주는 것이다.
func TestProbeLandingCountsAnUnlandedTurn(t *testing.T) {
	reg := builtin.NewRegistry()
	log := &syncLog{}
	h := loadLanding(t, reg, log)

	h.FireEventWith("turn_finished", map[string]string{
		"session": "s1", "text": "가이드를 골랐습니다. 5장부터 얹겠습니다.",
	})
	h.DrainEvents(5 * time.Second)

	if !strings.Contains(log.String(), "unlanded turn") {
		t.Errorf("신고 없이 끝난 턴을 기록하지 않았다: %s", log.String())
	}

	// 그리고 **지난 턴은 안 센다.** 모든 턴을 세면 그 수는 아무 말도 안 하게 된다.
	before := log.String()
	tool, _ := reg.Get("land")
	execTool(t, tool, `{"did":["슬라이드 9(id 271#150) 제목을 24pt 로 줄였다"]}`, t.TempDir())
	h.FireEventWith("turn_finished", map[string]string{"session": "s2", "text": "했습니다"})
	h.DrainEvents(5 * time.Second)
	if strings.Count(log.String(), "unlanded turn") != strings.Count(before, "unlanded turn") {
		t.Errorf("착지한 턴까지 셌다: %s", log.String())
	}
}

// **읽은 것은 한 일이 아니다.** 읽기에도 손잡이가 있어서 첫 판본의 문을 그냥 지났다 — 덱을
// 지으라는 부탁에 정찰만 하고 「list_slides 로 총 1장임을 확인했습니다(id 256#…)」로 착지했다
// (2026-09-04 실물). 읽기 동사는 verified 의 몫이다.
func TestProbeLandingRefusesAReadAsIfItWereWork(t *testing.T) {
	reg := builtin.NewRegistry()
	loadLanding(t, reg, &syncLog{})
	tool, _ := reg.Get("land")
	wd := t.TempDir()

	got, isErr := execTool(t, tool,
		`{"did":["list_slides 로 덱을 읽어 총 1장임을 확인했습니다 — id 256#2243864090"]}`, wd)
	if !isErr {
		t.Errorf("읽은 것을 한 일로 받아 줬다: %s", got)
	}
	if !strings.Contains(got, "읽은 것") {
		t.Errorf("무엇이 문제인지 안 말한다: %s", got)
	}

	// 그래도 **진짜 변경은 받는다.** 거절만 하는 문은 문이 아니라 벽이다.
	got, isErr = execTool(t, tool,
		`{"did":["슬라이드 5(id 270#22086) 를 새로 만들고 제목을 넣었다"],"verified":"read_slide 로 되읽었다"}`, wd)
	if isErr {
		t.Fatalf("진짜 변경을 거절했다: %s", got)
	}
}

// land 없이 끝난 턴은 **되부른다** — 설정에 소켓이 있으면 `magi --relay <소켓>` 으로 데몬에 사용자 메시지를
// 넣고, 한 대화에 두 번까지다. 여기서는 PATH 앞에 가짜 magi 를 두어 무엇을 보냈는지 잰다.
func TestProbeLandingNudgesAnUnlandedTurnThroughTheRelay(t *testing.T) {
	bin := t.TempDir()
	got := filepath.Join(bin, "got.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + got + "\ncat >> " + got + "\nprintf '{\"ok\":true}\\n'\n"
	if err := os.WriteFile(filepath.Join(bin, "magi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	reg := builtin.NewRegistry()
	log := &syncLog{}
	src := filepath.Join("..", "..", "..", "..", "plugins", "landing")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled landing plugin not present")
	}
	// 알림 채널을 붙인다 — 없으면 `magi.notify` 가 조용히 실패해 그 줄의 인자 오류를 못 잡는다(실물에서 죽었던 자리).
	var notes []string
	h := NewHostWithConfig(HostConfig{
		ToolSink: reg, ContextReg: &fakeContextReg{}, DataDir: t.TempDir(), Logf: log.logf,
		Notify:        func(sid, text string) { notes = append(notes, sid+": "+text) },
		PluginConfigs: map[string]map[string]any{"landing": {"socket": "/tmp/fake-daemon.sock"}},
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load landing: %v", err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })

	// 인사말 턴은 되부르지도, 세지도, 알리지도 않는다 — 신고할 일이 없다. 「말씀해 주시면 진행하겠습니다」는
	// 제안이지 계획이 아니다(실물 2026-09-06: 「겠습니다」 하나로 인사가 되불려 와 land 가 두 번 거절됐다).
	h.FireEventWith("turn_finished", map[string]string{"session": "s9",
		"text": "안녕하세요! 무엇을 도와드릴까요? 시트 이름을 말씀해 주시면 바로 진행하겠습니다."})
	h.DrainEvents(5 * time.Second)
	if _, err := os.Stat(got); err == nil {
		body, _ := os.ReadFile(got)
		t.Fatalf("인사말 턴을 되불렀다:\n%s\nlog: %s", body, log.String())
	}
	if len(notes) != 0 {
		t.Fatalf("인사말 턴을 알렸다: %v", notes)
	}
	for i := 0; i < 3; i++ {
		h.FireEventWith("turn_finished", map[string]string{"session": "s9", "text": "정리했습니다."})
		h.DrainEvents(5 * time.Second)
	}
	body, _ := os.ReadFile(got)
	sent := string(body)
	if strings.Count(sent, "--relay /tmp/fake-daemon.sock") != 2 {
		t.Fatalf("한 대화에 두 번까지 되불러야 한다(cap):\n%s\nlog: %s", sent, log.String())
	}
	if !strings.Contains(sent, `"method":"submit"`) || !strings.Contains(sent, `"session":"s9"`) || !strings.Contains(sent, "⟦landing⟧") {
		t.Errorf("submit 이 표식 달린 사용자 메시지로 가야 한다:\n%s", sent)
	}
	if !strings.Contains(sent, "land{did, verified, left}") {
		t.Errorf("무엇으로 끝내라는지 말해야 한다:\n%s", sent)
	}
	if !strings.Contains(log.String(), "nudged session s9 to land (2/2)") || !strings.Contains(log.String(), "no nudge") {
		t.Errorf("로그가 넛지와 cap 을 안 적는다: %s", log.String())
	}
	if len(notes) != 3 || !strings.HasPrefix(notes[0], "s9: landing:") {
		t.Errorf("알림이 (세션, 글)로 세 번 가야 한다(일을 했다고 한 턴만): %v", notes)
	}
	if strings.Contains(log.String(), "bad argument") {
		t.Errorf("처리기가 죽었다: %s", log.String())
	}
}

// **정직한 무(無)는 신고다.** 손잡이를 요구하기만 하면 아무것도 안 바꾼 턴은 영영 못 끝난다 — 실물에서
// 넛지가 「바꾼 것이 없으면 그렇게 적으라」 했는데 문이 손잡이가 없다고 두 번 거절했다(2026-09-06, 엑셀
// 작업창의 인사 턴). 「바꾼 것 없음」 한 줄은 지나가고, 손잡이 없는 「정리했습니다」는 여전히 막힌다.
func TestProbeLandingAcceptsAnHonestNothing(t *testing.T) {
	reg := builtin.NewRegistry()
	log := &syncLog{}
	loadLanding(t, reg, log)
	tool, _ := reg.Get("land")
	wd := t.TempDir()

	got, isErr := execTool(t, tool, `{"did":["바꾼 것 없음 — 인사에만 답했습니다"]}`, wd)
	if isErr || !strings.Contains(got, "바꾼 것 없음") {
		t.Errorf("정직한 무를 거절했다: %s", got)
	}
	got, isErr = execTool(t, tool, `{"did":["No change — only answered a greeting"],"left":""}`, wd)
	if isErr {
		t.Errorf("영어 무를 거절했다: %s", got)
	}
	// 무와 소감을 섞으면 소감이 걸린다 — 무는 **한 줄일 때만** 무다.
	got, isErr = execTool(t, tool, `{"did":["바꾼 것 없음","표지를 정리했습니다"]}`, wd)
	if !isErr {
		t.Errorf("무 옆의 손잡이 없는 줄을 받아 줬다: %s", got)
	}
}
