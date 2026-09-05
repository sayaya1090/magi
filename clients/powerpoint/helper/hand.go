package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"
)

// 손 — 붙어 있는 애드인들, 그리고 그들에게 조작을 넘기는 자리(DESIGN.md §5.5·§5.6).
//
// # 방향
//
// 헬퍼가 도구 호출을 애드인에게 **밀어야** 하는데 애드인은 리슨을 못 한다. 그래서 애드인이
// 먼저 붙어서 연결을 열어 두고, 호출은 그 연결을 **거슬러 내려간다.** 이 파일은 그 연결이
// 무엇으로 만들어졌는지를 모른다 — 채널만 안다. 전송은 `handhttp.go` 다.
//
// # 창이 둘이면 손도 둘이다
//
// 가르는 것은 이름이 아니라 `document` 인자다(§4.4 ④·§5.0.6). 이름으로 가르면 데몬 하나에
// `ppt-1`·`ppt-2` 가 붙고 **모델이 어느 쪽이 어느 덱인지를 이름만 보고** 판단해야 한다.
//
// # 잠금의 폭은 Office.js 호출 하나다 — 턴이 아니다
//
// 채팅 제출은 던지고 즉시 돌아오고(§5.7), 그 턴의 모델이 부르는 읽기 도구는 같은 헬퍼를
// 지난다. 「한 문서에 손은 하나」를 **요청 잠금**으로 구현하면 첫 요청이 두 번째를 기다리고
// 두 번째가 첫 번째를 기다린다. 그래서 여기서 잠그는 것은 호출 하나뿐이다.

// handCallTimeout 은 애드인이 답할 때까지 우리가 기다리는 시간이다.
//
// magi 쪽 천장이 60초고 그 값은 **줄어들기만 한다**(§4.4 ③ — `context.WithTimeout` 이 짧은
// 쪽을 택한다). 그래서 우리 값은 그보다 짧아야 의미가 있다: 우리가 먼저 끊으면 **누가 왜
// 끊었는지를 말할 수 있고**, magi 가 먼저 끊으면 모델이 받는 것은 `context deadline exceeded`
// 한 줄이라 「서버가 실패했다」로 읽힌다.
const handCallTimeout = 45 * time.Second

// handQueue 는 한 애드인에게 밀어 둘 수 있는 조작의 수. 넘치면 **거절한다** — 조용히 버리면
// 모델은 호출이 갔다고 믿고 기다린다.
const handQueue = 16

// HandRequest 는 애드인에게 내려가는 조작 하나.
type HandRequest struct {
	ID   string         `json:"id"`
	Op   string         `json:"op"`
	Args map[string]any `json:"args"`
}

// HandReply 는 애드인이 올려 보내는 답.
type HandReply struct {
	ID string `json:"id"`
	// Error 가 비어 있지 않으면 실패다. **애드인이 자기 말로 적는다** — 여기서 문장을 지어내면
	// Office.js 가 실제로 뭐라고 했는지가 사라진다.
	Error    string         `json:"error,omitempty"`
	Document string         `json:"document,omitempty"`
	Label    string         `json:"label,omitempty"`
	Result   map[string]any `json:"result,omitempty"`
	Changed  []string       `json:"changed,omitempty"`
	Epoch    int            `json:"epoch,omitempty"`
	Count    int            `json:"count,omitempty"`
}

