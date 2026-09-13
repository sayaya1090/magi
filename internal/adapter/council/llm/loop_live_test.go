package llm

// **생각만 하며 도는 백엔드를 세워, 보고된 순환을 통째로 재현한다** (#182).
//
// 이 이슈에서 두 세션이 같은 자리에 멈췄다 — 「증상 자체는 재현 못 했습니다: 3× 기권 무한 루프는
// reasoning 을 내는 실 백엔드가 있어야 합니다」. 그래서 그 백엔드를 세운다. 목이 하는 일은 하나다:
// **짧은 한 토막을 reasoning 채널로 되풀이하고, 답은 한 자도 안 보낸다.**
//
// ⚠ **제품의 사슬을 그대로 태운다** — HTTP 목 → `openai.New` → `app.GuardProvider` → 이 카운슬.
// 이 꾸러미의 다른 패널 시험들은 `port.LLMProvider` 를 가짜로 끼우므로 **어댑터와 스트림 가드를
// 건너뛴다**, 그리고 이 결함은 정확히 그 셋 사이에 산다: 가드는 생각과 말을 **둘 다** 세어 한도를
// 판정하고, `drain` 은 **말만** 모으고, 파서는 0바이트를 받는다. 가짜 Provider 로는 영원히 안 보인다.
//
// 여기서 잰 것(2026-09-14):
//
//	stream-guard aborted a degenerate repetition loop (a 46-byte unit repeated)
//	a council panel reply was cut off after 0 chars (276 chars of reasoning came first, …)
//	the council panel's verdicts (every lens recorded as an abstain) could not be parsed:
//	    nothing came back to parse (0 bytes)
//	→ 렌즈 셋 전부 기권, 백엔드 호출 2회
//
// 이슈에 적힌 두 줄과 같은 순환이고, 문구는 `b5199fdc` 가 고친 뒤의 것이다 — 그 고침이 **실제 사슬
// 위에서도** 사는지를 이 시험이 처음으로 본다(기존 시험은 `noteUnparsed` 를 직접 부른다).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/llm/openai"
	"github.com/sayaya1090/magi/internal/app"
	"github.com/sayaya1090/magi/internal/core/council"
	"github.com/sayaya1090/magi/internal/port"
)

// loopingUnit is the shape the issue reports, to the byte: the guard named a 46-byte unit and quoted
// it. Using the reported string rather than a made-up one keeps this test about THAT report.
const loopingUnit = "'s report is not a valid completion.\nThe agent"

// thinkingInLoops streams reasoning — and only reasoning — repeating one short unit. It never sends a
// word of content, which is what makes `drain` come back empty while the guard has seen hundreds of
// bytes. Every call is counted: the retry is part of what this measures.
func thinkingInLoops(t *testing.T, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		flush := func() {
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		// Enough repetitions to pass the guard's own bounds (a unit under 256 bytes, at least three
		// times, at least 128 bytes of tail, checked every 256 streamed bytes) with room to spare —
		// this is not testing where that threshold sits, only that the loop is reached.
		for i := 0; i < 200; i++ {
			b, _ := json.Marshal(map[string]any{"choices": []map[string]any{
				{"index": 0, "delta": map[string]string{"reasoning_content": loopingUnit}},
			}})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return // the guard hung up, which is the point
			}
			flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flush()
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestABackendThatOnlyThinksInLoopsAbstainsAndSaysWhyInWordsThatFit(t *testing.T) {
	var calls atomic.Int64
	srv := thinkingInLoops(t, &calls)
	c := New(func(string) port.LLMProvider {
		return app.GuardProvider(openai.New(srv.URL+"/v1", "k"))
	}, "mock")

	var d council.Deliberation
	var err error
	said := stderrOf(t, func() {
		d, err = c.Deliberate(context.Background(), port.DeliberationRequest{
			Task: "ship it", Actions: "wrote hello.txt", Members: council.DefaultMembers()})
	})
	if err != nil {
		t.Fatalf("생각만 하는 백엔드는 카운슬을 고장 내지 않는다 — 기권으로 끝나야 한다: %v", err)
	}

	// ① 렌즈마다 기권이고, **답을 지어내지 않는다.** 이 결함의 위험은 빨간색이 아니라 꾸며낸 초록이다.
	if len(d.Verdicts) != len(council.DefaultMembers()) {
		t.Fatalf("렌즈 수가 %d — 멤버마다 하나여야 한다", len(d.Verdicts))
	}
	for _, v := range d.Verdicts {
		if v.Decision != council.Abstain {
			t.Errorf("%s(%s): 아무것도 안 받았는데 %q 를 냈다 — 없는 답을 지어낸 것이다",
				v.Member, v.Lens, v.Decision)
		}
		if strings.TrimSpace(v.Rationale) == "" {
			t.Errorf("%s(%s): 기권에 사유가 없다 — 화면이 「왜」를 못 보여 준다", v.Member, v.Lens)
		}
	}

	// ② **0바이트는 산문이 아니다** — 이 이슈 제목의 그 문장. 이제껏 `noteUnparsed` 를 직접 부르는
	//    시험만 이것을 붙들었고, 실제 사슬 위에서는 아무도 안 봤다.
	if strings.Contains(said, "answered in prose") {
		t.Errorf("아무것도 안 온 것을 「산문으로 답했다」고 적는다 — 로그를 읽는 사람이 백엔드가 "+
			"죽은 줄 안다:\n%s", said)
	}
	if !strings.Contains(said, "nothing came back to parse") {
		t.Errorf("빈 답을 빈 답이라고 말하지 않는다:\n%s", said)
	}

	// ③ 그리고 **생각이 몇 자 먼저 왔는지** 말해야 한다. 가드는 수천 자를 보고 끊었고 파서는 0자를
	//    봤다 — 그 둘을 맞춰 볼 수 있는 유일한 숫자가 이것이고, 이것이 있어야 다음에 의심할 곳이
	//    백엔드가 아니라 **모델의 추론 채널**이 된다.
	if !strings.Contains(said, "of reasoning came first") {
		t.Errorf("생각이 먼저 왔다는 사실이 로그에 없다 — 0자 회신이 침묵과 구별되지 않는다:\n%s", said)
	}
	if !strings.Contains(said, "degenerate repetition loop") {
		t.Errorf("끊은 이유를 말하지 않는다:\n%s", said)
	}

	// ④ 호출 수를 **사실로** 적어 둔다. 가드가 끊은 뒤에도 카운슬은 한 번 더 묻는다 — 같은 백엔드에,
	//    같은 순환으로. 그 재시도를 건너뛸지는 이 이슈의 열린 결정(제안 ③)이고 동작에 관한 판단이라
	//    여기서 정하지 않는다. 대신 **지금이 무엇인지**를 못박아, 바꾸는 사람이 알고 바꾸게 한다.
	if got := calls.Load(); got != 2 {
		t.Errorf("백엔드 호출이 %d회다 — 지금 계약은 2회(패널 한 번, 가드가 끊은 뒤 재시도 한 번)다. "+
			"줄였다면 #182 의 제안 ③ 을 실행한 것이니 이 줄과 그 이슈를 같이 고쳐라", got)
	}
	t.Logf("기권 %d · 백엔드 호출 %d회\n%s", len(d.Verdicts), calls.Load(), strings.TrimSpace(said))
}
