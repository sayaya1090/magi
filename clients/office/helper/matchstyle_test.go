package office

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// styleHand 는 **덱마다 다르게 답하는** 손. 실물의 허브가 그렇다 — 이 시험이 재는 것이 정확히
// 「한 호출이 옆 덱을 읽고 이 덱에 쓰는가」라서, 문서 키로 갈리지 않는 가짜로는 아무것도 못 잰다.
type styleHand struct {
	mu    sync.Mutex
	calls []handCall
	style map[string]map[string]any // document → describe_style 의 result
	fail  map[string]error
}

func (h *styleHand) Attached() bool { return true }

func (h *styleHand) Call(_ context.Context, document, op string, args map[string]any) (HandResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// 인자는 호출 뒤에 고쳐질 수 있으므로 **뜬 그대로** 떠 둔다.
	snap := map[string]any{}
	for k, v := range args {
		snap[k] = v
	}
	h.calls = append(h.calls, handCall{Document: document, Op: op, Args: snap})
	if err := h.fail[document]; err != nil {
		return HandResult{}, err
	}
	// 옆 덱에서 읽는 두 문. **둘 다 이 픽스처가 답해야 한다** — 하나만 답하면 다른 쪽 시험이
	// 「안 옮겨졌다」로 빨개지는데 그건 제품이 아니라 픽스처의 말이다(그렇게 한 번 겪었다).
	if op == "describe_style" || op == "read_theme_colors" {
		return HandResult{Document: document, Label: document + ".pptx", Result: h.style[document]}, nil
	}
	return HandResult{Document: document, Label: document + ".pptx", Changed: []string{"장 3개를 고쳤습니다"}}, nil
}

func (h *styleHand) seen() []handCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]handCall{}, h.calls...)
}

func sourceStyle() map[string]map[string]any {
	return map[string]map[string]any{
		"src": {
			"title": map[string]any{"font": "맑은 고딕", "size": float64(44), "color": "#000000"},
			"body":  map[string]any{"font": "맑은 고딕", "size": float64(28), "color": "#000000"},
		},
	}
}

// carryStyle 은 **빈 자리만 채운다.** 이 인자는 「저 덱으로 빈 칸을 채워라」이지 「내 말을 덮어써라」가
// 아니다 — 덮어쓰면 「제목만 파랗게, 나머지는 저 덱처럼」이 불가능해진다.
func TestCarryingAStyleFillsOnlyWhatTheCallerLeftEmpty(t *testing.T) {
	hand := &styleHand{style: sourceStyle()}
	args := map[string]any{
		"title": map[string]any{"color": "#1F4E79"}, // 부른 쪽이 정한 값
	}
	notes, err := carryStyle(context.Background(), hand, "src", args)
	if err != nil {
		t.Fatalf("서식을 못 옮겼다: %v", err)
	}

	title, _ := args["title"].(map[string]any)
	if title["color"] != "#1F4E79" {
		t.Errorf("부른 쪽이 준 색을 덮어썼다: %v", title["color"])
	}
	if title["font"] != "맑은 고딕" || title["size"] != float64(44) {
		t.Errorf("빈 칸이 안 채워졌다: %v", title)
	}
	body, _ := args["body"].(map[string]any)
	if body["font"] != "맑은 고딕" || body["size"] != float64(28) || body["color"] != "#000000" {
		t.Errorf("본문이 안 채워졌다: %v", body)
	}

	joined := strings.Join(notes, " ")
	if !strings.Contains(joined, "src.pptx") {
		t.Errorf("어느 문서를 따랐는지 안 적었다: %s", joined)
	}
	// **ea_font 는 절대 지어내지 않는다.** 실물에서 모델이 지어낸 칸이 정확히 여기다(본고딕).
	if _, set := args["ea_font"]; set {
		t.Error("ea_font 를 지어냈다 — describe_style 이 못 읽는 칸이다")
	}
	if !strings.Contains(joined, "ea_font") {
		t.Errorf("안 옮긴 칸을 소리 내어 안 적었다: %s", joined)
	}
}

// 따라갈 버릇이 없으면 **없다고 말한다.** 아무 값이나 골라 박으면 덱이 더 어지러워진다.
func TestCarryingAStyleFromADeckWithNoHabitSaysSo(t *testing.T) {
	hand := &styleHand{style: map[string]map[string]any{"src": {}}}
	args := map[string]any{}
	notes, err := carryStyle(context.Background(), hand, "src", args)
	if err != nil {
		t.Fatalf("실패하면 안 된다: %v", err)
	}
	if len(args) != 0 {
		t.Errorf("없는 것을 지어냈다: %v", args)
	}
	if !strings.Contains(strings.Join(notes, " "), "못 찾았습니다") {
		t.Errorf("못 찾았다고 말해야 한다: %v", notes)
	}
}

