package mcp

import (
	"context"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

// **문에 주인을 실어 본다.** `mcpTool` 의 주인별 손은 단위로 쟀는데(owner_test.go), `Manager.Attach`
// 에 주인을 실어 부른 시험은 없었다 — 그래서 `ours` 검사가 주인 없는 열쇠를 보는 구멍이 실물에서야
// 났다(2026-09-05: PowerPoint 덱 둘 다 `"ppt" attached and then vanished`). 배선을 지나는 시험이다.
func TestAttachWithAnOwnerIsNotReadAsVanished(t *testing.T) {
	srv := mcpHTTP(t, "list_slides")
	defer srv.Close()
	m := NewManager(builtin.NewRegistry())
	defer m.Close()

	got, err := m.Attach(context.Background(), "sess-a", "ppt", srv.URL, nil)
	if err != nil {
		t.Fatalf("주인을 실어 붙였더니 거절당했다: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("도구가 %d개: %v", len(got), got)
	}
	// 같은 이름, 다른 주인 — **덮지 않고 합친다.** 둘 다 살아야 한다.
	srv2 := mcpHTTP(t, "list_slides")
	defer srv2.Close()
	if _, err := m.Attach(context.Background(), "sess-b", "ppt", srv2.URL, nil); err != nil {
		t.Fatalf("둘째 주인이 거절당했다: %v", err)
	}
	// 놓을 때도 자기 것만 — 첫째를 떼도 둘째는 남는다.
	if removed, err := m.Detach("sess-a", "ppt"); err != nil || !removed {
		t.Fatalf("첫째 주인을 못 뗐다: %v %v", removed, err)
	}
	if removed, _ := m.Detach("sess-b", "ppt"); !removed {
		t.Fatal("둘째 주인이 첫째와 같이 사라졌다")
	}
}
