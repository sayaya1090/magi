package llm

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
)

// capture runs fn with os.Stderr replaced, and returns what it wrote.
func stderrOf(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	fn()
	os.Stderr = old
	if cerr := w.Close(); cerr != nil {
		t.Fatalf("탐침 파이프를 못 닫았다: %v", cerr)
	}
	out := <-done
	if rerr := r.Close(); rerr != nil {
		t.Fatalf("탐침 파이프를 못 닫았다: %v", rerr)
	}
	return out
}

// 아무것도 안 온 것을 「산문으로 답했다」고 말하지 않는다.
//
// ⚠ **이 이슈의 제목이 그 문장이었고, 고쳐졌는데 아무도 안 붙들고 있었다.** 0바이트를
// `jsonx.Report` 에 넘기면 "no JSON object or array in the reply (the model answered in prose)"
// 가 나온다 — 빈 문자열에 대해 형식적으로는 참이지만, 로그를 읽는 사람에게는 **모델이 뭔가
// 길게 말했는데 JSON 이 아니었다**는 뜻으로 읽힌다. 실제로는 생각만 수천 자 오고 답은 한 자도
// 안 온 것이었고, 두 상황은 사람이 다음에 할 일이 다르다(백엔드를 의심할 것인가, 모델의
// 추론 채널을 의심할 것인가).
//
// 2026-09-11 실측: `noteUnparsed` 의 빈 입력 갈래를 통째로 들어내도 이 꾸러미가 전부 초록이었다.
// 동작은 시험이 붙들고 있었지만, 고침이 내놓는 **문장**은 아무 데서도 안 붙들렸다.
func TestNothingIsNotProse(t *testing.T) {
	got := stderrOf(t, func() { noteUnparsed("the council's verdicts", "") })
	if strings.Contains(got, "prose") {
		t.Errorf("0바이트를 산문이라 말한다: %q", got)
	}
	if !strings.Contains(got, "nothing came back") {
		t.Errorf("아무것도 안 왔다는 말을 안 한다: %q", got)
	}
	// 공백만 온 것도 같은 사실이다 — 파서에 넘길 것이 없다.
	got = stderrOf(t, func() { noteUnparsed("the council's verdicts", "   \n\t ") })
	if strings.Contains(got, "prose") {
		t.Errorf("공백뿐인 답을 산문이라 말한다: %q", got)
	}
	// 그리고 진짜 산문은 여전히 진단을 받는다 — 빈 것만 가르는 것이지 진단을 끈 것이 아니다.
	got = stderrOf(t, func() { noteUnparsed("the council's verdicts", "I think the agent is done.") })
	if !strings.Contains(got, "prose") {
		t.Errorf("진짜 산문에 진단이 안 붙는다: %q", got)
	}
}

// 끊긴 회신이 「0자」라고만 말하면, 생각이 수천 자 왔다는 사실이 어디에도 안 남는다.
//
// 스트림 가드는 생각과 말을 둘 다 세어 한도를 판정하고(internal/app/provider_guard.go), 파서는
// 말만 본다. 그 비대칭이 이 이슈의 뿌리였다 — 한쪽은 수천 자를 보고 끊었는데 다른 쪽은 0자를
// 보고 「아무것도 안 왔다」고 적었다. 사람이 그 둘을 잇지 못하면 백엔드가 죽은 줄 안다.
func TestACutReplySaysHowMuchThinkingCameFirst(t *testing.T) {
	long := strings.Repeat("'s report is not a valid completion.\nThe agent", 90)
	got := stderrOf(t, func() { cutOff("a council panel reply", "", long, errors.New("a degenerate repetition loop")) })
	if !strings.Contains(got, "reasoning came first") {
		t.Errorf("생각이 먼저 왔다는 말을 안 한다: %q", got)
	}
	if !strings.Contains(got, "none of it an answer") {
		t.Errorf("그 생각이 답은 아니었다는 말을 안 한다: %q", got)
	}
	// 길이가 실제로 실린다. 「생각이 있었다」와 「4213자였다」는 다른 사실이고, 뒤엣것이라야
	// 가드가 무엇을 보고 끊었는지 사람이 맞춰 볼 수 있다.
	if !strings.Contains(got, strconv.Itoa(len(long))) {
		t.Errorf("생각의 길이를 안 말한다 (%d 자여야 한다): %q", len(long), got)
	}
	// 말이 온 경우에는 그 줄이 끼어들지 않는다 — 그때는 끊긴 자리가 요점이다.
	got = stderrOf(t, func() { cutOff("a council reply", `{"decision":`, long, errors.New("cut")) })
	if strings.Contains(got, "reasoning came first") {
		t.Errorf("답이 온 회신에까지 생각 줄이 붙는다: %q", got)
	}
}
