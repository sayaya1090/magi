package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// 한 이름에 대화 둘. **덮지 않고 합친다** — 레지스트리는 이름으로 키를 잡으므로, 둘째 등록이
// 첫째 도구 객체를 갈아 치우면 첫째 대화는 자기 손을 조용히 잃는다. PowerPoint 창 둘이 정확히
// 그 모양이다(2026-09-04).
func TestOneNameHoldsAHandPerConversation(t *testing.T) {
	a, b := &Client{}, &Client{}
	tool := &mcpTool{name: "mcp__ppt__list_slides", byOwner: map[string]*Client{"sess-a": a}}

	if !tool.VisibleTo("sess-a") {
		t.Error("붙인 대화에 안 보인다")
	}
	if tool.VisibleTo("sess-b") {
		t.Error("남의 대화에 보인다 — 그 대화는 이 손을 붙인 적이 없다")
	}

	tool.adopt("sess-b", b)
	if !tool.VisibleTo("sess-a") || !tool.VisibleTo("sess-b") {
		t.Error("합친 뒤에 한쪽이 사라졌다")
	}
	if tool.handFor("sess-a") != a || tool.handFor("sess-b") != b {
		t.Error("부를 때 자기 손으로 안 간다")
	}

	// **놓는 것도 자기 것만.** 통째로 떼면 아직 붙어 있는 대화가 아무 잘못 없이 손을 잃는다.
	if left := tool.release("sess-a"); left != 1 {
		t.Errorf("남은 손이 %d 개다", left)
	}
	if tool.VisibleTo("sess-a") || !tool.VisibleTo("sess-b") {
		t.Error("놓은 쪽이 남았거나 남은 쪽이 사라졌다")
	}
	if left := tool.release("sess-b"); left != 0 {
		t.Errorf("마지막을 놓았는데 %d 개가 남았다", left)
	}
}

// 주인 없는 손은 **모두의 것**이다 — config 로 선언한 서버, 세션을 안 실어 보내는 옛 클라이언트.
// 이 갈래가 깨지면 이 변경은 하위호환이 아니게 된다.
func TestAnUnownedHandIsEveryonesAndStaysDefault(t *testing.T) {
	all := &Client{}
	tool := &mcpTool{name: "mcp__editor__open", byOwner: map[string]*Client{"": all}}
	for _, sid := range []string{"", "sess-a", "sess-b"} {
		if !tool.VisibleTo(sid) {
			t.Errorf("%q 에 안 보인다 — 주인 없는 손은 모두의 것이다", sid)
		}
		if tool.handFor(sid) != all {
			t.Errorf("%q 가 다른 손으로 갔다", sid)
		}
	}
	// 대화 하나가 자기 손을 더해도 나머지는 여전히 기본 손을 쓴다.
	mine := &Client{}
	tool.adopt("sess-a", mine)
	if tool.handFor("sess-a") != mine || tool.handFor("sess-b") != all {
		t.Error("자기 손이 있으면 자기 것, 없으면 기본 — 그 규칙이 안 지켜진다")
	}
}

// **광고를 막는 것으로는 모자란다.** 이름을 이미 아는 모델은 그냥 부른다.
func TestACallFromAnotherConversationIsRefused(t *testing.T) {
	tool := &mcpTool{name: "mcp__ppt__add_slide", byOwner: map[string]*Client{"sess-a": {}}}
	res, err := tool.Execute(t.Context(), json.RawMessage(`{}`),
		port.ToolEnv{SessionID: session.SessionID("sess-b")})
	if err != nil {
		t.Fatalf("거절은 에러가 아니라 결과여야 한다: %v", err)
	}
	if !res.IsError {
		t.Fatal("남의 대화 손을 그냥 불렀다")
	}
	var why string
	_ = json.Unmarshal(res.Content, &why)
	if why == "" {
		t.Error("사유를 안 적었다")
	}
}

// 코어는 주인을 **묻기만** 한다. 안 답하는 도구는 모두의 것 — 모든 빌트인이 그렇다.
func TestAToolThatDoesNotDeclareOwnershipIsEveryones(t *testing.T) {
	if !port.VisibleToSession(plainTool{}, "sess-a") {
		t.Error("주인을 안 밝힌 도구가 가려졌다 — 빌트인이 전부 사라진다")
	}
}

type plainTool struct{}

func (plainTool) Name() string            { return "read" }
func (plainTool) Description() string     { return "" }
func (plainTool) Schema() json.RawMessage { return nil }
func (plainTool) Execute(context.Context, json.RawMessage, port.ToolEnv) (session.ToolResult, error) {
	return session.ToolResult{}, nil
}
