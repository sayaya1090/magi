package main

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

// **만드는 것과 싣는 것은 다른 사실이다.** `imageBlock` 이 안내를 돌려줘도 핸들러가 그것을
// 본문에 안 실으면 모델은 아무 말도 못 듣고, 앞서와 똑같이 파일을 열려다 셸로 간다. 돌연변이가
// 그 구멍을 짚었다(2026-09-04): 안내를 버리게 고쳐도 다른 시험은 전부 초록이었다.
func TestTheAnswerActuallyCarriesThePictureNote(t *testing.T) {
	hand := &fakeHand{attached: true, answer: HandResult{
		Document: "doc-7",
		Result:   map[string]any{"image_base64": "AAAA", "image_mime": "image/png"},
	}}
	srv := httptest.NewServer(&MCPServer{Hand: hand, Now: func() time.Time { return time.Unix(0, 0) }})
	defer srv.Close()

	s := newSink()
	mgr := mcp.NewManager(s)
	defer mgr.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := mgr.Attach(ctx, ServerName, srv.URL, nil); err != nil {
		t.Fatalf("못 붙었다: %v", err)
	}
	tool := s.get("mcp__" + ServerName + "__render_shape")
	if tool == nil {
		t.Fatalf("render_shape 가 없다: %v", s.names())
	}
	res, err := tool.Execute(ctx, json.RawMessage(`{"slide":1,"shape_id":"3"}`),
		port.ToolEnv{SessionID: session.SessionID("sess-1")})
	if err != nil || res.IsError {
		t.Fatalf("호출 실패: %v / %s", err, res.Content)
	}
	body := string(res.Content)
	if !strings.Contains(body, "파일로 열려고 하지 마세요") {
		t.Errorf("답이 안내를 안 실었다:\n%s", body)
	}
}
