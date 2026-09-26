package office

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// #201 §6.50 — 스트림 일꾼이 쓰는 상태(live·empty·lastSeq·history)와 내는 알림은 **그 일꾼의 세대가 지금의 묶음일 때만**
// 바뀐다. 아래 시험들은 시간을 기다리지 않는다: 주입 read 가 장벽(채널)에서 서고, 시험이 순서를 정한다. 옛 일꾼이 끝났다는
// 것은 streamDone 으로 본다 — 제품 stream 그대로다(시험용 복사본을 만들지 않는다).

// genHarness 는 세대마다 read 의 시작·풀어 줌·끝남을 채널로 잡는다.
type genHarness struct {
	t       *testing.T
	b       *Bridge
	mu      sync.Mutex
	started map[uint64]chan context.Context
	release map[uint64]chan func(ctx context.Context, gen uint64, sid string) error
	done    map[uint64]chan struct{}
}

func newGenHarness(t *testing.T) *genHarness {
	h := &genHarness{t: t, b: NewBridge(),
		started: map[uint64]chan context.Context{}, release: map[uint64]chan func(context.Context, uint64, string) error{}, done: map[uint64]chan struct{}{}}
	for g := uint64(1); g <= 4; g++ {
		h.started[g] = make(chan context.Context, 8)
		h.release[g] = make(chan func(context.Context, uint64, string) error, 1)
		h.done[g] = make(chan struct{})
	}
	var once [5]sync.Once
	h.b.streamDone = func(gen uint64) {
		if gen < 5 {
			once[gen].Do(func() { close(h.done[gen]) })
		}
	}
	h.b.read = func(ctx context.Context, gen uint64, _, sid string, _ int64) error {
		h.started[gen] <- ctx
		select {
		case act := <-h.release[gen]:
			return act(ctx, gen, sid)
		case <-ctx.Done():
			// 풀어 주기 전에 취소됐어도 시험이 정한 행동을 기다린다 — 늦게 돌아오는 읽기를 흉내 낸다.
			return (<-h.release[gen])(ctx, gen, sid)
		}
	}
	t.Cleanup(func() {
		h.b.Stop()
		for g := uint64(1); g <= 4; g++ {
			select {
			case h.release[g] <- func(context.Context, uint64, string) error { return context.Canceled }:
			default:
			}
		}
	})
	return h
}

func (h *genHarness) waitStart(gen uint64) context.Context {
	h.t.Helper()
	select {
	case ctx := <-h.started[gen]:
		return ctx
	case <-time.After(3 * time.Second):
		h.t.Fatalf("세대 %d 의 read 가 시작하지 않았다", gen)
		return nil
	}
}

func (h *genHarness) waitDone(gen uint64) {
	h.t.Helper()
	select {
	case <-h.done[gen]:
	case <-time.After(3 * time.Second):
		h.t.Fatalf("세대 %d 의 일꾼이 끝나지 않았다", gen)
	}
}

// dialed 는 readOnce 가 붙었을 때 하는 일 그대로 — 세대가 맞으면 live=true 와 알림.
func (h *genHarness) dialed(ctx context.Context, gen uint64, sid string) bool {
	return h.b.forGen(ctx, gen, func() []StreamFrame {
		h.b.live, h.b.empty = true, false
		return []StreamFrame{{Kind: "stream", Data: []byte(fmt.Sprintf(`{"live":true,"session":%q}`, sid))}}
	})
}

func (h *genHarness) state() (live, empty bool, seq int64, hist int) {
	h.b.mu.Lock()
	defer h.b.mu.Unlock()
	return h.b.live, h.b.empty, h.b.lastSeq, len(h.b.history)
}

// drainAll 은 지금 채널에 있는 프레임을 다 꺼낸다(기다리지 않는다).
func drainAll(ch <-chan StreamFrame) []StreamFrame {
	var out []StreamFrame
	for {
		select {
		case f := <-ch:
			out = append(out, f)
		default:
			return out
		}
	}
}