// handConn 은 붙어 있는 애드인 하나.
type handConn struct {
	key   string // 헬퍼가 발급한 문서 키. 도구의 `document` 인자가 이 값이다
	label string // 사람이 부르는 이름(파일 이름). 키가 아니다(§5.6)
	// presentationID 는 애드인이 말한 `context.presentation.id`. **있으면 키의 재료가 되고
	// 없으면 우리가 발급한다**(§5.6 — S11 이 아직 안 닫혔다. 저장 안 된 덱 둘이 다른 값을
	// 받는지, 저장이 그 값을 바꾸는지를 문서가 안 적는다).
	presentationID string
	out            chan HandRequest
	seen           time.Time // 마지막으로 이 손이 무엇이든 말한 때. 활성 문서를 고르는 재료다

	mu      sync.Mutex
	waiting map[string]chan HandReply
	// busy 는 「이 문서에 손은 하나」다. 폭이 **호출 하나**인 이유는 이 파일 머리에 적었다.
	busy sync.Mutex
	// epoch 는 이 헬퍼 런 안에서만 뜻이 있는 수다. 헬퍼가 재시작하면 다른 값이 되고, 그때
	// 개정 쌍은 「안 바뀌었다」가 아니라 **「모른다」**로 답해야 한다.
	epoch int
	count int
	// answered 는 이 손이 **무엇이든 답한 적이 있는가**. 붙어 있다는 것과 일하고 있다는 것은
	// 다르고, 그 차이가 활성 문서를 고를 때 걸린다(아래 pick).
	answered bool
	// deaf 는 이 손이 **기다리다 놓친 호출 수**. 얼어붙은 작업창 하나가 모든 호출을 45초씩
	// 삼키는 것을 막는 재료다.
	deaf int
}

// HandHub 은 붙어 있는 손 전부.
type HandHub struct {
	mu    sync.Mutex
	conns map[string]*handConn
	next  int
	epoch int
	// Timeout 은 시험이 줄여 잡는다.
	Timeout time.Duration
	// Now 는 시험이 시계를 안 재게 주입한다.
	Now func() time.Time
}

func NewHandHub() *HandHub {
	return &HandHub{
		conns: map[string]*handConn{},
		// epoch 는 프로세스 런의 신원이다. 재시작하면 달라지고, 달라졌다는 것이 곧 「그 사이를
		// 아무도 못 봤다」는 사실이다.
		epoch: int(time.Now().UnixNano() & 0x7fffffff),
	}
}

func (h *HandHub) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *HandHub) timeout() time.Duration {
	if h.Timeout > 0 {
		return h.Timeout
	}
	return handCallTimeout
}

// Join 은 애드인 하나를 받아들이고 그 문서의 키를 발급한다.
//
// 키가 **경로가 아닌** 이유는 §5.6 이다: 저장 안 된 덱에는 경로가 없고, 없음의 표기가 API 마다
// 달라서(빈 문자열이거나 `null`) **저장 안 된 덱 둘이 같은 값을 준다.** 경로를 키로 삼으면
// 아무것도 공유하지 않는 두 덱이 한 손을 다투는 것으로 읽힌다.
func (h *HandHub) Join(presentationID, label string) *handConn {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 같은 프레젠테이션 id 로 다시 붙으면 **같은 키를 준다.** 작업창은 PowerPoint 를 껐다 켤
	// 때마다 새로 붙는데(§5.7), 그때마다 키가 바뀌면 모델이 들고 있던 `document` 가 죽는다.
	key := ""
	if presentationID != "" {
		key = "pid-" + presentationID
	} else {
		h.next++
		// 발급한 신원은 헬퍼가 재시작하면 사라진다. 그때 할 말은 「모른다」이지 「같은 문서」가
		// 아니다(§5.6) — 그래서 런 epoch 를 키에 섞어, 다시 뜬 헬퍼가 옛 키를 아는 척하지 않는다.
		key = "doc-" + strconv.Itoa(h.epoch) + "-" + strconv.Itoa(h.next)
	}
	c := h.conns[key]
	if c == nil {
		c = &handConn{key: key, out: make(chan HandRequest, handQueue), waiting: map[string]chan HandReply{}, epoch: h.epoch}
		h.conns[key] = c
	}
	c.presentationID = presentationID
	c.label = label
	c.seen = h.now()
	return c
}

