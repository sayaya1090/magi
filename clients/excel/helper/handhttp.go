package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 애드인 ↔ 헬퍼 전송(DESIGN.md §5.5).
//
// # 왜 WebSocket 이 아니라 SSE + POST 인가
//
// 설계는 「WebSocket 또는 롱폴」로 열어 두고 S10 에 답을 미뤘는데, 재기 전에 하나가 정해진다:
// 브라우저의 Local Network Access 가 **Chrome 147 부터 WebSocket·WebTransport 까지** 확대되고,
// 설계가 그것을 「위 문단이 고른 전송이 바로 그것」이라며 나쁜 소식으로 적어 뒀다(§5.5). 지금
// 고를 수 있는 것 중에서 그 게이트에 **나중에** 들어가는 쪽이 fetch 계열이고, 그쪽이 혼합
// 콘텐츠 면제가 실제로 서술된 쪽이기도 하다.
//
// 그리고 값이 하나 더 있다 — Go 표준 라이브러리에 WebSocket 서버가 없다. 의존성 하나를 늘리는
// 것과, 이미 있는 것으로 같은 일을 하는 것 중에 후자가 맞다. SSE 는 한 방향이지만 **우리에게
// 필요한 것이 정확히 한 방향**이다: 밀어 내리는 것은 헬퍼고, 올라오는 답은 평범한 POST 다.
//
// 되돌릴 수 있게 적어 둔다: S10 이 「데스크톱에서 WebSocket 도 무사하다」를 가져와도 이 선택은
// 안 바뀐다. 바뀔 이유가 되는 것은 **SSE 가 막히는 것**뿐이고, 그러면 롱폴로 내려간다.
//
// # 토큰이 쿼리에 실리는 이유
//
// `EventSource` 는 헤더를 못 싣는다. 그래서 스트림 한 자리만 쿼리로 받고 나머지는 헤더다.
// 루프백이고 페이지 자신이 그 토큰을 들고 있으므로 새로 새는 것이 없다 — 값이 커지는 자리는
// 로그인데, 이 헬퍼는 URL 을 안 남긴다.

// handStreamPath 등은 애드인과 나눠 갖는 계약이라 상수로 둔다. 애드인 쪽 문자열과 어긋나면
// 증상이 「안 붙는다」 하나로 뭉쳐 나온다 — 이름 넷 규칙과 같은 이유다(§5.5 ⚠).
const (
	handStreamPath = "/hand/stream"
	handReplyPath  = "/hand/reply"
	// handPingEvery 는 죽은 연결을 알아채는 값이자 프록시가 끊지 않게 하는 값이다. 스트림이
	// 조용한 것과 끊긴 것은 받는 쪽에서 같아 보이므로, 조용함을 없앤다.
	handPingEvery = 20 * time.Second
)

// HandHTTP 는 손의 전송 절반.
type HandHTTP struct {
	Hub   *HandHub
	Token string
	// Feed 는 이 스트림에 실어 보낼 **대화 프레임**을 준다. 없으면 조작만 흐른다.
	// 같은 연결을 반대 방향으로 한 번 더 쓰는 자리가 여기다(§5.7) — 새 포트도 새 연결도 없다.
	Feed func(document string) <-chan StreamFrame
	// PingEvery 는 시험이 줄여 잡는다.
	PingEvery time.Duration
}

// StreamFrame 은 애드인으로 내려가는 것 하나. `Kind` 로 갈린다 — `call` 은 조작이고,
// 나머지는 화면이 그릴 것이다.
type StreamFrame struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

func (h *HandHTTP) pingEvery() time.Duration {
	if h.PingEvery > 0 {
		return h.PingEvery
	}
	return handPingEvery
}

