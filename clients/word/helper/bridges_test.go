package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// **덱 하나에 대화 하나.** 사람이 PowerPoint 를 둘 띄웠더니 양쪽 작업창에 같은 말이 흘렀다
// (2026-09-04 제보). 도구 층은 문서별로 갈리는데 대화 바인딩과 스트림 구독이 안 갈렸다.
func TestEachDeckGetsItsOwnChat(t *testing.T) {
	bs := NewBridges()
	a, b := bs.For("deck-a"), bs.For("deck-b")
	if a == b {
		t.Fatal("두 덱이 한 대화를 나눠 가졌다")
	}
	if again := bs.For("deck-a"); again != a {
		t.Error("같은 덱이 부를 때마다 새 대화가 생긴다 — 그러면 아무도 앞말을 못 듣는다")
	}
	// 이름을 안 실어 보내는 길도 한 덱이다. 그 길이 없으면 옛 창이 아무 데도 못 붙는다.
	if bs.For("") == nil {
		t.Error("열쇠 없는 덱이 없다")
	}
}

// 요청은 자기 덱의 대화에 말한다. 열쇠가 없으면 옛 하나로 떨어진다.
func TestRequestPicksItsOwnChat(t *testing.T) {
	bs := NewBridges()
	fallback := NewBridge()
	api := &API{Bridge: fallback, Bridges: bs}

	same := api.chat(httptest.NewRequest("GET", "/api/status?deck=alpha", nil))
	if same != bs.For("alpha") {
		t.Error("deck=alpha 요청이 alpha 의 대화로 안 갔다")
	}
	if api.chat(httptest.NewRequest("GET", "/api/status?deck=beta", nil)) == same {
		t.Error("다른 덱이 같은 대화로 갔다 — 이것이 제보된 증상 그 자체다")
	}
	if api.chat(httptest.NewRequest("GET", "/api/status", nil)) != fallback {
		t.Error("이름 없는 요청이 옛 대화로 안 떨어졌다")
	}
	// 등록부가 아예 없어도 선다 — 시험과 옛 배선이 그 길로 돈다.
	if (&API{Bridge: fallback}).chat(httptest.NewRequest("GET", "/x?deck=alpha", nil)) != fallback {
		t.Error("등록부 없이 부르면 넘어진다")
	}
}

// **한 세션에 두 덱이 붙으면 갈라 놓은 뜻이 없어진다.** 명단에서 고른 것은 「저 컴패니언」이지
// 「저 대화」가 아니므로, 이미 남이 든 대화면 같은 데몬에 새 대화를 연다.
func TestChoosingATakenSessionOpensAFreshOne(t *testing.T) {
	bs := NewBridges()
	held := bs.For("deck-a")
	if err := held.Bind("/sock", "sess-1"); err != nil {
		t.Skipf("이 판에서는 붙기가 안 된다: %v", err)
	}
	if who, taken := bs.Holder("sess-1"); !taken || who != "deck-a" {
		t.Fatalf("누가 들고 있는지를 못 찾는다: %q %v", who, taken)
	}
	if _, taken := bs.Holder("sess-2"); taken {
		t.Error("아무도 안 든 대화를 들고 있다고 한다")
	}
	if _, taken := bs.Holder(""); taken {
		t.Error("빈 이름을 대화로 셌다")
	}

	opened := ""
	// `choose` 는 먼저 도구를 붙인다 — 그 자리를 안 채우면 nil 이 터진다. 여기서 재려는 것은
	// 그 뒤의 갈래(남이 든 대화면 새 대화)라, 붙기는 성공한 것으로 세운다.
	api := &API{
		Bridge: NewBridge(), Bridges: bs,
		Attachments: NewAttachments(),
		Bolt:        func(socket, url, token string) ([]string, error) { return []string{"list_paragraphs"}, nil },
		Fresh: func(socket string) (string, error) {
			opened = socket
			return "sess-new", nil
		},
	}
	body, _ := json.Marshal(map[string]string{"socket": "/sock", "session": "sess-1"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/choose?deck=deck-b", strings.NewReader(string(body)))
	api.choose(w, r)
	if opened != "/sock" {
		t.Errorf("남이 든 대화를 그대로 나눠 줬다 — 새 대화를 안 열었다 (opened=%q, 답 %d: %s)",
			opened, w.Code, w.Body.String())
	}
	if _, sid, _ := bs.For("deck-b").Bound(); sid == "sess-1" {
		t.Errorf("두 덱이 같은 대화에 붙었다: %s", sid)
	}
	if w.Code >= http.StatusInternalServerError {
		t.Errorf("답이 %d: %s", w.Code, w.Body.String())
	}
}