// Leave 는 애드인이 사라졌을 때. **헬퍼는 애드인 없이도 산다**(§5.4) — 마지막 손이 없어져도
// 프로세스는 그대로고, 도구가 「붙어 있지 않다」로 실패할 뿐이다.
// Peek 은 보는 연결(role=viewer)이 볼 손을 돌려준다 — 붙지도 떼지도 않는다.
//
// 먼저 그 문서 키 그대로. 없으면 **답하는 손 중 가장 최근 것**이다. 2021 판에서 그 자리를 봤다
// (2026-09-05): 창은 자기 덱 이름을 태그(PowerPointApi 1.3)로 짓는데 그 호스트에는 태그 칸이 없어
// 빈 이름을 들고 오고, COM 손은 파일 경로 지문으로 붙는다 — 둘의 키가 같을 길이 없다. 키가 같아야만
// 보게 하면 2021 의 창은 영영 아무것도 못 본다. 손이 하나도 없으면 nil 이다 — 볼 것이 없다.
//
// 보는 것은 고치는 것이 아니라서 pick 의 「둘이 똑같이 좋으면 안 고른다」는 여기 없다 — 화면이
// 옆 덱의 전사를 보는 것은 헛일이지 사고가 아니다.
func (h *HandHub) Peek(presentationID string) *handConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	if presentationID != "" {
		if c := h.conns["pid-"+presentationID]; c != nil {
			return c
		}
	}
	return h.bestLocked()
}

// bestLocked 은 답하는 손 중 가장 최근 것. pick 과 같은 차례다. 잠근 채로 부른다.
func (h *HandHub) bestLocked() *handConn {
	var best *handConn
	for _, c := range h.conns {
		if best == nil || rankConn(c) < rankConn(best) || (rankConn(c) == rankConn(best) && c.seen.After(best.seen)) {
			best = c
		}
	}
	return best
}

// rankConn 은 손을 고르는 차례: ① 답한 적 있고 안 놓친 손 → ② 아직 아무 말도 안 해 본 손 → ③ 놓친 적 있는 손.
// 사연은 pick 에 있다.
func rankConn(c *handConn) int {
	deaf, answered := c.record()
	switch {
	case answered && deaf == 0:
		return 0
	case !answered && deaf == 0:
		return 1
	default:
		return 2
	}
}

// record 는 이 손의 전적(놓친 수·답한 적)을 **잠그고** 읽는다. 둘 다 `mu` 아래에서 쓰이는데
// (deliver 가 answered 를, 시간 초과가 deaf 를), 고르는 쪽은 허브 락만 쥐고 읽고 있었다 —
// -race 가 그 자리를 잡았다(CI 2026-09-06). 허브 락은 목록을 지키지 손의 칸을 지키지 않는다.
func (c *handConn) record() (deaf int, answered bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deaf, c.answered
}

func (h *HandHub) Leave(c *handConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.conns[c.key]; ok && cur == c {
		delete(h.conns, c.key)
	}
}

func (h *HandHub) Attached() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns) > 0
}

// Documents 는 붙어 있는 덱의 목록. 채팅창이 「어느 덱이 붙어 있나」를 그릴 때 쓴다.
func (h *HandHub) Documents() []map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]map[string]string, 0, len(h.conns))
	for _, c := range h.conns {
		out = append(out, map[string]string{"document": c.key, "label": c.label})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["document"] < out[j]["document"] })
	return out
}

