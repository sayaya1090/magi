package office

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
	// App 은 이 서버가 광고하는 도구 표의 주인.
	App *App
	// Hand 는 덱에 닿는 구멍. nil 이면 손이 없는 것과 같다.
	Hand Hand
	// Token 이 비어 있지 않으면 `Authorization: Bearer <token>` 을 요구한다. 루프백이라고
	// 신뢰하지 않는 이유는 §8 에 있다 — 토큰이 새면 같은 머신의 아무 프로세스나 이 포트를
	// 두드릴 수 있다.
	Token string
	// Now 는 결과의 `as_of` 를 찍는다. 시험이 시계를 안 재게 주입한다.
	Now func() time.Time
	// Council 은 **붙은 컴패니언이 카운슬로 끝내는가**를 답한다. 도구 설명문의 마무리 안내가
	// 이 값으로 갈린다 — 없는 도구를 이름으로 적으면 모델이 그것을 부른다(`tools.go` 의
	// `declare`). nil 이면 「모른다」이고, 그때는 **안 적는다**: 지어낸 안내가 없는 문을
	// 가리키는 쪽이, 있는 문을 안 알려 주는 쪽보다 나쁘다.
	Council func() bool
}

// hasCouncil 은 위 물음의 답. 모르면 거짓이다.
func (s *MCPServer) hasCouncil() bool { return s.Council != nil && s.Council() }

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
			"serverInfo":      map[string]any{"name": "magi-office-" + s.App.Key, "version": helperVersion},
			// 서버가 적어 보내는 instructions 는 **magi 에 도달하지 않는다**(§7 — 클라이언트가
			// 핸드셰이크 결과를 통째로 버린다). 다른 클라이언트를 위해 싣되, 이 문장에 기대는
			// 설계는 없다. 기대는 자리는 도구 설명문이다.
			// 이 문장은 **낡으면 안 된다.** 앞 판본은 「차트·애니메이션·노트는 못 고친다」라고
			// 적어 둔 채로 그 셋이 다 생겼다 — 있는 것을 없다고 적는 것이 이 저장소가 제일
			// 싫어하는 모양인데, magi 에 안 닿는다는 이유로 아무도 안 고쳤다.
			"instructions": s.App.MCPInstructions,
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
// readOnly 는 이 이름이 덱을 안 고치는 조작인가. **표는 하나뿐이다**(`tools()`) — 여기에
// 이름을 또 적으면 도구가 하나 늘 때마다 두 자리를 고쳐야 하고, 하나를 빠뜨리는 날이 온다.
func (s *MCPServer) readOnly(name string) bool {
	for _, t := range s.App.Catalogue(s.hasCouncil()) {
		if t.Name == name {
			return t.ReadOnly
		}
	}
	return false
}

// isTimeout 은 「우리가 기다리다 그만뒀다」인가. 거절의 뜻을 문자열로 가르는 것이 좋진 않지만,
// 그 문구를 만드는 곳도 우리이고 시험이 둘을 함께 문다.
func isTimeout(err error) bool {
	return err != nil && strings.Contains(err.Error(), "stopped waiting after")
}

