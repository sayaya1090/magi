package office

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// **부르는 대화가 자기 덱을 안다.** 그 대화 몫으로 붙인 등록은 주소에 덱을 싣고 오므로, 인자에
// `document` 가 없어도 어느 덱인지가 정해진다.
//
// 없으면 허브가 「덱이 둘이면 안 고른다」로 거절하고 모델은 사람에게 되묻는다 — 실물에서 그
// 화면을 봤다(2026-09-05): 작업창에서 시킨 일인데 「어느 덱을 비울까요」를 물었고, 그 대화는
// 그 덱에 묶여 있었다.
func TestTheAddressDecidesTheDeck(t *testing.T) {
	hand := &fakeHand{attached: true, answer: HandResult{Document: "wb-a", Result: map[string]any{"ok": true}}}
	srv := httptest.NewServer(&MCPServer{App: Word, Hand: hand, Now: func() time.Time { return time.Unix(0, 0) }})
	defer srv.Close()

	call := func(url, args string) {
		t.Helper()
		s := newSink()
		m := mcp.NewManager(s)
		defer m.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := m.Attach(ctx, "", Word.Key, url, nil); err != nil {
			t.Fatalf("못 붙었다: %v", err)
		}
		tool := s.get("mcp__" + Word.Key + "__list_paragraphs")
		if tool == nil {
			t.Fatal("도구가 없다")
		}
		if _, err := tool.Execute(ctx, json.RawMessage(args), port.ToolEnv{SessionID: session.SessionID("s1")}); err != nil {
			t.Fatalf("호출 실패: %v", err)
		}
	}

	call(srv.URL+"?deck=wb-a", `{}`)
	if got := lastDoc(hand); got != "wb-a" {
		t.Errorf("주소의 덱이 안 쓰였다: %q — 그러면 모델이 사람에게 되묻는다", got)
	}

	// **인자가 이긴다.** 한 대화에서 옆 덱을 읽는 일은 있고, 그때 주소가 이겨 버리면 그 부탁이
	// 조용히 엉뚱한 덱에 간다.
	call(srv.URL+"?deck=wb-a", `{"document":"wb-b"}`)
	if got := lastDoc(hand); got != "wb-b" {
		t.Errorf("인자로 댄 덱이 졌다: %q", got)
	}

	// 주소에 덱이 없으면 여태처럼 빈 값 — 허브의 「하나뿐이면 그것」 규칙이 답한다.
	call(srv.URL, `{}`)
	if got := lastDoc(hand); got != "" {
		t.Errorf("없는 덱을 지어냈다: %q", got)
	}
}

// lastDoc 은 손이 마지막으로 받은 문서. **받은 것을 본다** — 답에 실린 것을 보면 손이 무엇으로
// 골랐는지가 아니라 손이 무엇을 돌려줬는지를 재게 된다.
func lastDoc(h *fakeHand) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) == 0 {
		return "(호출 없음)"
	}
	return h.calls[len(h.calls)-1].Document
}
