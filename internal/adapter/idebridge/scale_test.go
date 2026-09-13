package idebridge

import (
	"fmt"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
)

// synthLog is a conversation of n events shaped like a real one: a question, an answer, a tool call,
// its result, and the turn ending.
func synthLog(n int) []event.Event {
	var evs []event.Event
	for i := 0; len(evs) < n; i++ {
		id := fmt.Sprintf("%d", i)
		evs = append(evs,
			mk(int64(len(evs)), "prompt.submitted", map[string]any{"messageId": "u" + id,
				"parts": []any{map[string]any{"kind": "text", "text": "a question about the codebase"}}}, nil),
			mk(int64(len(evs)+1), "part.appended", map[string]any{"messageId": "m" + id, "role": "assistant",
				"part": map[string]any{"kind": "text", "text": "an answer of a few dozen characters"}}, nil),
			mk(int64(len(evs)+2), "part.appended", map[string]any{"messageId": "m" + id, "role": "assistant",
				"part": map[string]any{"kind": "tool-call", "toolCall": map[string]any{
					"callId": "c" + id, "name": "bash", "args": map[string]any{"command": "go test ./..."}}}}, nil),
			mk(int64(len(evs)+3), "part.appended", map[string]any{"messageId": "m" + id, "role": "tool",
				"part": map[string]any{"kind": "tool-result", "toolResult": map[string]any{
					"callId": "c" + id, "content": "ok\nsome output"}}}, nil),
			mk(int64(len(evs)+4), "turn.finished", map[string]any{}, nil),
		)
	}
	return evs[:n]
}

// **Does clearing the waiting marks cost the conversation's length?**
//
// The fold was quadratic until 2026-09-13. `answerPending` walked every row built so far, and an
// answer arrives per assistant part, so a conversation cost its own length squared — 200 events
// 1.5ms, 20000 events 786ms, 53% of it inside that one closure, and the same shape again in
// `turn.finished` (once per turn, over everything). The `rows` door pays this on every call: three
// quarters of a second to answer one question about a long conversation, and a live surface that
// re-folds per event could not keep up at all. It is 157ms now, and proportional.
//
// ⚠ **Counted, not timed.** A ratio of milliseconds cannot hold this: measured on a log 16× longer,
// the quadratic shape and the proportional one came out 36× and 24× — one and a half apart, because
// at that size the allocator and the GC cost more than the walk does. So the fold reports what it
// looked at, and this asks for a bound that the old shape misses by a mile rather than by a whisker.
//
// The bound is 4 marks per prompt: a prompt's mark is cleared once by the answer and once when the
// turn ends, it may be looked at again while it waits, and a resurfaced one comes back as a new
// candidate. Measured after the fix: 2.0 per prompt. The old shape, on the same log: 1000×.
func TestClearingTheMarksCostsTheMarksNotTheConversation(t *testing.T) {
	for _, n := range []int{1000, 8000} {
		events := synthLog(n)
		rows, stats := fold(events)
		prompts := 0
		for _, r := range rows {
			if r.Who == WhoUser {
				prompts++
			}
		}
		if prompts < n/10 {
			t.Fatalf("사건 %d 개에서 질문 행이 %d 개뿐이다 — 합성 로그가 잴 것을 안 만든다", n, prompts)
		}
		per := float64(stats.pendingVisits) / float64(prompts)
		t.Logf("사건 %5d, 질문 %4d → 표시를 본 횟수 %7d (질문당 %.1f)", n, prompts, stats.pendingVisits, per)
		if per > 4 {
			t.Errorf("질문 하나당 대기 표시를 %.1f 번 본다 — 표시 수가 아니라 대화 길이에 비례하는 "+
				"모양이 돌아왔다. 사건마다 지금까지의 행을 훑는 자리가 생겼는지 볼 것", per)
		}
	}
}