func (s *MCPServer) toolDefs() []map[string]any {
	tools := s.App.Catalogue(s.hasCouncil())
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Desc,
			"inputSchema": schemaOf(s.App, t),
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
	for _, t := range s.App.Catalogue(s.hasCouncil()) {
		if t.Name == name {
			c := t
			found = &c
			break
		}
	}
	if found == nil {
		return errorResult(fmt.Sprintf("no such tool: %s. This server has: %s", name, strings.Join(s.App.toolNames(), ", ")))
	}
	args, err := validateArgs(s.App, *found, raw)
	if err != nil {
		return errorResult(err.Error())
	}
	if s.Hand == nil || !s.Hand.Attached() {
		// **조용히 빈 결과를 주지 않는다**(§5.4). 빈 결과는 에이전트가 「덱이 비어 있다」로
		// 읽는다. 사유가 「PowerPoint 에 붙어 있지 않다」여야 하고, 다음에 무엇을 하면 되는지도
		// 같이 적는다 — 이 실패는 사람이 창을 열면 풀린다.
		return errorResult("not attached to " + s.App.Product + ": no add-in task pane is connected to this helper right now, " +
			"so nothing could be read or changed. Ask the person to open the magi pane in " + s.App.Product + ", then try again.")
	}

	// **그림은 여기서 읽어 실어 보낸다.**
	//
	// 애드인은 브라우저 안이라 디스크를 못 읽고, 모델은 사진을 지어낼 수 없다. 남은 길은 모델이
	// base64 를 인자로 싣는 것인데 — 1MB 짜리 사진이 1.3MB 의 글이 되어 **매 걸음 다시 실려
	// 간다.** 그래서 모델은 경로만 말하고, 사람의 머신에서 도는 이 프로세스가 읽는다.
	//
	// 읽는 쪽이 **내용을 보고 그림이 아니면 거절한다**(image.go) — 남이 준 덱에 숨은 글이 모델을
	// 꾀어 엉뚱한 파일을 가리키게 할 수 있고, 그러면 그 내용이 슬라이드에 박혀 사람이 그것을
	// 그대로 남에게 보낸다.
	// Word 의 이름은 `insert_image` 다 — 파워포인트·엑셀 판의 `add_image` 를 그대로 두었더니 실물에서
	// 「그림 바이트가 안 왔습니다」로 죽었다(2026-09-06).
	if s.App.WantsImage != nil && s.App.WantsImage(name, args) {
		img, ierr := ReadImage(fmt.Sprint(args["path"]))
		if ierr != nil {
			return errorResult(ierr.Error())
		}
		// 판에게 줄 것만 채운다. `path` 는 그대로 두어 결과가 어느 파일이었는지 적을 수 있게 한다.
		args["image_base64"] = img.Base64
		args["image_ext"] = img.Ext
		args["image_mime"] = img.Mime
		args["image_width"] = img.Width
		args["image_height"] = img.Height
		args["path"] = img.Path
		args["image_bytes"] = img.Bytes
	}
	// 문서 파일도 같은 길이다 — Word 의 insert_file(.docx), Excel 의 insert_sheets_from_file(.xlsx).
	if s.App.WantsFile != nil {
		if ext := s.App.WantsFile(name); ext != "" {
			doc, derr := ReadDocFile(fmt.Sprint(args["path"]), ext)
			if derr != nil {
				return errorResult(derr.Error())
			}
			args["file_base64"] = doc.Base64
			args["file_name"] = doc.Name
			args["file_bytes"] = doc.Bytes
			args["path"] = doc.Path
		}
	}
	// **부르는 대화가 자기 덱을 안다.** 그 대화 몫으로 붙인 등록은 주소에 덱을 싣고 오므로,
	// 인자에 `document` 가 없어도 어느 덱인지가 정해진다.
	//
	// 없으면 허브가 「덱이 둘이면 안 고른다」로 거절하고, 모델은 사람에게 되묻는다 — 실물에서
	// 그 화면을 봤다(2026-09-05: 작업창에서 시킨 일인데 「어느 덱을 비울까요」를 물었다. 그
	// 대화는 그 덱에 묶여 있었다). 물을 필요가 없는 것을 묻는 것은 사람의 시간을 쓰는 일이다.
	//
	// ⚠ 주소의 덱은 **인자보다 약하다.** 모델이 다른 덱을 대면 그쪽이 이긴다 — 한 대화에서 옆
	// 덱을 읽는 일은 있고, 그때 우리가 이겨 버리면 그 부탁이 조용히 엉뚱한 덱에 간다.
	where := documentOf(args)
	if where == "" {
		where = r.URL.Query().Get("deck")
	}
	res, err := s.Hand.Call(r.Context(), where, name, args)
	// **읽기만 하는 조작은 한 번 더 보낸다.**
	//
	// 실물에서 본 것(2026-09-03): 첫 호출 둘이 45초씩 죽고, 셋째가 4초에 살아나고, 그 뒤로는
	// 20ms 다. 작업창이 잠깐 답을 못 하는 창이 있고 곧 돌아온다 — 그런데 그 사이에 죽은
	// 호출을 받은 모델은 **이 도구를 통째로 버리고 bash 로 PowerPoint 를 직접 열어 딴 파일을
	// 만들려 했다.** 사람은 안 바뀐 화면과 부탁하지 않은 스크립트 더미를 받는다.
	//
	// **쓰기는 안 보낸다.** 시간 초과는 「안 갔다」가 아니라 「답을 못 들었다」이므로, 다시
	// 보내면 장이 둘 생기거나 글이 두 번 바뀔 수 있다. 읽기는 두 번 해도 같다.
	if err != nil && isTimeout(err) && s.readOnly(name) {
		res, err = s.Hand.Call(r.Context(), where, name, args)
	}
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

	// 그림은 **글이 아니라 자기 블록으로** 간다. 손이 base64 를 실어 보내면 그대로 넘긴다 —
	// 다만 이 경로는 아껴 쓴다(개정 3): 붙을 모델이 멀티모달이라는 보장이 없고, **카운슬은
	// 어느 경우에도 그림을 못 본다**(§7).
	//
	// ⚠ **떼는 것이 글을 짓기 전이어야 한다.** 앞 판본은 `delete` 를 아래 `MarshalIndent` **뒤에**
	// 두었다 — 그 줄은 이미 만들어진 글을 못 고치므로 **아무 일도 안 했다.** 주석은 「본문에서는
	// 지운다」고 적고 코드는 안 지우는, 이 저장소가 이름까지 붙여 둔 그 모양이다.
	//
	// 값은 실측으로 나왔다(2026-09-04, Mac): 한 번의 `render_slide` 결과 본문이 **55,392자**였고
	// 같은 그림이 이미지 블록으로 한 번 더 갔다. §6.10 이 「제일 비싼 도구」라고 적어 둔 것이
	// 값을 **두 번** 물고 있었고, 못 보는 모델에게는 그 55KB 가 순수한 낭비다.
	var image map[string]any
	if block, note, ok := imageBlock(s.App, res.Result); ok {
		image = block
		delete(body, "image_base64")
		body["picture"] = note
	}

	text, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return errorResult("could not render the result: " + err.Error())
	}

	content := []map[string]any{{"type": "text", "text": string(text)}}
	if image != nil {
		content = append(content, image)
	}
	return map[string]any{"content": content}
}

