package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/mcp"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// 1×1 PNG — 내용을 보고 그림이라 판단하는 ReadImage 를 지나가야 한다.
const tinyPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// 배경 그림은 add_image 와 같은 길로 **헬퍼가 파일을 읽어** 바이트를 싣는다. 도구 이름만 보고
// add_image 에만 실으면 set_background{kind:picture} 는 경로만 들고 판에 가서 「바이트가 안 왔다」로
// 죽는다 — 광고는 됐는데 실행이 안 되는 계약이다.
func TestBackgroundPictureBytesAreReadByTheHelper(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bg.png")
	raw, _ := base64.StdEncoding.DecodeString(tinyPNG)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	hand := &fakeHand{attached: true, answer: HandResult{Document: "doc-1", Result: map[string]any{"background": "picture"}}}
	srv := httptest.NewServer(&MCPServer{Hand: hand, Now: func() time.Time { return time.Unix(0, 0) }})
	defer srv.Close()
	s := newSink()
	mgr := mcp.NewManager(s)
	defer mgr.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := mgr.Attach(ctx, "", ServerName, srv.URL, nil); err != nil {
		t.Fatalf("못 붙었다: %v", err)
	}
	tool := s.get("mcp__" + ServerName + "__set_background")
	if tool == nil {
		t.Fatalf("set_background 가 없다: %v", s.names())
	}
	args, _ := json.Marshal(map[string]any{"slide": 1, "kind": "picture", "path": path})
	res, err := tool.Execute(ctx, json.RawMessage(args), port.ToolEnv{SessionID: session.SessionID("sess-1")})
	if err != nil || res.IsError {
		t.Fatalf("호출 실패: %v / %s", err, res.Content)
	}
	hand.mu.Lock()
	defer hand.mu.Unlock()
	if len(hand.calls) != 1 {
		t.Fatalf("손 호출 %d회", len(hand.calls))
	}
	got := hand.calls[0].Args
	if got["image_base64"] != tinyPNG || got["image_mime"] != "image/png" {
		t.Fatalf("판에 그림 바이트가 안 갔다: %v", got)
	}
}
