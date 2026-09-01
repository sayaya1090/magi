package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// MCP 서버 쪽 얼굴. **Streamable HTTP 다, stdio 가 아니다**(DESIGN.md §4.5).
//
// stdio 면 데몬이 서버를 자식으로 띄우는데, magi 데몬은 워크스페이스당 하나라 여럿이고 각자
// 자기 헬퍼를 띄우면 **같은 애드인 하나를 두고 헬퍼 N 개가 싸운다.** HTTP 면 헬퍼가 먼저 서고
// 데몬들이 클라이언트로 붙는다 — 그게 §5.2 의 「머신에 하나」가 실제로 필요한 이유다.

// mcpProtocolVersion 은 우리가 답하는 리비전이다.
//
// **맞춰 주는 쪽이 우리다**(§4.4). magi 는 핸드셰이크 응답을 통째로 버려서(`Initialize` 가
// 결과를 `&struct{}{}` 로 받는다) 우리가 무엇을 답하든 그냥 이어지는데, 그게 좋은 소식이
// 아니다 — 어긋남이 핸드셰이크가 아니라 **한참 뒤 이상한 호출 실패**로 나타난다. 그래서
// magi 의 상수와 같은 값을 그대로 답한다.
const mcpProtocolVersion = "2025-06-18"

// MCPServer 는 `/mcp` 를 답하는 쪽.
type MCPServer struct {
	// Hand 는 덱에 닿는 구멍. nil 이면 손이 없는 것과 같다.
	Hand Hand
	// Token 이 비어 있지 않으면 `Authorization: Bearer <token>` 을 요구한다. 루프백이라고
	// 신뢰하지 않는 이유는 §8 에 있다 — 토큰이 새면 같은 머신의 아무 프로세스나 이 포트를
	// 두드릴 수 있다.
	Token string
	// Now 는 결과의 `as_of` 를 찍는다. 시험이 시계를 안 재게 주입한다.
	Now func() time.Time
}

func (s *MCPServer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// ServeHTTP 는 POST 하나가 곧 요청 하나인 모양이다. SSE 로 답할 이유가 아직 없다 — 한 호출에
// 답이 하나고, 스트림으로 올리면 클라이언트마다 다르게 읽는 자리만 는다.
func (s *MCPServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !loopbackOnly(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		// GET 은 스펙상 서버가 열어 두는 SSE 통로인데 우리는 안 연다. 405 로 **말한다** —
		// 404 로 답하면 엔드포인트를 잘못 적은 것처럼 읽힌다.
		w.Header().Set("Allow", "POST")
		http.Error(w, "this MCP endpoint answers POST only; it opens no server-initiated stream", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "this helper needs the token the add-in page was served with", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil {
		http.Error(w, "could not read the request body", http.StatusBadRequest)
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, "parse error: "+err.Error())
		return
	}

	// **알림에는 202 다.** Streamable HTTP 스펙이 이 방향에 대해 본문 없이 202 Accepted 를
	// MUST 로 적는다(§4.5). 얼마 전까지 magi 가 유일하게 거절하던 값이 이것이라, 스펙대로 만든
	// 서버가 핸드셰이크 중간에 쫓겨나고 안 지킨 서버만 붙었다 — 지금은 200·204·202 를 다 받는다.
	// 우리가 202 를 고르는 이유는 붙을 상대가 magi 만이 아니어서다(§4.5).
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := s.handle(r, req)
	if rpcErr != nil {
		writeRPCError(w, req.ID, rpcErr.code, rpcErr.message)
		return
	}
	writeJSON(w, map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

type rpcFault struct {
	code    int
	message string
}

func (s *MCPServer) handle(r *http.Request, req rpcRequest) (any, *rpcFault) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "magi-ppt", "version": helperVersion},
			// 서버가 적어 보내는 instructions 는 **magi 에 도달하지 않는다**(§7 — 클라이언트가
			// 핸드셰이크 결과를 통째로 버린다). 다른 클라이언트를 위해 싣되, 이 문장에 기대는
			// 설계는 없다. 기대는 자리는 도구 설명문이다.
			"instructions": "Tools act on the deck a person has open in PowerPoint. Positions are 1-based. " +
				"Nothing here edits charts, SmartArt, animation or speaker notes, and nothing restyles an existing table.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": s.toolDefs()}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResult("could not read the call: " + err.Error()), nil
		}
		return s.call(r, p.Name, p.Arguments), nil
	default:
		return nil, &rpcFault{code: -32601, message: "no such method: " + req.Method}
	}
}

// toolDefs 는 `tools/list` 의 몸이다.
//
// `annotations.readOnlyHint` 를 단다. **오늘 magi 는 그것을 안 읽는다** — `toolDef` 가
// `{name, description, inputSchema}` 셋뿐이라 통째로 버려진다(§4.4 ⑤). 그러니 이 칸은 다른
// 클라이언트를 향한 선언이자, 규약이 붙을 자리를 미리 맞춰 두는 것이지 지금 무엇을 막고 있다는
// 뜻이 아니다. 지금 `advise` 를 실제로 가르는 것은 이름 하나이고, 그 자리는 허용 규칙이다.
func (s *MCPServer) toolDefs() []map[string]any {
	tools := catalogue()
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Desc,
			"inputSchema": schemaOf(t),
			"annotations": map[string]any{"readOnlyHint": t.ReadOnly},
		})
	}
	return out
}