// pick 은 조작이 갈 손을 고른다.
//
// 문서를 **말했으면 그것만** 본다 — 없으면 비슷한 것으로 갈음하지 않는다(§5.8 의 규칙이
// 여기서도 걸린다: 못 찾은 것을 비슷한 것으로 대신하면 모델이 엉뚱한 덱을 고치고도 성공했다고
// 말한다). 생략했으면 **가장 최근에 말한 손**이 활성 문서다(§4.4 ④).
func (h *HandHub) pick(document string) (*handConn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.conns) == 0 {
		return nil, errors.New("not attached to PowerPoint: no add-in task pane is connected to this helper right now")
	}
	if document != "" {
		c, ok := h.conns[document]
		if !ok {
			// **일한 적 있는 손만 덱으로 센다.**
			//
			// 붙어 있다는 것과 일하고 있다는 것은 다르다. 엿듣기만 하는 붙임, 얼어붙은 창,
			// 아직 아무 말도 안 해 본 연결이 여기 섞이면 「덱이 둘 있다」는 거짓말이 된다 —
			// 실물에서 그 답을 봤다(2026-09-03): 작업창이 끊긴 뒤 목록에 남은 것은 엿듣는
			// 붙임 하나뿐이었는데, 그것을 열린 덱으로 적어 보냈다.
			open := make([]string, 0, len(h.conns))
			for k, c := range h.conns {
				// **물어봤는데 답을 못 한 연결만 뺀다.** 아직 아무도 안 물어본 창은 멀쩡한 덱이다 —
				// 갓 붙은 작업창이 그렇고, 그것까지 빼면 첫 호출이 「덱이 없다」로 죽는다.
				if deaf, answered := c.record(); deaf > 0 && !answered {
					continue
				}
				open = append(open, fmt.Sprintf("%s (%s)", k, c.label))
			}
			sort.Strings(open)
			if len(open) == 0 {
				// **모델이 다른 길을 찾아 나서는 자리다.** 무엇이 일어났고 사람이 무엇을
				// 하면 되는지 적는다 — 안 적으면 모델은 PowerShell 로 PowerPoint 를 직접
				// 열려 하고, 그건 사람이 쓰던 창에 붙어 그 파일을 닫을 수 있다.
				return nil, fmt.Errorf("the deck %q is no longer attached, and no other deck is "+
					"either — the magi task pane was closed, reloaded or stopped answering. "+
					"Nothing was changed. This is not something you can work around: tell the "+
					"person to open the magi pane in PowerPoint again, and do NOT try to build "+
					"the deck with PowerShell, COM automation or python-pptx (that attaches to "+
					"their running PowerPoint and can close their file)", document)
			}
			return nil, fmt.Errorf("no open deck is addressed by %q. Open decks: %s. "+
				"Nothing was changed — a deck that is not open is not one this helper will guess at",
				document, joinOr(open, "none"))
		}
		return c, nil
	}
	// **가장 최근에 붙은 손이 아니라, 답하는 손을 고른다.**
	//
	// 앞 판본은 `seen` 이 제일 늦은 것을 골랐다. 그러면 **답 못 하는 연결 하나가 모든 호출을
	// 삼킨다** — 얼어붙은 작업창, 남아 있는 옛 PowerPoint 창, 엿듣기만 하는 붙임 하나면 된다.
	// 2026-09-03 에 그 화면을 봤다: 모델이 `list_slides` 를 세 번 부르고 세 번 다 45초 뒤에
	// 「PowerPoint 가 답을 안 했다」를 받았고, 결국 **덱이 안 열려 있다고 판단해 사람에게
	// 빈 파일을 올려 달라고 했다.** 덱은 그동안 열려 있었다.
	//
	// 차례: ① 답한 적 있고 안 놓친 손 → ② 아직 아무 말도 안 해 본 손 → ③ 놓친 적 있는 손.
	// 같은 층에서는 최근 것이 이긴다. **한 번은 나쁜 연결에 걸릴 수 있어도 두 번은 아니다.**
	var best *handConn
	for _, c := range h.conns {
		if best == nil || rankConn(c) < rankConn(best) || (rankConn(c) == rankConn(best) && c.seen.After(best.seen)) {
			best = c
		}
	}
	// **둘 이상이 똑같이 좋으면 고르지 않는다.**
	//
	// 여태 같은 층에서는 최근 것이 이겼다. 덱이 하나일 때는 그것이 답인데, **둘일 때는 답이
	// 아니라 추측**이다 — 그리고 그 추측이 틀리면 사람이 보고 있지도 않은 덱이 고쳐진다.
	// 실물에서 그 화면을 봤다(2026-09-04): PowerPoint 창 둘 중 새 덱에 자료를 만들라고 했는데
	// 모델이 읽은 것은 옆 덱(17장)이었고, 사람이 "17장짜리는 다른 덱인데?" 라고 물었다. 그때는
	// 아직 아무것도 안 쓴 참이었다.
	//
	// 어느 창이 앞에 있는지는 이 허브가 알 수 없다 — PowerPoint 만 안다. 모르는 것을 고르는
	// 대신 **이름을 대라고 답한다.** 덱이 하나면 여태처럼 그것을 준다.
	tied := 0
	for _, c := range h.conns {
		if rankConn(c) == rankConn(best) {
			tied++
		}
	}
	if tied > 1 {
		open := make([]string, 0, tied)
		for k, c := range h.conns {
			if rankConn(c) == rankConn(best) {
				open = append(open, fmt.Sprintf("%s (%s)", k, c.label))
			}
		}
		sort.Strings(open)
		return nil, fmt.Errorf("more than one deck is open and this helper cannot tell which one "+
			"the person is looking at — PowerPoint knows that, we do not. Name it: pass "+
			"document with one of %s. Nothing was changed; guessing here would edit a deck "+
			"nobody is watching", joinOr(open, "none"))
	}
	return best, nil
}