func (a *App) toolNames() []string {
	var out []string
	// 이름만 센다 — 마무리 안내는 설명문에만 붙으므로 어느 쪽이든 목록이 같다.
	for _, t := range a.Catalogue(false) {
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

// pictureNote 는 **그림을 주면서 볼 방법을 안 주면 모델이 셸로 간다**는 실측에서 나왔다.
//
// 2026-09-04: `render_shape` 로 도형을 뽑은 뒤 모델이 그 그림을 확인하려고 캐시 파일을 `read`
// 했고 — magi 는 그림을 워크스페이스 **밖**에 쓴다 — 「outside workdir」로 거부당하자 인자
// 이름을 바꿔 한 번 더 시도하고, 그다음 **PIL 이 있는지 보려고 bash 를 불렀다.** 승인기가
// 거기서 판을 세웠고 사람이 본 것은 멈춘 판이다. 그림은 이미 답에 붙어 있으니, 파일로 열
// 생각을 말라고 여기서 말한다 — 못 보는 모델에게는 무엇으로 대신하라는 말까지 같이.
const pictureNote = "이 답에 이미지로 붙여 보냈습니다. 파일로 열려고 하지 마세요 — " +
	"그림은 워크스페이스 밖 캐시에 있어 read 가 거부하고, 셸로 열 일도 아닙니다. "

// imageBlock 은 결과에 그림이 있으면 MCP 이미지 블록과 그 안내를 준다.
func imageBlock(app *App, result map[string]any) (map[string]any, string, bool) {
	img, ok := result["image_base64"].(string)
	if !ok || img == "" {
		return nil, "", false
	}
	mime, _ := result["image_mime"].(string)
	if mime == "" {
		mime = "image/png"
	}
	return map[string]any{"type": "image", "data": img, "mimeType": mime}, pictureNote + app.RenderHint, true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	encodeBody(w, v)
}

// encodeBody writes the body, and is the one place that discards the encoder's error.
//
// There is nowhere for it to go. By the time Encode can fail the status line and the headers are
// already on the wire, so the client cannot be told, and this helper answers a local add-in whose
// only recovery is to ask again. Two copies of that reasoning were two discarded returns; one is
// the honest count.
func encodeBody(w http.ResponseWriter, v any) {
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
	encodeBody(w, v)
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