// Stream 은 애드인이 붙는 자리. **붙는 것이 곧 등록이고, 끊기는 것이 곧 떠남이다** — 따로
// 알리는 프레임을 두지 않는다(§5.5: 죽음은 연결이 끊기는 것으로 알린다).
func (h *HandHTTP) Stream(w http.ResponseWriter, r *http.Request) {
	if !loopbackOnly(w, r) {
		return
	}
	if !h.allowed(r) {
		http.Error(w, "this stream needs the token the page was served with", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "this server cannot stream", http.StatusInternalServerError)
		return
	}

	// **보는 연결은 손이 아니다.** 같은 문서 키로 붙는 연결을 허브는 하나로 보고 호출을 아무 쪽에나 준다 —
	// 2021 판에서는 손(COM 프로세스)과 화면(창)이 다른 연결이라, 화면은 role=viewer 로 붙어 전사만 받고
	// 호출은 안 받는다(hand-com/README.md). Leave 도 하지 않는다: 손의 자리를 화면이 걷어가면 안 된다.
	viewer := r.URL.Query().Get("role") == "viewer"
	var conn *handConn
	if viewer {
		conn = h.Hub.Peek(r.URL.Query().Get("workbook"))
		if conn == nil {
			http.Error(w, "no hand is attached for that workbook yet — a viewer needs a hand to look at", http.StatusNotFound)
			return
		}
	} else {
		conn = h.Hub.Join(r.URL.Query().Get("workbook"), r.URL.Query().Get("label"))
		defer h.Hub.Leave(conn)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	// 애드인이 자기 문서 키를 **첫 프레임으로** 안다. 이 값이 도구의 `document` 인자와 같은
	// 문자열이고, 화면이 「어느 덱에 붙었나」를 적는 근거다.
	writeSSE(w, "hello", map[string]any{"document": conn.key, "label": conn.label, "epoch": conn.epoch})
	flusher.Flush()

	var feed <-chan StreamFrame
	if h.Feed != nil {
		feed = h.Feed(conn.key)
	}
	ping := time.NewTicker(h.pingEvery())
	defer ping.Stop()

	calls := conn.out
	if viewer {
		calls = nil // 닫힌 채널이 아니라 nil 채널: 영영 안 고른다
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case req := <-calls:
			writeSSE(w, "call", req)
			flusher.Flush()
		case f, ok := <-feed:
			if !ok {
				feed = nil
				continue
			}
			writeSSE(w, f.Kind, json.RawMessage(f.Data))
			flusher.Flush()
		case <-ping.C:
			// 주석 프레임. 이벤트가 아니므로 화면이 아무것도 안 그린다.
			_, _ = io.WriteString(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// Reply 는 애드인이 조작의 답을 올려 보내는 자리.
func (h *HandHTTP) Reply(w http.ResponseWriter, r *http.Request) {
	if !loopbackOnly(w, r) {
		return
	}
	if !h.allowed(r) {
		http.Error(w, "this endpoint needs the token the page was served with", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "answers are posted", http.StatusMethodNotAllowed)
		return
	}
	var rep HandReply
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&rep); err != nil {
		http.Error(w, "could not read the answer: "+err.Error(), http.StatusBadRequest)
		return
	}
	document := rep.Document
	if document == "" {
		document = r.URL.Query().Get("document")
	}
	conn := h.Hub.conn(document)
	if conn == nil {
		// **무엇이 붙어 있는지 같이 적는다.** 그냥 「없다」로 답하면 애드인이 다음에 무엇을
		// 보내야 하는지 모르고, 사람이 보는 것은 45초 뒤의 타임아웃뿐이다 — 실제로 처음
		// PowerPoint 에 붙인 날 애드인이 빈 문서 키를 실어 보내 이 자리가 조용히 삼켰다.
		open := make([]string, 0, 4)
		for _, d := range h.Hub.Documents() {
			open = append(open, d["document"])
		}
		http.Error(w, fmt.Sprintf(
			"no attached workbook called %q — this reply cannot be routed and the caller is still waiting. Attached: %s",
			document, joinOr(open, "none")), http.StatusNotFound)
		return
	}
	// **기다리는 사람이 없는 답은 버린다.** 늦게 온 답이 다음 호출의 답으로 소비되면, 모델은
	// 자기가 묻지 않은 것에 대한 답을 받는다 — magi 의 소켓이 락스텝인 이유와 같다(§5.5).
	if !conn.deliver(rep) {
		// 200 이 아니라 **410 이다.** 애드인이 「내 답이 갔다」와 「아무도 안 기다렸다」를
		// 구분할 수 있어야 다음에 무엇을 할지 정한다.
		http.Error(w, "nobody was waiting for that call any more", http.StatusGone)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *HandHTTP) allowed(r *http.Request) bool {
	if h.Token == "" {
		return true
	}
	if got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer")); got != "" {
		return constantEquals(got, h.Token)
	}
	return constantEquals(r.URL.Query().Get("token"), h.Token)
}

// conn 은 키로 손을 찾는다. 없으면 nil — 지어내지 않는다.
func (h *HandHub) conn(key string) *handConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[key]
}

func writeSSE(w io.Writer, event string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		// 우리가 만든 값이라 여기 오면 코드 결함이다. 그래도 스트림을 죽이지는 않는다 —
		// 한 프레임을 잃는 것과 연결을 잃는 것은 값이 다르다.
		b = []byte(`{"error":"could not render this frame"}`)
	}
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
}
