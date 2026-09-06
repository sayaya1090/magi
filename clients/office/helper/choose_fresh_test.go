package office

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// **덱마다 자기 대화다.**
//
// 컴패니언에 아직 대화가 없으면(갓 뜬 데몬) `Bind(socket, "")` 이 「아직 대화가 없습니다」로
// 거절한다 — 그 창은 붙긴 했는데 말을 걸 곳이 없다. 사람이 물었다(2026-09-05): "덱마다 새 세션
// 아니야?" 맞는 말이고, 그때까지는 `/api/fresh` 를 손으로 불러 메우고 있었다.
func TestADeckWithNoConversationGetsOne(t *testing.T) {
	bs := NewBridges()
	opened := 0
	api := &API{App: Word,
		Bridge: NewBridge(), Bridges: bs, Attachments: NewAttachments(Word),
		Bolt:  func(socket, url, token string) ([]string, error) { return []string{"list_paragraphs"}, nil },
		Fresh: func(socket, _ string) (string, error) { opened++; return "sess-new", nil },
	}
	body, _ := json.Marshal(map[string]string{"socket": "/sock"}) // 세션 없이 — 갓 뜬 데몬
	w := httptest.NewRecorder()
	api.choose(w, httptest.NewRequest("POST", "/api/choose?deck=deck-a", strings.NewReader(string(body))))

	if opened != 1 {
		t.Fatalf("대화를 안 열었다(opened=%d) — 그 창은 말을 걸 곳이 없다: %d %s", opened, w.Code, w.Body.String())
	}
	if _, sid, _ := bs.For("deck-a").Bound(); sid != "sess-new" {
		t.Errorf("연 대화에 안 묶였다: %q", sid)
	}

	// **이미 대화를 댄 호출은 안 건드린다** — 사람이 명단에서 고른 그 대화로 가야 한다.
	opened = 0
	body, _ = json.Marshal(map[string]string{"socket": "/sock", "session": "sess-old"})
	api.choose(httptest.NewRecorder(),
		httptest.NewRequest("POST", "/api/choose?deck=deck-b", strings.NewReader(string(body))))
	if opened != 0 {
		t.Errorf("댄 대화가 있는데 새로 열었다")
	}
	if _, sid, _ := bs.For("deck-b").Bound(); sid != "sess-old" {
		t.Errorf("고른 대화로 안 갔다: %q", sid)
	}
}
