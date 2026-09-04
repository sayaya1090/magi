package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/daemon"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// 채팅창의 뒤쪽 — 방향이 둘이다(DESIGN.md §5.7).
//
// 여기까지 이 헬퍼는 한 방향만 했다: 모델 → magi → MCP → 헬퍼 → 애드인 → 덱. 그런데 사용자가
// 말을 거는 자리는 애드인이다. 그러면 방향이 하나 더 생긴다: 사람 → 애드인 → 헬퍼 → 데몬 → 모델.
//
// **새 프로토콜은 안 만든다 — 문에 이미 있다.** 데몬의 `dispatchNow` 가 `submit`·`steer`·
// `interrupt`·`permission`·`answer` 를 받고, 대화는 `transcript` 로 흘러나온다. 우리가 할 일은
// 그 둘을 애드인까지 나르는 것뿐이다.
//
// # 연결이 둘이다
//
// `transcript` 는 `watch` 와 같은 모양이라 **연결을 통째로 가져간다**: 클라이언트 쪽
// `Client.Transcript` 가 다른 모든 호출이 한 왕복만 잡는 그 뮤텍스를 읽는 내내 쥔다. 그러니 같은
// `Client` 로 `status` 를 부르면 거절도 에러도 아니고 **그냥 안 돌아온다** — `Dial` 은 데드라인을
// 안 걸어서 영영. 그래서 헬퍼는 소켓에 **두 번** 붙는다(§5.7 ⚠).
//
// 그 대가로 하나가 따라온다: **락스텝 연결이 멀쩡한 것은 스트림이 살아 있다는 증거가 아니다.**
// 화면이 그 둘을 하나로 그리면 「보냈습니다」와 「보냈는데 확인은 못 합니다」가 같은 문장이 된다.

// Bridge 는 고른 컴패니언 하나에 붙어 있는 상태.
type Bridge struct {
	mu      sync.Mutex
	socket  string
	session string
	// live 는 대화 스트림이 살아 있는가. **요청 쪽 연결과 따로 센다** — 서로의 생존 증거가 아니다.
	live      bool
	lastSeq   int64
	listeners map[chan StreamFrame]struct{}
	cancel    context.CancelFunc
	stopped   bool
}

func NewBridge() *Bridge { return &Bridge{listeners: map[chan StreamFrame]struct{}{}} }

// Bound 는 지금 어느 컴패니언에 붙어 있는가. 안 붙었으면 빈 문자열.
func (b *Bridge) Bound() (socket, sid string, streamLive bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.socket, b.session, b.live
}

// Subscribe 는 애드인 스트림 하나를 대화에 붙인다. 돌려주는 것은 끊는 함수.
func (b *Bridge) Subscribe() (<-chan StreamFrame, func()) {
	ch := make(chan StreamFrame, 64)
	b.mu.Lock()
	b.listeners[ch] = struct{}{}
	live, sid := b.live, b.session
	b.mu.Unlock()
	// 붙자마자 **지금 상태를 한 번 말한다.** 「스트림이 끊겼습니다」를 부팅 화면이 단언하던
	// 결함이 목업에 실제로 있었다(README): `detach()` 는 알리는데 `attach()` 는 안 알려서,
	// 빈 대화에 붙으면 붙기 전에 그린 그림이 영영 서 있었다. **비대칭 통지는 거짓말 생성기다.**
	ch <- StreamFrame{Kind: "stream", Data: json.RawMessage(mustJSON(map[string]any{"live": live, "session": sid}))}
	return ch, func() {
		b.mu.Lock()
		delete(b.listeners, ch)
		b.mu.Unlock()
	}
}

func (b *Bridge) push(f StreamFrame) {
	b.mu.Lock()
	targets := make([]chan StreamFrame, 0, len(b.listeners))
	for ch := range b.listeners {
		targets = append(targets, ch)
	}
	b.mu.Unlock()
	for _, ch := range targets {
		select {
		case ch <- f:
		default:
			// 안 받아 가는 창은 **버린다.** 여기서 막히면 다른 창까지 같이 선다.
		}
	}
}