// 옆 덱을 못 읽으면 **조용히 넘어가지 않는다** — 조용히 넘어가면 「따랐습니다」 하고 안 따른 것이 된다.
func TestCarryingAStyleFromAnUnreachableDeckIsRefusedOutLoud(t *testing.T) {
	hand := &styleHand{style: sourceStyle(), fail: map[string]error{"src": errors.New("more than one deck is open")}}
	if _, err := carryStyle(context.Background(), hand, "src", map[string]any{}); err == nil {
		t.Fatal("거절해야 한다")
	} else if !strings.Contains(err.Error(), "list_documents") {
		t.Errorf("다음에 무엇을 하면 되는지를 안 적었다: %v", err)
	}
}

// **배선을 잰다.** 함수만 재고 핸들러가 그것을 안 부르면 초록인 채로 아무 일도 안 한다 —
// 이 저장소가 하루에 두 번 겪은 모양이다(TESTING §9.1).
func TestApplyStyleCarriesAnotherDecksStyleThroughTheMCPDoor(t *testing.T) {
	hand := &styleHand{style: sourceStyle()}
	srv := httptest.NewServer(&MCPServer{App: PPT, Hand: hand, Now: func() time.Time { return time.Unix(0, 0) }})
	defer srv.Close()

	s := newSink()
	m := mcp.NewManager(s)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := m.Attach(ctx, "", PPT.Key, srv.URL+"?deck=tgt", nil); err != nil {
		t.Fatalf("못 붙었다: %v", err)
	}
	tool := s.get("mcp__" + PPT.Key + "__apply_style")
	if tool == nil {
		t.Fatal("apply_style 이 광고되지 않았다")
	}
	res, err := tool.Execute(ctx, json.RawMessage(`{"match_document":"src"}`), port.ToolEnv{SessionID: session.SessionID("s1")})
	if err != nil {
		t.Fatalf("호출 실패: %v", err)
	}
	if res.IsError {
		t.Fatalf("도구가 에러로 답했다: %s", res.Content)
	}

	calls := hand.seen()
	if len(calls) != 2 {
		t.Fatalf("옆 덱 읽기 + 이 덱 쓰기, 둘이어야 한다: %+v", calls)
	}
	if calls[0].Document != "src" || calls[0].Op != "describe_style" {
		t.Errorf("먼저 옆 덱의 서식을 읽어야 한다: %+v", calls[0])
	}
	if calls[1].Document != "tgt" || calls[1].Op != "apply_style" {
		t.Errorf("그다음 이 덱에 써야 한다: %+v", calls[1])
	}
	title, _ := calls[1].Args["title"].(map[string]any)
	if title["font"] != "맑은 고딕" || title["size"] != float64(44) {
		t.Errorf("손이 받은 인자에 옆 덱의 서식이 없다: %+v", calls[1].Args)
	}
	// 손에게는 뜻이 없는 칸이다 — 남겨 보내면 손이 모르는 인자를 받는다.
	if _, still := calls[1].Args[matchDocumentArg]; still {
		t.Errorf("match_document 를 손에 그대로 넘겼다: %+v", calls[1].Args)
	}

	var text string
	if err := json.Unmarshal(res.Content, &text); err != nil {
		t.Fatalf("결과가 글이 아니다: %v", err)
	}
	var body struct {
		Changed []string `json:"changed"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("답이 JSON 이 아니다: %v\n%s", err, text)
	}
	// **증거가 changed 에 실려야 한다.** 카운슬이 「저 덱을 따랐다」를 재는 칸이 그것뿐이고,
	// 실물에서 거절 사유가 정확히 「증거 없음」이었다(2026-09-08).
	if len(body.Changed) == 0 || !strings.Contains(body.Changed[0], "src.pptx") {
		t.Errorf("따라온 서식이 changed 맨 앞에 안 실렸다: %v", body.Changed)
	}
}

// 자기 자신을 따르라는 호출은 **왕복을 안 만든다** — 그리고 옆 덱이 없을 때 조용히 실패하지도 않는다.
func TestMatchingYourOwnDeckIsANoOp(t *testing.T) {
	hand := &styleHand{style: sourceStyle()}
	srv := httptest.NewServer(&MCPServer{App: PPT, Hand: hand})
	defer srv.Close()

	s := newSink()
	m := mcp.NewManager(s)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := m.Attach(ctx, "", PPT.Key, srv.URL+"?deck=tgt", nil); err != nil {
		t.Fatalf("못 붙었다: %v", err)
	}
	tool := s.get("mcp__" + PPT.Key + "__apply_style")
	if _, err := tool.Execute(ctx, json.RawMessage(`{"match_document":"tgt"}`), port.ToolEnv{SessionID: session.SessionID("s1")}); err != nil {
		t.Fatalf("호출 실패: %v", err)
	}
	calls := hand.seen()
	if len(calls) != 1 || calls[0].Op != "apply_style" {
		t.Fatalf("제 덱을 따르라는 것은 왕복을 안 만든다: %+v", calls)
	}
}

// 테마 색도 같은 문으로 온다 — 그리고 **같은 층을 읽는다.** 층이 어긋나면 딴 값을 가져다 놓고
// 「따랐습니다」라고 적게 된다.
func TestCarryingThemeColoursReadsTheSameLayer(t *testing.T) {
	hand := &styleHand{style: map[string]map[string]any{
		"src": {"theme": map[string]any{
			"dark1": "#000000", "light1": "#FFFFFF",
			"accent1": "#156082", "accent2": "#E97132",
		}},
	}}
	args := map[string]any{
		"scope":  "master",
		"colors": map[string]any{"accent1": "#1F4E79"}, // 부른 쪽이 정한 값
	}
	notes, err := carryThemeColors(context.Background(), hand, "src", args)
	if err != nil {
		t.Fatalf("테마 색을 못 옮겼다: %v", err)
	}
	calls := hand.seen()
	if len(calls) != 1 || calls[0].Op != "read_theme_colors" {
		t.Fatalf("read_theme_colors 로 읽어야 한다: %+v", calls)
	}
	if calls[0].Args["scope"] != "master" {
		t.Errorf("이 호출과 같은 층을 읽어야 한다: %+v", calls[0].Args)
	}
	colors, _ := args["colors"].(map[string]any)
	if colors["accent1"] != "#1F4E79" {
		t.Errorf("부른 쪽이 준 색을 덮어썼다: %v", colors["accent1"])
	}
	if colors["dark1"] != "#000000" || colors["accent2"] != "#E97132" {
		t.Errorf("빈 칸이 안 채워졌다: %v", colors)
	}
	if !strings.Contains(strings.Join(notes, " "), "src.pptx") {
		t.Errorf("어느 문서를 따랐는지 안 적었다: %v", notes)
	}
}

// `set_theme_colors` 는 **둘 중 하나**가 있어야 한다. 앞 판은 `colors` 가 필수라 「저 덱 팔레트로」를
// 인자 검사가 먼저 거절했다 — 그래서 필수를 풀고 이 규칙을 CheckArgs 로 옮겼다. 둘 다 없으면
// **조용히 아무것도 안 하는 호출**이 되므로 여전히 거절한다.
func TestSetThemeColoursNeedsColoursOrADeckToTakeThemFrom(t *testing.T) {
	var themeTool *tool
	for _, x := range PPT.Catalogue(false) {
		if x.Name == "set_theme_colors" {
			c := x
			themeTool = &c
		}
	}
	if themeTool == nil {
		t.Fatal("set_theme_colors 가 없다")
	}
	for _, r := range themeTool.Required {
		if r == "colors" {
			t.Error("colors 가 아직 필수다 — 그러면 match_document 만 준 호출이 인자 검사에서 죽는다")
		}
	}
	if _, err := validateArgs(PPT, *themeTool, []byte(`{}`)); err == nil {
		t.Error("둘 다 없는 호출은 거절해야 한다")
	} else if !strings.Contains(err.Error(), matchDocumentArg) {
		t.Errorf("무엇을 주면 되는지 둘 다 적어야 한다: %v", err)
	}
	if _, err := validateArgs(PPT, *themeTool, []byte(`{"match_document":"src"}`)); err != nil {
		t.Errorf("match_document 만 줘도 통과해야 한다: %v", err)
	}
	if _, err := validateArgs(PPT, *themeTool, []byte(`{"colors":{"accent1":"#1F4E79"}}`)); err != nil {
		t.Errorf("colors 만 줘도 통과해야 한다: %v", err)
	}
}

// 배선 — 테마 쪽도 MCP 문을 지나 손에 닿는가.
func TestSetThemeColoursCarriesAnotherDecksPaletteThroughTheMCPDoor(t *testing.T) {
	hand := &styleHand{style: map[string]map[string]any{
		"src": {"theme": map[string]any{"accent1": "#156082", "dark1": "#000000"}},
	}}
	srv := httptest.NewServer(&MCPServer{App: PPT, Hand: hand, Now: func() time.Time { return time.Unix(0, 0) }})
	defer srv.Close()

	s := newSink()
	m := mcp.NewManager(s)
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := m.Attach(ctx, "", PPT.Key, srv.URL+"?deck=tgt", nil); err != nil {
		t.Fatalf("못 붙었다: %v", err)
	}
	tool := s.get("mcp__" + PPT.Key + "__set_theme_colors")
	if tool == nil {
		t.Fatal("set_theme_colors 가 광고되지 않았다")
	}
	res, err := tool.Execute(ctx, json.RawMessage(`{"match_document":"src","scope":"master"}`), port.ToolEnv{SessionID: session.SessionID("s1")})
	if err != nil {
		t.Fatalf("호출 실패: %v", err)
	}
	if res.IsError {
		t.Fatalf("도구가 에러로 답했다: %s", res.Content)
	}
	calls := hand.seen()
	if len(calls) != 2 || calls[0].Op != "read_theme_colors" || calls[1].Op != "set_theme_colors" {
		t.Fatalf("옆 덱 읽기 + 이 덱 칠하기, 둘이어야 한다: %+v", calls)
	}
	colors, _ := calls[1].Args["colors"].(map[string]any)
	if colors["accent1"] != "#156082" {
		t.Errorf("손이 받은 인자에 옆 덱의 팔레트가 없다: %+v", calls[1].Args)
	}
	if _, still := calls[1].Args[matchDocumentArg]; still {
		t.Errorf("match_document 를 손에 그대로 넘겼다: %+v", calls[1].Args)
	}
}

// 안내가 **모델이 실제로 서 있는 자리**에 있는가. 앞 판은 `describe_style` 설명 안에만 있었고,
// 그 글은 그 도구를 부르기로 이미 정한 모델만 읽는다 — 실물에서 모델은 그것을 건너뛰었다.
func TestTheWayToCopyAnotherDecksLookIsWhereTheModelIs(t *testing.T) {
	var applyStyle, addSlides, listDocs *tool
	for _, x := range PPT.Catalogue(false) {
		switch x.Name {
		case "apply_style":
			c := x
			applyStyle = &c
		case "add_slides":
			c := x
			addSlides = &c
		case "list_documents":
			c := x
			listDocs = &c
		}
	}
	if applyStyle == nil || addSlides == nil || listDocs == nil {
		t.Fatal("세 도구가 다 있어야 한다")
	}

	var prop *property
	for _, p := range applyStyle.Props {
		if p.Name == matchDocumentArg {
			c := p
			prop = &c
		}
	}
	if prop == nil {
		t.Fatal("apply_style 이 match_document 를 안 받는다")
	}
	if !strings.Contains(prop.Desc, "ea_font") {
		t.Error("한글 글꼴을 안 옮긴다는 것을 스키마가 안 적었다 — 실물에서 모델이 지어낸 칸이다")
	}
	if !strings.Contains(applyStyle.Desc, matchDocumentArg) {
		t.Error("apply_style 설명이 match_document 를 안 가리킨다")
	}
	// 장을 만드는 도구가 그 길을 가리켜야 한다. 모델은 거기 서 있다가 서식을 지어냈다.
	for _, p := range addSlides.Props {
		if p.Name == "match_style" && !strings.Contains(p.Desc, matchDocumentArg) {
			t.Error("add_slides 의 match_style 이 옆 덱을 따르는 길을 안 가리킨다")
		}
	}

	// 그리고 **답에도** 실려야 한다 — 설명은 그 도구를 부른 모델만 읽는다.
	hub := NewHandHub(PPT)
	hub.Join("com-a", "a.pptx")
	res, err := listDocs.Local(hub, "pid-com-a", nil)
	if err != nil {
		t.Fatalf("list_documents 실패: %v", err)
	}
	if _, has := res.Result["note"]; has {
		t.Error("덱이 하나뿐이면 옆 덱 이야기를 안 한다")
	}
	hub.Join("com-b", "b.pptx")
	res, err = listDocs.Local(hub, "pid-com-a", nil)
	if err != nil {
		t.Fatalf("list_documents 실패: %v", err)
	}
	note, _ := res.Result["note"].(string)
	if !strings.Contains(note, matchDocumentArg) {
		t.Errorf("덱이 둘일 때 답이 그 길을 안 가리킨다: %q", note)
	}
}
