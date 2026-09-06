package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// fakeHand 는 애드인 자리에 서는 시험용 손. **재는 것은 헬퍼이지 PowerPoint 가 아니다** —
// 이 머신에 PowerPoint 가 없다는 사실을 시험이 감추면 안 되므로 이름이 fake 다.
type fakeHand struct {
	mu       sync.Mutex
	attached bool
	calls    []handCall
	answer   HandResult
	err      error
}

type handCall struct {
	Document string
	Op       string
	Args     map[string]any
}

func (h *fakeHand) Attached() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.attached
}

func (h *fakeHand) Call(_ context.Context, document, op string, args map[string]any) (HandResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls = append(h.calls, handCall{Document: document, Op: op, Args: args})
	if h.err != nil {
		return HandResult{}, h.err
	}
	res := h.answer
	if res.Document == "" {
		// 손은 **실제로 손댄 문서**를 답한다. 생략된 호출에도 답이 있다는 것이 요점이다.
		res.Document = "doc-active"
	}
	return res, nil
}

// sink 는 magi 의 매니저가 도구를 꽂는 자리. 진짜 앱의 레지스트리 대신 쓴다.
type sink struct {
	mu    sync.Mutex
	tools map[string]port.Tool
}

func newSink() *sink { return &sink{tools: map[string]port.Tool{}} }

func (s *sink) Register(t port.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.Name()] = t
}

func (s *sink) Unregister(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tools, name)
}

func (s *sink) get(name string) port.Tool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tools[name]
}

func (s *sink) names() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.tools))
	for n := range s.tools {
		out = append(out, n)
	}
	return out
}

// magi 자신의 MCP 클라이언트가 이 헬퍼에 붙는다.
//
// 이 시험이 있는 이유는 저장소가 이미 한 번 적어 둔 것이다(`mcpserve_interop_test.go`): 서버는
// 줄을 먹여 보며 시험하고 클라이언트는 자기에게 맞춘 가짜 서버로 시험하면, 두 구현이 **자기
// 자신과만 맞고 서로와는 안 맞는다.** 그리고 §4.5 가 그 표본을 하나 들고 있다 — 스펙이 MUST 로
// 적은 202 를 magi 가 유일하게 거절하던 시절, 스펙대로 만든 서버만 쫓겨났다.
//
// 그래서 여기서 도는 것은 진짜 `mcp.Manager.Attach` 다. door 가 실제로 부르는 그 경로다(§5.0.1).
func TestMagisOwnClientAttachesToThisHelper(t *testing.T) {
	hand := &fakeHand{attached: true, answer: HandResult{
		Document: "doc-7",
		Label:    "q3.pptx",
		Result:   map[string]any{"slides": []any{map[string]any{"slide": 1, "slide_id": "s1", "layout": "제목 및 내용", "shapes": 4}}},
	}}
	srv := httptest.NewServer(&MCPServer{Hand: hand, Now: func() time.Time { return time.Unix(0, 0) }})
	defer srv.Close()

	s := newSink()
	mgr := mcp.NewManager(s)
	defer mgr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// door 가 답하는 것은 ack 가 아니라 **증거**다(§5.0.1) — 무엇이 등록됐는지.
	got, err := mgr.Attach(ctx, "", ServerName, srv.URL, nil)
	if err != nil {
		t.Fatalf("magi 가 못 붙었다: %v", err)
	}
	if len(got) != len(catalogue(true)) {
		t.Fatalf("도구 %d 개를 올렸는데 %d 개가 등록됐다: %v", len(catalogue(true)), len(got), got)
	}
	want := "mcp__" + ServerName + "__list_paragraphs"
	tool := s.get(want)
	if tool == nil {
		t.Fatalf("%s 가 레지스트리에 없다. 있는 것: %v", want, s.names())
	}

	// 그리고 그 도구를 **실제로 부른다.** 등록됐다는 것과 부르면 답한다는 것은 다른 사실이다.
	res, err := tool.Execute(ctx, json.RawMessage(`{}`), port.ToolEnv{SessionID: session.SessionID("sess-1")})
	if err != nil {
		t.Fatalf("호출이 실패했다: %v", err)
	}
	if res.IsError {
		t.Fatalf("도구가 에러로 답했다: %s", res.Content)
	}
	var text string
	if err := json.Unmarshal(res.Content, &text); err != nil {
		t.Fatalf("결과가 글이 아니다: %v", err)
	}
	for _, must := range []string{`"document": "doc-7"`, `"document_label": "q3.pptx"`, `"as_of"`, `"slides"`} {
		if !strings.Contains(text, must) {
			t.Errorf("결과에 %s 가 없다:\n%s", must, text)
		}
	}
	if len(hand.calls) != 1 || hand.calls[0].Op != "list_paragraphs" {
		t.Fatalf("손이 받은 것: %+v", hand.calls)
	}
}