// Bind 는 컴패니언 하나를 고르고 대화 스트림을 연다.
//
// 대화가 바뀌면 **커서를 -1 로 되돌린다**(§5.7). 옛 커서를 새 대화에 들고 가면 그 대화의 앞을
// 못 본다 — 와이어에는 숫자만 있고 어느 로그에서 센 숫자인지가 안 실려 오므로, 세션 id 를 seq
// 옆에 같이 들고 있는 것은 **클라이언트 몫**이라고 문이 스스로 적어 뒀다.
func (b *Bridge) Bind(socket, sid string) error {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	if b.session != sid {
		b.lastSeq = -1
	}
	b.socket, b.session, b.live = socket, sid, false
	b.stopped = false
	b.mu.Unlock()

	if sid == "" {
		return errors.New("이 컴패니언은 아직 대화가 없습니다 — 한 번 말을 걸면 생깁니다")
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.mu.Lock()
	b.cancel = cancel
	b.mu.Unlock()
	go b.stream(ctx, socket, sid)
	return nil
}

// Stop 은 붙어 있던 것을 놓는다.
func (b *Bridge) Stop() {
	b.mu.Lock()
	b.stopped = true
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
	b.mu.Unlock()
}

// stream 은 대화를 읽어 애드인으로 흘린다. 끊기면 다시 붙는다(§5.4 — 하트비트를 두지 않는다).
func (b *Bridge) stream(ctx context.Context, socket, sid string) {
	backoff := 200 * time.Millisecond
	for ctx.Err() == nil {
		b.mu.Lock()
		since := b.lastSeq
		b.mu.Unlock()

		err := b.readOnce(ctx, socket, sid, since)
		b.mu.Lock()
		wasLive := b.live
		b.live = false
		b.mu.Unlock()
		if wasLive {
			b.push(StreamFrame{Kind: "stream", Data: json.RawMessage(`{"live":false}`)})
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			b.push(StreamFrame{Kind: "note", Data: json.RawMessage(mustJSON(map[string]any{
				"note": "대화 스트림이 끊겼습니다: " + err.Error(),
			}))})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 5*time.Second {
			backoff *= 2
		}
	}
}

func (b *Bridge) readOnce(ctx context.Context, socket, sid string, since int64) error {
	cl, err := daemon.Dial(socket)
	if err != nil {
		return err
	}
	defer cl.Close()
	go func() {
		<-ctx.Done()
		cl.Close() // 읽는 중인 연결을 끊는 유일한 길
	}()

	b.mu.Lock()
	b.live = true
	b.mu.Unlock()
	b.push(StreamFrame{Kind: "stream", Data: json.RawMessage(mustJSON(map[string]any{"live": true, "session": sid}))})

	return cl.Transcript(sid, since, func(why string) {
		// **서버가 커서를 거절하면 이벤트보다 먼저 이 프레임 하나가 온다**(`answerable`).
		// 안 읽으면 화면에 있던 대화 뒤에 같은 대화의 처음이 붙는다 — 작업창은 PowerPoint 를
		// 껐다 켤 때마다 새로 붙으므로 이 프레임을 제일 자주 받는 쪽이 우리다(§5.7).
		b.mu.Lock()
		b.lastSeq = -1
		b.mu.Unlock()
		b.push(StreamFrame{Kind: "restart", Data: json.RawMessage(mustJSON(map[string]any{"why": why}))})
	}, func(ev event.Event) bool {
		// **커서를 미는 것은 `seq > 0` 인 이벤트뿐이다**(§5.7). 로그에 안 앉는 이벤트는 `Seq`
		// 가 0 이고, 0 은 이 문의 계약에서 「전부」다 — 그대로 커서에 넣으면 다시 붙을 때 화면이
		// 두 벌이 되고, 그 사고가 **아무 소리 없이** 일어난다.
		if ev.Seq > 0 {
			b.mu.Lock()
			b.lastSeq = ev.Seq
			b.mu.Unlock()
		}
		b.push(StreamFrame{Kind: "event", Data: json.RawMessage(mustJSON(ev))})
		return true
	})
}

// call 은 **요청용 연결**을 새로 열어 한 왕복을 돈다.
//
// 스트림과 같은 클라이언트를 쓰면 안 돌아온다(위 주석). 매번 여는 값은 소켓 하나이고, 그건
// 콘솔이 모델을 부르는 호출에 대해 이미 내린 것과 같은 결정이다.
func (b *Bridge) call(fn func(*daemon.Client) error) error {
	socket, _, _ := b.Bound()
	if socket == "" {
		return errors.New("아직 어느 컴패니언에도 안 붙었습니다")
	}
	cl, err := daemon.Dial(socket)
	if err != nil {
		return fmt.Errorf("컴패니언에 못 닿았습니다: %w", err)
	}
	defer cl.Close()
	return fn(cl)
}

// Submit 은 사람이 친 말을 낸다. **던지고 즉시 돌아온다**(§5.7 — 기다리면 교착이 난다).
//
// 화면은 낸 말을 **미리 안 붙인다.** `submit` 은 아무 식별자도 안 돌려주고(응답에 seq 도
// messageId 도 없다) 밖에서 붙은 창은 전부 `actor.id = "attach"` 로 찍히므로, 로그에 올라온
// 말이 내 것인지 옆 창 것인지 값으로 못 가린다. 그래서 지우는 것은 로그의 메아리다.
func (b *Bridge) Submit(text string) error {
	_, sid, _ := b.Bound()
	if sid == "" {
		return errors.New("아직 대화가 없습니다")
	}
	return b.call(func(cl *daemon.Client) error {
		return cl.Submit(context.Background(), command.SubmitPrompt{
			SessionID: session.SessionID(sid),
			Parts:     []session.Part{{Kind: session.PartText, Text: text}},
		})
	})
}

// Steer 는 도는 중에 끼어든다.
func (b *Bridge) Steer(text string) error {
	_, sid, _ := b.Bound()
	if sid == "" {
		return errors.New("아직 대화가 없습니다")
	}
	return b.call(func(cl *daemon.Client) error {
		return cl.Steer(context.Background(), command.SubmitPrompt{
			SessionID: session.SessionID(sid),
			Parts:     []session.Part{{Kind: session.PartText, Text: text}},
		})
	})
}

// Interrupt 는 멈춘다.
func (b *Bridge) Interrupt() error {
	_, sid, _ := b.Bound()
	return b.call(func(cl *daemon.Client) error {
		return cl.Interrupt(context.Background(), command.Interrupt{SessionID: session.SessionID(sid)})
	})
}

// Status 는 **폴이다, 스트림이 아니다**(§5.7).
//
// 물음은 로그에 안 쌓이고 막힌 데몬의 버스에만 나가므로 `transcript` 를 아무리 붙들고 있어도
// 안 온다. 어태치 TUI 가 `status` 를 두드리는 이유가 그것이고, 밖에서 그리는 창이면 다 하는 일이다.
func (b *Bridge) Status() (map[string]any, error) {
	_, sid, live := b.Bound()
	out := map[string]any{"streamLive": live, "session": sid}
	err := b.call(func(cl *daemon.Client) error {
		st, err := cl.Status(sid)
		if err != nil {
			return err
		}
		out["reachable"] = true
		out["doing"] = st.Doing
		out["permission"] = st.Permission
		out["backend"] = st.Backend
		out["model"] = st.Model
		// **카운슬이 있는지**. 도구 설명문이 이 값을 보고 문장을 고른다(`tools.go` 의 `declare`) —
		// 없는 도구를 이름으로 부르면 모델이 그것을 부르고 `unknown tool` 을 받는다.
		out["council"] = st.Council
		if st.Asking != nil {
			out["asking"] = st.Asking
		}
		return nil
	})
	if err != nil {
		// **못 닿은 것과 「묻는 게 없다」는 값이 같으면 안 된다**(§5.7). 앞엣것은 모르는
		// 것이고 뒤엣것은 아는 것이다.
		out["reachable"] = false
		out["why"] = err.Error()
	}
	return out, nil
}

// AnswerPermission 은 호출 하나에 대한 허락. **대화 턴이 아니다** — 답은 호출에 붙고 턴이 도는
// 중에 나가야 하므로 채팅 제출과 다른 손이다.
func (b *Bridge) AnswerPermission(callID, decision string) error {
	if callID == "" {
		return errors.New("어느 호출에 대한 답인지가 없습니다")
	}
	switch decision {
	case "allow", "deny", "always":
	default:
		// 보낼 수 있는 낱말이 정해져 있다. 지어낸 값을 보내면 코어가 **틀린 사유**로 떨어뜨린다.
		return fmt.Errorf("%q 는 보낼 수 있는 결정이 아닙니다(allow · deny · always)", decision)
	}
	_, sid, _ := b.Bound()
	return b.call(func(cl *daemon.Client) error {
		return cl.RespondPermission(context.Background(), command.RespondPermission{
			SessionID: session.SessionID(sid), CallID: callID, Decision: decision,
		})
	})
}

// AnswerQuestion 은 모델이 물은 것에 대한 답. **권한과 손이 다르다** — 권한은 정해진 낱말이고
// 질문은 사람이 고른 글이다. 한 손으로 합치면 질문에 `allow` 를 보낼 수 있게 된다.
func (b *Bridge) AnswerQuestion(callID, text string) error {
	if callID == "" {
		return errors.New("어느 물음에 대한 답인지가 없습니다")
	}
	_, sid, _ := b.Bound()
	return b.call(func(cl *daemon.Client) error {
		return cl.RespondQuestion(context.Background(), command.RespondQuestion{
			SessionID: session.SessionID(sid), CallID: callID, Answer: text,
		})
	})
}
