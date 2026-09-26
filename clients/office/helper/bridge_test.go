package office

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// talkEngine 은 대화를 내주는 엔진 — `Transcriber` 를 만족한다. door 엔진과 갈라 두는 이유는
// §5.0.5 그대로다: 두 cap 은 **등급이 다르다.** 도구를 못 붙이면 못 고르고, 대화를 못 내주면
// 고를 수는 있고 채팅창만 못 뜬다.
type talkEngine struct {
	doorEngine

	tmu    sync.Mutex
	log    []event.Event
	subs   []talkSub
	closed bool
}

// talkSub 는 구독 하나 — **어느 대화의** 구독인지를 같이 든다.
type talkSub struct {
	sid session.SessionID
	ch  chan event.Event
}

// Subscribe 는 **ctx 가 끝나면 채널을 닫아야 한다.** 안 닫으면 데몬의 전사 핸들러가
// `for e := range evs` 에서 영영 서고, 그러면 `Serve` 의 `wg.Wait` 이 안 끝나 시험이 통째로
// 매달린다 — 실제로 그렇게 45초를 매달렸고, 그 매달림이 이 주석의 근거다. 진짜 스토어는 그
// 계약을 지키므로, 안 지키는 가짜로 재는 것은 **없는 세상을 재는 것**이다.
//
// ⚠ **같은 이유로 대화 id 를 본다.** 이 가짜가 id 를 버리고 로그 전체를 되풀이하던 때, 「다른 대화로
// 옮기면 되풀이할 것도 바뀐다」를 재는 시험이 **옮긴 뒤마다** 옛 대화의 사건을 받았다(실측 2026-09-26: 뒤
// 검사를 넓히니 20/20). 진짜 스토어는 대화별로 준다 — 가짜만 대화를 섞는 세상에서 재던 것이다(#201).
func (e *talkEngine) Subscribe(ctx context.Context, sid session.SessionID, fromSeq int64) (<-chan event.Event, func(), error) {
	e.tmu.Lock()
	ch := make(chan event.Event, 64)
	for _, ev := range e.log {
		if ev.SessionID != sid {
			continue
		}
		// 스토어의 규칙 그대로: `fromSeq > 0` 일 때만 자른다. **0 도 음수도 「전부」다.**
		if fromSeq > 0 && ev.Seq <= fromSeq {
			continue
		}
		ch <- ev
	}
	e.subs = append(e.subs, talkSub{sid: sid, ch: ch})
	e.tmu.Unlock()

	go func() {
		<-ctx.Done()
		e.drop(ch)
	}()
	return ch, func() { e.drop(ch) }, nil
}

// drop 은 구독 하나를 목록에서 빼고 닫는다. **빼고 나서 닫는다** — emit 은 락을 쥔 채 보내므로,
// 순서가 그러면 닫힌 채널로 보내는 일이 없다.
func (e *talkEngine) drop(ch chan event.Event) {
	e.tmu.Lock()
	kept := e.subs[:0]
	found := false
	for _, s := range e.subs {
		if s.ch == ch {
			found = true
			continue
		}
		kept = append(kept, s)
	}
	e.subs = kept
	e.tmu.Unlock()
	if found {
		close(ch)
	}
}

func (e *talkEngine) NewSince(_ context.Context, sid session.SessionID, _ int64) (int64, bool, error) {
	e.tmu.Lock()
	defer e.tmu.Unlock()
	var latest int64
	known := false
	for _, ev := range e.log {
		if ev.SessionID != sid {
			continue
		}
		known = true
		if ev.Seq > latest {
			latest = ev.Seq
		}
	}
	return latest, known, nil
}

// emit 은 사실 하나를 로그에 앉히고 **그 대화의** 구독에만 흘린다.
func (e *talkEngine) emit(ev event.Event) {
	e.tmu.Lock()
	defer e.tmu.Unlock()
	if ev.Seq > 0 {
		e.log = append(e.log, ev)
	}
	for _, s := range e.subs {
		if s.sid != ev.SessionID {
			continue
		}
		select {
		case s.ch <- ev:
		default:
		}
	}
}

func drain(t *testing.T, ch <-chan StreamFrame, want string, timeout time.Duration) StreamFrame {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case f := <-ch:
			if want == "" || f.Kind == want {
				return f
			}
		case <-deadline:
			t.Fatalf("%q 프레임이 %v 안에 안 왔다", want, timeout)
		}
	}
}