// 스펙이 MUST 로 적은 값(§4.5). 알림에는 본문 없이 202 다.
func TestANotificationIsAcceptedWithTwoOhTwo(t *testing.T) {
	srv := httptest.NewServer(&MCPServer{Hand: &fakeHand{attached: true}})
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("알림에 %d 로 답했다 — 스펙은 202 를 MUST 로 적는다", resp.StatusCode)
	}
}

// 손이 없으면 도구는 **실패하고 사유를 댄다**(§5.4). 조용한 빈 결과는 에이전트가 「덱이 비어
// 있다」로 읽는다 — 그게 이 항목이 있는 이유다.
func TestWithNoAddinAttachedToolsFailAndSayWhy(t *testing.T) {
	srv := &MCPServer{Hand: &fakeHand{attached: false}}
	got := srv.call(httptest.NewRequest("POST", "/mcp", nil), "read_paragraphs", json.RawMessage(`{"from":1}`))
	if got["isError"] != true {
		t.Fatalf("실패로 안 답했다: %v", got)
	}
	text := firstText(t, got)
	if !strings.Contains(text, "not attached to Word") {
		t.Errorf("사유가 붙어 있지 않다는 말이 아니다: %s", text)
	}
	if !strings.Contains(strings.ToLower(text), "try again") {
		t.Errorf("다음에 무엇을 하면 되는지가 없다: %s", text)
	}
}

// 모르는 도구도, 모르는 인자도 **호출이 도착했다는 사실은 인정하고** 거절한다 — JSON-RPC
// 에러가 아니라 `isError` 결과다. 그 차이가 모델이 「인자를 고칠까 도구를 바꿀까」를 정한다.
func TestRefusalsComeBackAsToolErrorsNotProtocolErrors(t *testing.T) {
	hand := &fakeHand{attached: true}
	srv := &MCPServer{Hand: hand}
	req := httptest.NewRequest("POST", "/mcp", nil)

	unknown := srv.call(req, "폴더_열기", json.RawMessage(`{}`))
	if unknown["isError"] != true || !strings.Contains(firstText(t, unknown), "no such tool") {
		t.Errorf("모르는 도구의 답: %v", unknown)
	}
	badArg := srv.call(req, "insert_paragraphs", json.RawMessage(`{"address":"A1","values":[[1]],"keep_formatting":true}`))
	if badArg["isError"] != true || !strings.Contains(firstText(t, badArg), "keep_formatting") {
		t.Errorf("모르는 인자의 답: %v", badArg)
	}
	// 그리고 **손에 안 갔어야 한다.** 거절이 도착 뒤에 오면 덱이 이미 바뀐 뒤다.
	if len(hand.calls) != 0 {
		t.Errorf("거절했는데 손이 %d 번 불렸다", len(hand.calls))
	}
}

// 토큰이 걸려 있으면 없는 요청은 401 이다(§8 — 루프백이라고 신뢰하지 않는다).
func TestTheMCPDoorWantsTheToken(t *testing.T) {
	srv := httptest.NewServer(&MCPServer{Hand: &fakeHand{attached: true}, Token: "s3cret"})
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("토큰 없이 %d 로 통과했다", resp.StatusCode)
	}

	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	req.Header.Set("Authorization", "Bearer s3cret")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("토큰을 내밀었는데 %d 다", resp2.StatusCode)
	}
}

// 핸드셰이크가 magi 의 상수와 같은 리비전을 답한다(§4.4 — 맞춰 주는 쪽이 우리다).
func TestTheHandshakeAnswersTheRevisionMagiSpeaks(t *testing.T) {
	srv := &MCPServer{Hand: &fakeHand{attached: true}}
	got, fault := srv.handle(httptest.NewRequest("POST", "/mcp", nil), rpcRequest{Method: "initialize", ID: json.RawMessage("1")})
	if fault != nil {
		t.Fatal(fault.message)
	}
	m := got.(map[string]any)
	if m["protocolVersion"] != mcpProtocolVersion {
		t.Fatalf("리비전이 %v 다", m["protocolVersion"])
	}
	if mcpProtocolVersion != "2025-06-18" {
		t.Fatalf("magi 의 `internal/adapter/mcp/jsonrpc.go` 상수와 같아야 한다: %s", mcpProtocolVersion)
	}
}