// attachedNames 는 지금 붙어 있는 덱들을 사람이 부르는 이름으로. **거절문이 사실을 적게**
// 하는 재료다 — 「못 찾았다」와 「하나도 안 붙었다」는 사람이 할 일이 다르다.
func (h *HandHub) attachedNames() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, 0, len(h.conns))
	for k, c := range h.conns {
		if c.label != "" {
			out = append(out, c.label)
		} else {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Call 은 조작 하나를 손에 넘기고 답을 기다린다.
func (h *HandHub) Call(ctx context.Context, document, op string, args map[string]any) (HandResult, error) {
	c, err := h.pick(document)
	if err != nil {
		return HandResult{}, err
	}

	// 「이 문서에 손은 하나」. 잠그는 폭이 호출 하나라, 기다리는 것도 앞 호출 하나뿐이다.
	lockCtx, cancelLock := context.WithTimeout(ctx, h.timeout())
	defer cancelLock()
	if err := lock(lockCtx, &c.busy); err != nil {
		return HandResult{}, fmt.Errorf("the deck %q is busy with another call and did not free up in time; nothing was changed", c.label)
	}
	defer c.busy.Unlock()

	id := newRequestID()
	reply := make(chan HandReply, 1)
	c.mu.Lock()
	c.waiting[id] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
	}()

	select {
	case c.out <- HandRequest{ID: id, Op: op, Args: args}:
	default:
		// 큐가 찼다. **조용히 버리지 않는다** — 버리면 모델이 갔다고 믿고 기다린다.
		return HandResult{}, fmt.Errorf("the add-in for %q has %d operations queued and did not take another; nothing was changed", c.label, handQueue)
	}

	timeout := time.NewTimer(h.timeout())
	defer timeout.Stop()

	select {
	case <-ctx.Done():
		// 부른 쪽이 그만뒀다. **이건 애드인에 대한 증거가 아니다**(§5.4 — magi 도 자기 천장을
		// 서버의 잘못으로 세지 않는다). 사유를 그대로 적는다.
		return HandResult{}, fmt.Errorf("the caller stopped waiting before PowerPoint answered (%v); the operation may still have run", ctx.Err())
	case <-timeout.C:
		// 우리가 끊었다는 것과 **얼마에서 끊었는지**를 적는다. magi 의 60초 천장이 이 문구를
		// 안 실어 주는 것이 §4.4 ③ 이 코어에 낸 유일한 요청이라, 우리 쪽에서 같은 실수를 하지 않는다.
		//
		// **이 손이 놓쳤다는 사실을 남긴다.** 다음 고르기에서 뒤로 밀린다 — 얼어붙은 연결
		// 하나가 모든 호출을 삼키는 것을 여기서 끊는다.
		c.mu.Lock()
		c.deaf++
		c.mu.Unlock()
		// **덱이 안 열렸다는 뜻이 아니다.** 이 말을 안 적었더니 모델이 세 번 놓친 뒤
		// 「덱이 없다」고 판단하고 사람에게 빈 파일을 올려 달라고 했다(2026-09-03 실측).
		// 붙어 있는 덱이 몇인지 같이 적어, 그 결론을 못 내리게 한다.
		return HandResult{}, fmt.Errorf(
			"the magi helper stopped waiting after %v: PowerPoint did not answer this call. "+
				"It may still be running, so re-read before assuming nothing changed. "+
				"THE DECK IS STILL OPEN AND ATTACHED (%d attached: %s) — this is a slow or stuck "+
				"answer, not a missing deck, and asking the person to open or upload a file is the "+
				"wrong move. Try the same call again: a call that goes to a stuck connection is "+
				"retried on a live one. And do NOT reach for PowerShell, COM automation or python-pptx "+
				"instead — New-Object -ComObject PowerPoint.Application ATTACHES TO THE PERSON'S RUNNING "+
				"POWERPOINT, and a script that adds or closes presentations there can shut their open deck. "+
				"That has happened. If these tools cannot do it, say so and stop.",
			h.timeout(), len(h.attachedNames()), joinOr(h.attachedNames(), "none"))
	case rep := <-reply:
		c.mu.Lock()
		c.seen = h.now()
		c.mu.Unlock()
		if rep.Error != "" {
			return HandResult{}, errors.New(rep.Error)
		}
		res := HandResult{
			Document: c.key,
			Label:    firstNonEmpty(rep.Label, c.label),
			Result:   rep.Result,
			Changed:  rep.Changed,
		}
		if rep.Document != "" && rep.Document != c.key {
			// 애드인이 자기가 다른 덱을 손댔다고 말하면 **그 말을 싣는다.** 우리가 아는 키로
			// 덮어쓰면 「실제로 손댄 문서」가 아니라 「우리가 보냈다고 믿는 문서」가 된다.
			res.Document = rep.Document
		}
		if rep.Epoch != 0 || rep.Count != 0 {
			res.Revision = &Revision{Known: true, Epoch: rep.Epoch, Count: rep.Count}
		} else {
			res.Revision = &Revision{Known: false}
		}
		return res, nil
	}
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func joinOr(ss []string, ifEmpty string) string {
	if len(ss) == 0 {
		return ifEmpty
	}
	out := ss[0]
	for _, s := range ss[1:] {
		out += ", " + s
	}
	return out
}

// lock 은 뮤텍스를 기다리되 ctx 가 끝나면 포기한다. `sync.Mutex` 에는 그런 문이 없어서 만든다.
func lock(ctx context.Context, mu *sync.Mutex) error {
	got := make(chan struct{})
	go func() {
		mu.Lock()
		close(got)
	}()
	select {
	case <-got:
		return nil
	case <-ctx.Done():
		// 뒤늦게 잡히면 놓아 준다. 안 놓으면 그 문서가 영영 잠긴다.
		go func() {
			<-got
			mu.Unlock()
		}()
		return ctx.Err()
	}
}

var requestSeq struct {
	sync.Mutex
	n int
}

func newRequestID() string {
	requestSeq.Lock()
	defer requestSeq.Unlock()
	requestSeq.n++
	return "r" + strconv.Itoa(requestSeq.n)
}

// deliver 는 전송이 올려 보낸 답을 기다리던 자리에 놓는다. 기다리는 사람이 없으면 **버린다** —
// 늦게 온 답이 엉뚱한 호출의 답으로 소비되면 안 된다(magi 의 소켓이 같은 이유로 락스텝이다).
func (c *handConn) deliver(rep HandReply) bool {
	c.mu.Lock()
	ch, ok := c.waiting[rep.ID]
	// **답한 적이 있다는 사실을 남긴다.** 기다리는 사람이 없는 늦은 답이어도 이 손이 살아
	// 있다는 증거이므로 센다.
	c.answered = true
	c.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- rep:
		return true
	default:
		return false
	}
}