// 옛 read 가 시작된 뒤 다른 대화로 묶고, 새 스트림이 live 를 세우고 사건을 받은 **다음에** 옛 read 가 끊김으로 돌아온다.
// 새 묶음의 live·empty·history·lastSeq 와 창이 받은 알림이 전부 그대로여야 한다.
func TestAnOldStreamCannotUndoTheNewBinding(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(map[bool]string{false: "다른 대화", true: "같은 대화로 다시"}[same], func(t *testing.T) {
			h := newGenHarness(t)
			frames, unsub := h.b.Subscribe()
			defer unsub()
			if err := h.b.BindWith("/sock", "sess-A", "1@t", nil); err != nil {
				t.Fatal(err)
			}
			h.waitStart(1)
			next := "sess-B"
			if same {
				next = "sess-A"
			}
			if err := h.b.BindWith("/sock", next, "1@t", nil); err != nil {
				t.Fatal(err)
			}
			ctx2 := h.waitStart(2)
			if !h.dialed(ctx2, 2, next) {
				t.Fatal("지금 세대의 연결 성공을 막았다")
			}
			if !h.b.deliver(ctx2, 2, next, StreamFrame{Kind: "event", Data: []byte(`{"seq":5}`)}, 5) {
				t.Fatal("지금 세대의 사건을 막았다")
			}
			_ = drainAll(frames)

			// 옛 read 가 이제 돌아온다: 끊김 오류 + (readOnce 라면 했을) 늦은 연결 성공 시도.
			h.release[1] <- func(ctx context.Context, gen uint64, sid string) error {
				if h.dialed(ctx, gen, sid) {
					t.Error("옛 세대의 늦은 연결 성공이 live 를 세웠다")
				}
				return errors.New("연결이 끊겼다")
			}
			h.waitDone(1)

			if live, empty, seq, hist := h.state(); !live || empty || seq != 5 || hist != 1 {
				t.Fatalf("옛 일꾼이 새 묶음을 바꿨다: live=%v empty=%v seq=%d history=%d", live, empty, seq, hist)
			}
			for _, f := range drainAll(frames) {
				t.Errorf("옛 일꾼의 알림이 창에 왔다: %s %s", f.Kind, f.Data)
			}
		})
	}
}

// 멈춘 뒤 늦게 돌아온 읽기는 — 붙었든 끊겼든 — 상태도 알림도 못 바꾼다.
func TestAStoppedBridgeIgnoresLateReads(t *testing.T) {
	for _, dialOK := range []bool{true, false} {
		t.Run(map[bool]string{true: "늦은 연결 성공", false: "늦은 연결 실패"}[dialOK], func(t *testing.T) {
			h := newGenHarness(t)
			frames, unsub := h.b.Subscribe()
			defer unsub()
			if err := h.b.BindWith("/sock", "sess-A", "1@t", nil); err != nil {
				t.Fatal(err)
			}
			h.waitStart(1)
			h.b.Stop()
			_ = drainAll(frames)
			h.release[1] <- func(ctx context.Context, gen uint64, sid string) error {
				if dialOK {
					if h.dialed(ctx, gen, sid) {
						t.Error("멈춘 뒤의 연결 성공이 live 를 세웠다")
					}
					return nil
				}
				return errors.New("dial: 연결 거부")
			}
			h.waitDone(1)
			if live, _, _, _ := h.state(); live {
				t.Error("멈춘 묶음이 살아 있다고 적혔다")
			}
			for _, f := range drainAll(frames) {
				t.Errorf("멈춘 뒤 알림이 창에 왔다: %s %s", f.Kind, f.Data)
			}
		})
	}
}

// 지금 세대는 다 된다 — 막는 자리가 전부 막으면 이 시험이 먼저 안다: 붙음 → 사건 → restart → 끊김 안내.
func TestTheCurrentBindingStillStreams(t *testing.T) {
	h := newGenHarness(t)
	frames, unsub := h.b.Subscribe()
	defer unsub()
	_ = drain(t, frames, "stream", time.Second) // 붙기 전 상태 한 줄
	if err := h.b.BindWith("/sock", "sess-A", "1@t", nil); err != nil {
		t.Fatal(err)
	}
	h.waitStart(1)
	h.release[1] <- func(ctx context.Context, gen uint64, sid string) error {
		if !h.dialed(ctx, gen, sid) {
			t.Error("지금 세대의 연결 성공을 막았다")
		}
		h.b.deliver(ctx, gen, sid, StreamFrame{Kind: "event", Data: []byte(`{"seq":3}`)}, 3)
		h.b.deliver(ctx, gen, sid, StreamFrame{Kind: "restart", Data: []byte(`{"why":"x"}`)}, -1)
		return errors.New("연결이 끊겼다")
	}
	// 끊긴 뒤 일꾼은 다시 붙으러 간다 — 그 둘째 read 가 시작하면 앞의 안내는 다 나간 것이다.
	h.waitStart(1)
	var kinds []string
	for _, f := range drainAll(frames) {
		kinds = append(kinds, f.Kind)
		if f.Kind == "note" && !strings.Contains(string(f.Data), "연결이 끊겼다") {
			t.Errorf("끊김 안내에 사유가 없다: %s", f.Data)
		}
	}
	if strings.Join(kinds, ",") != "stream,event,restart,stream,note" {
		t.Errorf("지금 세대의 알림 순서: %v", kinds)
	}
	if live, _, seq, hist := h.state(); live || seq != -1 || hist != 0 {
		t.Errorf("끊김 뒤 상태: live=%v seq=%d history=%d", live, seq, hist)
	}
	h.release[1] <- func(context.Context, uint64, string) error { return context.Canceled }
}

