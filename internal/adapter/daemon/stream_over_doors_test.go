package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// **연결을 내주는 문은 끝을 말하고 끝난다** — 그리고 이 규칙은 표에서 저절로 따라온다(#197).
//
// 닫힘이 못 알리는 이유는 `Response.Over` 에 적혀 있다(윈도우 AF_UNIX 가 쓰기 직후의 닫힘을 잃는다,
// 순수 소켓으로 600회 중 12회). 그래서 `transcript` 가 끝을 말하게 됐는데, **`watch` 는 안 하고
// 있었다** — 같은 모양의 문이고 같은 노출이다. 문 하나를 고치는 것으로는 다음 문이 또 잊는다.
//
// 그래서 이 가드는 **표에서 대상을 고른다**: `why` 가 「gives the connection over」라고 적은 문이면
// 끝을 말해야 한다. 새 스트림 문이 그 문장으로 들어오면 자동으로 여기 걸리고, 잊으면 빨개진다.
// (`capsOf` 가 같은 표에서 파생되는 것과 같은 이유 — 두 곳에 적힌 사실은 어긋난다.)
//
// ⚠ 한 번만 답하고 연결을 되돌려주는 문들(`roster`·`shutdown`·`restart`·`update`)은 대상이 아니다.
// 그쪽은 스트림이 아니라 보통의 왕복이고, 끝을 말할 스트림이 없다.

// overRecorder is a wire whose frames can be read back, with a read side that BLOCKS — a peer that
// is still there. A scanner over an already-ended reader would make every door's drain goroutine
// fire `hungUp()` at once, and then the test would measure "the peer left", not "the door finished".
type overRecorder struct {
	out   *strings.Builder
	close func()
}

func recordingWire(t *testing.T) (wire, *overRecorder) {
	t.Helper()
	var out strings.Builder
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close(); _ = pr.Close() })
	return wire{
		enc:  json.NewEncoder(&out),
		sc:   bufio.NewScanner(pr),
		home: t.TempDir(),
		stop: func() {}, restart: func() {},
	}, &overRecorder{out: &out, close: func() { _ = pw.Close() }}
}

// frames parses what the door wrote, in order.
func (r *overRecorder) frames(t *testing.T) []Response {
	t.Helper()
	var got []Response
	for _, line := range strings.Split(strings.TrimSpace(r.out.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("문이 쓴 줄이 JSON 이 아니다: %q (%v)", line, err)
		}
		got = append(got, resp)
	}
	return got
}

// endingEngine answers every door this test drives, and every stream it opens is ALREADY finished:
// the transcript's events channel is closed and the watch returns at once. That is the ending this
// guard is about — the door's own, not the peer's.
type endingEngine struct{ omniEngine }

func (e *endingEngine) Watch(context.Context, string, func(Handover) error) error { return nil }

func (e *endingEngine) Subscribe(context.Context, session.SessionID, int64) (<-chan event.Event, func(), error) {
	ch := make(chan event.Event)
	close(ch)
	return ch, func() {}, nil
}

func TestEveryDoorThatGivesTheConnectionOverSaysWhenItIsOver(t *testing.T) {
	// The request each such door needs to get past its own gates, by name. A door named here that
	// leaves the table is caught below; one that joins it without an entry is caught too.
	asks := map[string]Request{
		"transcript": {Method: "transcript", Session: "s_new"},
		"watch":      {Method: "watch", Name: "r_1"},
	}
	// Selected in BOTH directions, the way consoleSurface is: the table picks the doors, and every
	// door named here must have been picked. Without the second half the selection is prose anybody
	// can reword — measured: changing watch's `why` took it out of this guard and nothing failed.
	exercised := map[string]bool{}
	giving := 0
	for name, d := range streams {
		if !strings.Contains(d.why, "gives the connection over") {
			continue
		}
		giving++
		exercised[name] = true
		ask, ok := asks[name]
		if !ok {
			t.Errorf("%q 가 연결을 내주는 문이 됐는데 이 가드가 그것을 부를 요청을 모른다 — "+
				"asks 에 넣어라(그러지 않으면 새 문은 이 규칙 밖에 있다)", name)
			continue
		}
		t.Run(name, func(t *testing.T) {
			w, rec := recordingWire(t)
			got := d.run(context.Background(), &endingEngine{}, ask, w)
			if got != done {
				t.Fatalf("스트림이 끝났는데 연결을 되돌려줬다 (%v) — 이 문은 대상이 아니거나 표가 틀렸다", got)
			}
			frames := rec.frames(t)
			if len(frames) == 0 {
				t.Fatal("아무 프레임도 안 썼다 — 끝을 말할 자리가 없다")
			}
			last := frames[len(frames)-1]
			if !last.Over {
				t.Errorf("끝나면서 끝났다고 말하지 않았다 — 읽는 쪽은 닫힘을 기다리게 되고, 윈도우에서 "+
					"그 닫힘은 잃어버려질 수 있다(Response.Over). 마지막 프레임: %+v", last)
			}
			// 끝은 **마지막**이어야 한다. 중간에 말하면 그 뒤 프레임을 읽는 쪽이 버린다.
			for i, f := range frames[:len(frames)-1] {
				if f.Over {
					t.Errorf("%d번째 프레임이 벌써 끝났다고 말한다 — 그 뒤로 %d개가 더 갔다",
						i, len(frames)-1-i)
				}
			}
		})
	}
	if giving == 0 {
		t.Fatal("표에서 연결을 내주는 문을 하나도 못 찾았다 — 이 가드는 아무것도 안 재고 있다")
	}
	for name := range asks {
		if !exercised[name] {
			t.Errorf("%q 를 부를 요청이 여기 있는데 표가 그 문을 안 골랐다 — `why` 의 문장이 바뀌었다면 "+
				"그 문은 이 규칙 밖으로 나간 것이고, 그것은 결정이어야 한다(여기서 이름을 지워라). "+
				"사라진 문이면 같은 자리에서 이름을 지워라 — 안 그러면 이 표가 허구가 된다", name)
		}
	}
}