// 대화가 애드인까지 흐른다 — 그리고 **붙자마자 지금 상태를 한 번 말한다.**
//
// 목업이 겪은 결함이 그 자리다(README): `detach()` 는 알리는데 `attach()` 는 안 알려서, 빈
// 대화에 붙으면 「스트림이 끊겼습니다」가 영영 서 있었다. **비대칭 통지는 거짓말 생성기다.**
func TestTheTranscriptReachesTheAddin(t *testing.T) {
	dir := shortDir(t)
	eng := &talkEngine{}
	sock, _ := startDaemon(t, dir, "talk", eng)

	b := NewBridge()
	defer b.Stop()
	frames, unsub := b.Subscribe()
	defer unsub()

	// 붙기 전의 첫 프레임은 「아직 안 붙었다」다.
	first := drain(t, frames, "stream", time.Second)
	if strings.Contains(string(first.Data), `"live":true`) {
		t.Fatalf("안 붙었는데 살아 있다고 한다: %s", first.Data)
	}

	if err := b.Bind(sock, "sess-talk"); err != nil {
		t.Fatal(err)
	}
	live := drain(t, frames, "stream", 3*time.Second)
	if !strings.Contains(string(live.Data), `"live":true`) {
		t.Fatalf("붙었는데 %s 라고 한다", live.Data)
	}

	eng.emit(event.Event{Seq: 7, Type: event.TypePromptSubmitted, SessionID: "sess-talk"})
	got := drain(t, frames, "event", 3*time.Second)
	var ev event.Event
	if err := json.Unmarshal(got.Data, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Seq != 7 {
		t.Fatalf("이벤트가 %+v 다", ev)
	}
}

// **커서를 미는 것은 `seq > 0` 인 이벤트뿐이다**(§5.7).
//
// 로그에 안 앉는 이벤트는 `Seq` 가 0 이고, 0 은 이 문의 계약에서 「전부」다. 그대로 커서에 넣으면
// 다시 붙을 때 화면이 두 벌이 되는데, 그 사고가 **아무 소리 없이** 일어난다.
func TestOnlyASeatedEventMovesTheCursor(t *testing.T) {
	dir := shortDir(t)
	eng := &talkEngine{}
	sock, _ := startDaemon(t, dir, "cur", eng)

	b := NewBridge()
	defer b.Stop()
	frames, unsub := b.Subscribe()
	defer unsub()
	if err := b.Bind(sock, "sess-cur"); err != nil {
		t.Fatal(err)
	}
	drain(t, frames, "stream", 3*time.Second)

	eng.emit(event.Event{Seq: 4, Type: event.TypePromptSubmitted, SessionID: "sess-cur"})
	drain(t, frames, "event", 3*time.Second)
	// 버스 전용 이벤트 — 자리가 없다.
	eng.emit(event.Event{Seq: 0, Type: event.TypePartDelta, SessionID: "sess-cur"})
	drain(t, frames, "event", 3*time.Second)

	b.mu.Lock()
	got := b.lastSeq
	b.mu.Unlock()
	if got != 4 {
		t.Fatalf("커서가 %d 다 — 자리 없는 이벤트가 밀었다", got)
	}
}

// **연결이 둘이다**(§5.7 ⚠).
//
// `transcript` 는 연결을 통째로 가져가므로, 같은 클라이언트로 `status` 를 부르면 거절도 에러도
// 아니고 **그냥 안 돌아온다.** 그래서 요청은 새 연결로 돈다 — 이 시험은 스트림이 흐르는 동안
// 상태가 답하는지를 잰다.
func TestStatusAnswersWhileTheTranscriptIsStreaming(t *testing.T) {
	dir := shortDir(t)
	eng := &talkEngine{}
	sock, _ := startDaemon(t, dir, "two", eng)

	b := NewBridge()
	defer b.Stop()
	frames, unsub := b.Subscribe()
	defer unsub()
	if err := b.Bind(sock, "sess-two"); err != nil {
		t.Fatal(err)
	}
	drain(t, frames, "stream", 3*time.Second)

	done := make(chan map[string]any, 1)
	go func() {
		st, _ := b.Status()
		done <- st
	}()
	select {
	case st := <-done:
		if st["reachable"] != true {
			t.Fatalf("스트림이 도는 동안 상태가 %v 다", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("상태가 안 돌아왔다 — 스트림과 같은 연결을 쓰고 있다")
	}
}

// 낸 말이 데몬에 도착한다. **그리고 답은 이 왕복이 아니라 스트림으로 온다** — `submit` 은
// 받았다는 것만 답한다.
func TestSubmitReachesTheDaemonAndAnswersNothingElse(t *testing.T) {
	dir := shortDir(t)
	eng := &talkEngine{}
	sock, _ := startDaemon(t, dir, "sub", eng)

	b := NewBridge()
	defer b.Stop()
	if err := b.Bind(sock, "sess-sub"); err != nil {
		t.Fatal(err)
	}
	if err := b.Submit("슬라이드 3 제목 줄여 줘"); err != nil {
		t.Fatal(err)
	}
}

// 권한의 답과 질문의 답은 **다른 손이다**(§5.7). 한 손으로 합치면 질문에 `allow` 를 보낼 수 있게 된다.
func TestAPermissionDecisionIsOneOfThreeWords(t *testing.T) {
	b := NewBridge()
	if err := b.AnswerPermission("c1", "yes"); err == nil {
		t.Fatal("지어낸 낱말이 통과했다")
	} else if !strings.Contains(err.Error(), "allow") {
		t.Errorf("무엇을 보낼 수 있는지가 안 적혔다: %v", err)
	}
	if err := b.AnswerPermission("", "allow"); err == nil {
		t.Fatal("어느 호출인지 없이 통과했다")
	}
}

// 첫 말 전의 대화는 코어 저장소에 없어서 전사 문이 「없는 대화」라 답한다. 그것은 끊김이 아니다:
// 창에는 empty 프레임 한 번, 「끊겼습니다」 쪽지는 없음, 그리고 조용히 다시 붙어 본다. 실물
// 2026-09-05: 데몬 재기동 뒤 새 대화로 붙은 창 둘이 「대화 스트림이 끊겼습니다」를 띄웠다.
func TestAnEmptyConversationIsNotABrokenStream(t *testing.T) {
	b := NewBridge()
	defer b.Stop()
	var calls int32
	b.read = func(ctx context.Context, _, _ string, _ int64) error {
		atomic.AddInt32(&calls, 1)
		return errors.New(`no conversation "s_x" in this workspace — ` + "`sessions`" + ` lists them`)
	}
	frames, unsub := b.Subscribe()
	defer unsub()
	drain(t, frames, "stream", time.Second) // the initial "not live"
	if err := b.BindWith("/sock", "s_x", "1@t0", nil); err != nil {
		t.Fatal(err)
	}
	f := drain(t, frames, "stream", 3*time.Second)
	if !strings.Contains(string(f.Data), `"empty":true`) {
		t.Fatalf("빈 대화를 끊김으로 적었다: %s", f.Data)
	}
	if !b.Empty() {
		t.Fatal("Empty() 가 거짓이다")
	}
	deadline := time.After(1500 * time.Millisecond)
	for {
		select {
		case fr := <-frames:
			if fr.Kind == "note" {
				t.Fatalf("빈 대화에 「끊겼습니다」 쪽지를 냈다: %s", fr.Data)
			}
			if fr.Kind == "stream" && strings.Contains(string(fr.Data), `"empty":true`) {
				t.Fatalf("empty 를 되풀이해 말했다: %s", fr.Data)
			}
		case <-deadline:
			if atomic.LoadInt32(&calls) < 2 {
				t.Fatalf("다시 붙어 보지 않았다(read %d회)", calls)
			}
			return
		}
	}
}

// 헬퍼가 넘긴 권한 답은 기억된다 — 창이 「무엇으로 답했는지 모른다」 대신 결정을 적기 위해.
// 보낼 수 없는 결정은 기억되지 않는다.
func TestTheLastPermissionAnswerIsRemembered(t *testing.T) {
	b := NewBridge()
	if id, _ := b.LastAnswer(); id != "" {
		t.Fatalf("처음부터 답이 있다: %q", id)
	}
	if err := b.AnswerPermission("call_1", "maybe"); err == nil {
		t.Fatal("보낼 수 없는 결정을 받았다")
	}
	if id, _ := b.LastAnswer(); id != "" {
		t.Fatalf("거절된 답을 기억했다: %q", id)
	}
}

// 늦게 붙는 창은 그 대화의 앞을 받는다 — 붙은 뒤의 프레임만 흘리면 창을 다시 열 때마다 대화가
// 빈 채로 시작한다(사용자 지적 2026-09-05). 다른 대화로 옮기면 되풀이할 것도 바뀐다.
func TestALateSubscriberGetsTheConversationSoFar(t *testing.T) {
	dir := shortDir(t)
	eng := &talkEngine{}
	sock, _ := startDaemon(t, dir, "talk", eng)
	b := NewBridge()
	defer b.Stop()
	first, unsub1 := b.Subscribe()
	defer unsub1()
	if err := b.Bind(sock, "sess-hist"); err != nil {
		t.Fatal(err)
	}
	drain(t, first, "stream", 3*time.Second)
	for i := int64(1); i <= 3; i++ {
		eng.emit(event.Event{Seq: i, Type: event.TypePromptSubmitted, SessionID: "sess-hist"})
		drain(t, first, "event", 3*time.Second)
	}
	late, unsub2 := b.Subscribe()
	defer unsub2()
	drain(t, late, "stream", time.Second)
	var seqs []int64
	for i := 0; i < 3; i++ {
		f := drain(t, late, "event", time.Second)
		var ev event.Event
		_ = json.Unmarshal(f.Data, &ev)
		seqs = append(seqs, ev.Seq)
	}
	if fmt.Sprint(seqs) != "[1 2 3]" {
		t.Fatalf("늦게 붙은 쪽이 앞을 순서대로 못 받았다: %v", seqs)
	}
	if err := b.Bind(sock, "sess-other"); err != nil {
		t.Fatal(err)
	}
	again, unsub3 := b.Subscribe()
	defer unsub3()
	drain(t, again, "stream", time.Second)
	// **온 것을 다 본다 — 첫 하나가 아니라.** 이 자리가 프레임 하나만 받고 끝나던 때는, 그 하나가 거의 늘
	// 「live」 알림이라 그 뒤에 따라오는 옛 대화의 사건을 한 번도 안 봤다. 120번에 한 번 깨진 것은 스케줄이
	// 사건을 알림보다 앞에 놓은 드문 경우였고, 새는 것 자체는 매번이었다(#201, 2026-09-26).
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case f := <-again:
			if f.Kind == "event" {
				t.Fatalf("다른 대화로 옮겼는데 옛 대화의 프레임을 되풀이했다: %s", f.Data)
			}
			continue
		case <-deadline:
		}
		break
	}
}

// 옮겨 간 뒤에 옛 스트림이 늦게 읽은 것은 새 대화에 안 섞인다 — 기록에도, 창에도, 커서에도.
//
// ⚠ 옮기면 옛 스트림은 ctx 가 취소될 뿐이고 연결은 따로 닫힌다. 그 틈에 이미 읽힌 옛 대화의 사건이 콜백을
// 타면, 막는 자리가 없던 때는 새 대화의 history 에 붙고 **새 스트림의 커서를 옛 번호로 덮었다** — 새 대화를
// 앞이 빠진 채 읽게 되는 쪽이 더 나쁘다(#201, 2026-09-26). 시간에 기대지 않고 그 틈을 직접 만든다.
func TestAStreamThatWasReboundAwayDeliversNothing(t *testing.T) {
	b := NewBridge()
	b.mu.Lock()
	b.session, b.lastSeq = "sess-new", -1
	b.mu.Unlock()
	ch, unsub := b.Subscribe()
	defer unsub()
	drain(t, ch, "stream", time.Second)

	ev := StreamFrame{Kind: "event", Data: json.RawMessage(`{"seq":7,"sessionId":"sess-old"}`)}
	if b.deliver(context.Background(), "sess-old", ev, 7) {
		t.Fatal("옛 대화의 스트림이 계속 읽으라는 답을 받았다")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if b.deliver(cancelled, "sess-new", ev, 7) {
		t.Fatal("취소된 스트림이 계속 읽으라는 답을 받았다")
	}
	b.mu.Lock()
	seq, hist := b.lastSeq, len(b.history)
	b.mu.Unlock()
	if seq != -1 || hist != 0 {
		t.Fatalf("떨어진 스트림이 새 묶음을 건드렸다: 커서 %d, 기록 %d개", seq, hist)
	}
	select {
	case f := <-ch:
		t.Fatalf("떨어진 스트림의 프레임이 창에 왔다: %s %s", f.Kind, f.Data)
	case <-time.After(50 * time.Millisecond):
	}

	// 지금의 묶음이면 그대로 간다 — 막는 자리가 전부 막는 것은 아니어야 한다.
	if !b.deliver(context.Background(), "sess-new", ev, 7) {
		t.Fatal("지금 묶인 대화의 프레임을 막았다")
	}
	if f := drain(t, ch, "event", time.Second); string(f.Data) != string(ev.Data) {
		t.Fatalf("다른 프레임이 왔다: %s", f.Data)
	}
	b.mu.Lock()
	seq = b.lastSeq
	b.mu.Unlock()
	if seq != 7 {
		t.Fatalf("커서가 안 밀렸다: %d", seq)
	}
}