// call 은 도구 하나를 돌린다.
//
// 실패는 **JSON-RPC 에러가 아니라 `isError` 결과**다. 호출이 서버에 닿았고 이해됐다는 것과
// 서버가 그 요청을 못 들어준다는 것은 다른 사실이고, 그 차이가 모델이 「인자를 고칠까 도구를
// 바꿀까」를 정하는 데 쓰인다.
func (s *MCPServer) call(r *http.Request, name string, raw json.RawMessage) map[string]any {
	var found *tool
	for _, t := range catalogue() {
		if t.Name == name {
			c := t
			found = &c
			break
		}
	}
	if found == nil {
		return errorResult(fmt.Sprintf("no such tool: %s. This server has: %s", name, strings.Join(toolNames(), ", ")))
	}
	args, err := validateArgs(*found, raw)
	if err != nil {
		return errorResult(err.Error())
	}
	if s.Hand == nil || !s.Hand.Attached() {
		// **조용히 빈 결과를 주지 않는다**(§5.4). 빈 결과는 에이전트가 「덱이 비어 있다」로
		// 읽는다. 사유가 「PowerPoint 에 붙어 있지 않다」여야 하고, 다음에 무엇을 하면 되는지도
		// 같이 적는다 — 이 실패는 사람이 창을 열면 풀린다.
		return errorResult("not attached to PowerPoint: no add-in task pane is connected to this helper right now, " +
			"so nothing could be read or changed. Ask the person to open the magi pane in PowerPoint, then try again.")
	}

	res, err := s.Hand.Call(r.Context(), documentOf(args), name, args)
	if err != nil {
		return errorResult(err.Error())
	}

	body := map[string]any{
		// **손댄 문서를 싣는다**(§6). 받은 인자를 되받아 적는 것이 아니라 실제로 손댄 것이다 —
		// 생략했을 때 답이 되는 쪽이 그것이고, 되받아 적기만 하면 생략한 호출은 여전히
		// 아무것도 안 말한다. 그러면 `document` 는 모델이 채울 수 없는 인자가 된다.
		"document": res.Document,
		// as_of: 위치는 스냅숏이다(CAPABILITIES.md §10.5). 데이터가 스스로 그렇게 말한다.
		"as_of": s.now().UTC().Format(time.RFC3339),
	}
	if res.Label != "" {
		body["document_label"] = res.Label
	}
	for k, v := range res.Result {
		body[k] = v
	}
	if res.Revision != nil {
		body["revision"] = res.Revision
	}
	// **바뀐 값은 결과가 스스로 싣는다**(§4.4 ⑤·§7). 카운슬이 「이번 턴의 편집」으로 받는 칸은
	// 우리 턴에서 늘 빈다 — 우리 도구는 PowerPoint 를 시켜 고치지 파일을 쓰지 않으므로 디스크가
	// 안 바뀐다. 그래서 before→after 가 여기 없으면 판정에 도달하는 것이 아무것도 없다.
	if len(res.Changed) > 0 {
		body["changed"] = res.Changed
	}

	text, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return errorResult("could not render the result: " + err.Error())
	}

	content := []map[string]any{{"type": "text", "text": string(text)}}
	// 그림은 **글이 아니라 자기 블록으로** 간다. 손이 base64 를 실어 보내면 그대로 넘긴다 —
	// 다만 이 경로는 아껴 쓴다(개정 3): 붙을 모델이 멀티모달이라는 보장이 없고, **카운슬은
	// 어느 경우에도 그림을 못 본다**(§7).
	if img, ok := res.Result["image_base64"].(string); ok && img != "" {
		mime, _ := res.Result["image_mime"].(string)
		if mime == "" {
			mime = "image/png"
		}
		content = append(content, map[string]any{"type": "image", "data": img, "mimeType": mime})
		delete(body, "image_base64")
	}
	return map[string]any{"content": content}
}

func toolNames() []string {
	var out []string
	for _, t := range catalogue() {
		out = append(out, t.Name)
	}
	return out
}

func errorResult(msg string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]any{{"type": "text", "text": msg}},
	}
}

func (s *MCPServer) authorized(r *http.Request) bool {
	if s.Token == "" {
		return true
	}
	got := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer"))
	return constantEquals(got, s.Token)
}

// loopbackOnly 는 라우팅 가능한 주소에서 온 요청을 거절한다.
//
// 리스너가 이미 루프백에만 바인드하지만(§8), 그건 **바인드하는 쪽의 약속**이고 이건 **받는
// 쪽의 검사**다. 둘 중 하나가 언젠가 넓어져도 다른 하나가 남는다.
func loopbackOnly(w http.ResponseWriter, r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip != nil && !ip.IsLoopback() {
		http.Error(w, "this helper answers loopback only", http.StatusForbidden)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeStatus 는 **실패도 JSON 으로** 낸다.
//
// `http.Error` 는 본문을 평문으로 흘리므로, 받는 쪽은 사유 한 줄 말고는 아무것도 못 읽는다.
// 여기서 실패에 실어야 하는 것은 사유만이 아니라 **자리**다 — 소켓·워크스페이스·로그 경로가
// 사람이 다음에 할 일 그 자체이고, 평문으로 흘리면 화면이 그걸 링크로도 못 만든다.
func writeStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	writeJSON(w, map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]any{"code": code, "message": msg},
	})
}

// constantEquals 는 길이가 다르면 바로 지되 같은 길이에서는 끝까지 본다. 토큰 비교에 `==` 를
// 쓰면 앞자리부터 맞춰 보는 시간 차가 남는다 — 루프백이라도 같은 머신의 남의 프로세스가 재는
// 것이 정확히 이 설계가 토큰을 둔 이유다(§8).
func constantEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

// newToken 은 기동 때 한 번 만드는 토큰이다. 페이지에 박혀 나가고(§5.5), 데몬에는 attach 의
// 헤더로 간다(§5.0.1).
func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