func firstText(t *testing.T, res map[string]any) string {
	t.Helper()
	blocks, ok := res["content"].([]map[string]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("content 가 없다: %v", res)
	}
	s, _ := blocks[0]["text"].(string)
	return s
}

// 읽기만 하는 조작은 시간 초과 뒤 **한 번 더** 보낸다.
//
// 실물에서 본 것(2026-09-03): 첫 호출 둘이 45초씩 죽고 셋째가 살아난다. 그 사이에 죽은
// 호출을 받은 모델은 이 도구를 통째로 버리고 bash 로 PowerPoint 를 직접 열어 딴 파일을
// 만들려 했다 — 사람은 안 바뀐 화면과 부탁하지 않은 스크립트 더미를 받는다.
//
// **쓰기는 안 보낸다.** 시간 초과는 「안 갔다」가 아니라 「답을 못 들었다」이므로, 다시
// 보내면 장이 둘 생길 수 있다.
func TestReadOnlyCallsAreTriedOnceMoreAfterATimeout(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		tool  string
		want  int
		story string
	}{
		{"읽기는 다시 보낸다", "list_paragraphs", 2, "덱을 안 고치므로 두 번 해도 같다"},
		{"쓰기는 안 보낸다", "set_properties", 1, "답을 못 들은 것이지 안 간 것이 아니다 — 시트가 둘 생긴다"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			hs := &countingHand{
				onCall: func() (HandResult, error) {
					calls++
					return HandResult{}, fmt.Errorf("the magi helper stopped waiting after 45s: Word did not answer this call")
				},
			}
			s := &MCPServer{Hand: hs}
			s.call(httptest.NewRequest(http.MethodPost, "/mcp", nil), tc.tool, json.RawMessage(`{}`))
			if calls != tc.want {
				t.Fatalf("%s: %d번 불렀다, %d번이어야 한다 — %s", tc.tool, calls, tc.want, tc.story)
			}
		})
	}
}

type countingHand struct{ onCall func() (HandResult, error) }

func (h *countingHand) Attached() bool { return true }
func (h *countingHand) Call(_ context.Context, _, _ string, _ map[string]any) (HandResult, error) {
	return h.onCall()
}

// TestThePictureGoesOnceNotTwice 는 **그림이 글에 남지 않는가**를 잰다.
//
// 실물에서 나왔다(2026-09-04, Mac). `render_range` 한 번의 결과 본문이 **55,392자**였고, 같은
// 그림이 이미지 블록으로 한 번 더 갔다. 이유는 순서였다: `delete(body, "image_base64")` 가
// `MarshalIndent` **뒤에** 있어서 이미 만들어진 글을 못 고쳤다 — 그 줄은 아무 일도 안 했고,
// 주석만 「본문에서는 지운다」고 말하고 있었다.
//
// §6.10 이 이 도구를 「제일 비싼 도구」라고 적어 두었는데 값을 두 번 물고 있었다. 못 보는
// 모델에게는 그 55KB 가 순수한 낭비이고, 보는 모델에게도 같은 그림을 두 벌 보낸 것이다.
func TestThePictureGoesOnceNotTwice(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	hand := &fakeHand{attached: true, answer: HandResult{
		Document: "doc-1",
		Result:   map[string]any{"image_base64": png, "image_mime": "image/png", "image_bytes": 68},
	}}
	srv := &MCPServer{Hand: hand}
	out := srv.call(httptest.NewRequest("POST", "/mcp", nil), "read_html", json.RawMessage(`{"from":1}`))

	blocks, _ := out["content"].([]map[string]any)
	if len(blocks) != 2 {
		t.Fatalf("글 하나와 그림 하나여야 한다 — 블록 %d개: %v", len(blocks), out)
	}
	text, _ := blocks[0]["text"].(string)
	if strings.Contains(text, png) {
		t.Errorf("글에 base64 가 남았다(%d자) — 그림이 두 번 간다", len(text))
	}
	if !strings.Contains(text, "image_bytes") {
		t.Error("얼마짜리였는지는 글에 남아야 한다 — 모르면 아끼는 판단을 못 한다(§6.10)")
	}
	if blocks[1]["type"] != "image" || blocks[1]["data"] != png {
		t.Errorf("그림 블록이 그림을 안 들었다: %v", blocks[1])
	}
}