// 빈 대화도 지금 세대면 제대로 말한다 — empty 한 번.
func TestTheCurrentBindingStillSaysEmpty(t *testing.T) {
	h := newGenHarness(t)
	frames, unsub := h.b.Subscribe()
	defer unsub()
	_ = drain(t, frames, "stream", time.Second)
	if err := h.b.BindWith("/sock", "sess-A", "1@t", nil); err != nil {
		t.Fatal(err)
	}
	h.waitStart(1)
	h.release[1] <- func(context.Context, uint64, string) error { return errors.New(`no conversation "sess-A"`) }
	h.waitStart(1)
	got := drainAll(frames)
	if len(got) != 1 || !strings.Contains(string(got[0].Data), `"empty":true`) {
		t.Errorf("빈 대화 알림: %v", got)
	}
	if _, empty, _, _ := h.state(); !empty {
		t.Error("empty 가 안 섰다")
	}
	h.release[1] <- func(context.Context, uint64, string) error { return context.Canceled }
}

// 동시에 온 Bind 둘 — 남는 일꾼과 cancel 의 주인은 마지막으로 선 세대 하나다. 진 쪽의 ctx 는 취소돼 있다.
func TestConcurrentBindsLeaveOneWorker(t *testing.T) {
	h := newGenHarness(t)
	var wg sync.WaitGroup
	gate := make(chan struct{})
	for _, sid := range []string{"sess-A", "sess-B"} {
		wg.Add(1)
		go func(sid string) {
			defer wg.Done()
			<-gate
			_ = h.b.BindWith("/sock", sid, "1@t", nil)
		}(sid)
	}
	close(gate)
	wg.Wait()

	h.b.mu.Lock()
	final, session := h.b.gen, h.b.session
	h.b.mu.Unlock()
	if final != 2 {
		t.Fatalf("세대가 %d — Bind 둘이면 2", final)
	}
	win := h.waitStart(final)
	loser := final - 1
	// 진 쪽은 read 를 시작했을 수도(그 ctx 는 취소돼 있어야 한다), 시작도 못 하고 끝났을 수도 있다.
	select {
	case ctx := <-h.started[loser]:
		if ctx.Err() == nil {
			t.Error("진 쪽의 일꾼이 취소되지 않은 채 돈다")
		}
		h.release[loser] <- func(context.Context, uint64, string) error { return context.Canceled }
	case <-h.done[loser]:
	case <-time.After(3 * time.Second):
		t.Fatal("진 쪽의 일꾼이 시작도 끝도 안 했다")
	}
	h.waitDone(loser)
	if win.Err() != nil {
		t.Fatalf("이긴 쪽(%s)의 일꾼이 취소됐다", session)
	}
	// cancel 의 주인이 이긴 쪽인가: Stop 이 부르는 cancel 이 그 ctx 를 끊어야 한다.
	h.b.Stop()
	select {
	case <-win.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop 이 이긴 쪽의 일꾼을 못 끊었다 — cancel 이 다른 세대의 것이다")
	}
}

// 세대 검사는 ctx 취소를 보조하는 것이 아니라 **그 자체로** 막아야 한다: 같은 대화, 살아 있는 ctx, 세대만 옛것.
// (지금의 BindWith 는 옛 ctx 를 같은 락 안에서 먼저 끊으므로, 다른 시험에서는 ctx 검사만으로도 막힌다 — 그래서 세대
// 검사를 빼는 변이를 그 시험들이 못 잡았다. 이 시험은 세대 하나로만 갈리는 경우를 직접 만든다.)
func TestAStaleGenerationIsRefusedEvenWithALiveContext(t *testing.T) {
	b := NewBridge()
	b.mu.Lock()
	b.session, b.gen, b.lastSeq = "sess-A", 3, -1
	b.mu.Unlock()
	ch, unsub := b.Subscribe()
	defer unsub()
	_ = drainAll(ch)
	live := context.Background()
	if b.forGen(live, 2, func() []StreamFrame { b.live = true; return []StreamFrame{{Kind: "stream", Data: []byte(`{"live":true}`)}} }) {
		t.Error("옛 세대가 같은 대화·살아 있는 ctx 로 상태를 바꿨다")
	}
	if b.deliver(live, 2, "sess-A", StreamFrame{Kind: "event", Data: []byte(`{"seq":9}`)}, 9) {
		t.Error("옛 세대의 사건이 같은 대화라는 이유로 들어갔다")
	}
	if l, _, seq, hist := (&genHarness{b: b}).state(); l || seq != -1 || hist != 0 {
		t.Errorf("옛 세대가 남긴 것: live=%v seq=%d history=%d", l, seq, hist)
	}
	if got := drainAll(ch); len(got) != 0 {
		t.Errorf("옛 세대의 알림이 창에 왔다: %v", got)
	}
	if !b.forGen(live, 3, func() []StreamFrame { return nil }) {
		t.Error("지금 세대를 막았다")
	}
}
