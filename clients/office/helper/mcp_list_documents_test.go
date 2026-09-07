package office

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// `list_documents` 는 **헬퍼가 답한다** — 손에 안 간다. 한 대화에서 옆 덱을 읽거나 서식을 옮기려면 옆 덱의
// 키가 있어야 하는데, 그것을 알 길이 거절문뿐이었다(2026-09-07). 허브가 아는 것을 그대로 내고, 이 대화가
// 묶인 덱을 current 로 표시한다. 손이 하나도 없어도 답이 있다 — 「없다」도 답이다.
func TestListDocumentsIsAnsweredByTheHubNotAHand(t *testing.T) {
	hub := NewHandHub(PPT)
	hub.Join("com-aaaa", "a.pptx")
	hub.Join("com-bbbb", "b.pptx")
	srv := httptest.NewServer(&MCPServer{App: PPT, Hand: hub, Now: func() time.Time { return time.Unix(0, 0) }})
	defer srv.Close()

	call := func(url string) string {
		t.Helper()
		s := newSink()
		m := mcp.NewManager(s)
		defer m.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := m.Attach(ctx, "", PPT.Key, url, nil); err != nil {
			t.Fatalf("못 붙었다: %v", err)
		}
		tool := s.get("mcp__" + PPT.Key + "__list_documents")
		if tool == nil {
			t.Fatal("list_documents 가 광고되지 않았다")
		}
		res, err := tool.Execute(ctx, json.RawMessage(`{}`), port.ToolEnv{SessionID: session.SessionID("s1")})
		if err != nil {
			t.Fatalf("호출 실패: %v", err)
		}
		if res.IsError {
			t.Fatalf("도구가 에러로 답했다: %s", res.Content)
		}
		var text string
		if err := json.Unmarshal(res.Content, &text); err != nil {
			t.Fatalf("결과가 글이 아니다: %v", err)
		}
		return text
	}

	got := call(srv.URL + "?deck=pid-com-aaaa")
	var body struct {
		Document  string `json:"document"`
		Count     int    `json:"count"`
		Documents []struct {
			Document string `json:"document"`
			Label    string `json:"label"`
			Current  bool   `json:"current"`
		} `json:"documents"`
	}
	if err := json.Unmarshal([]byte(got), &body); err != nil {
		t.Fatalf("답이 JSON 이 아니다: %v\n%s", err, got)
	}
	if body.Count != 2 || len(body.Documents) != 2 {
		t.Fatalf("덱 둘을 내야 한다: %s", got)
	}
	current := map[string]bool{}
	for _, d := range body.Documents {
		current[d.Document] = d.Current
		if d.Label == "" {
			t.Errorf("이름이 비었다: %+v", d)
		}
	}
	if !current["pid-com-aaaa"] || current["pid-com-bbbb"] {
		t.Fatalf("이 대화의 덱만 current 여야 한다: %s", got)
	}

	// 손이 하나도 없어도 「없다」를 답한다 — 「붙어 있지 않다」로 죽지 않는다.
	empty := httptest.NewServer(&MCPServer{App: PPT, Hand: NewHandHub(PPT)})
	defer empty.Close()
	if got := call(empty.URL); !strings.Contains(got, `"count": 0`) {
		t.Fatalf("빈 허브는 count 0 을 답해야 한다: %s", got)
	}
}
