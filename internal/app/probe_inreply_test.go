package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

// Forensic probe (excluded from commits): the idle-park chitchat reply must be tagged with
// InReplyTo == the answered message's origin msgID, so the TUI can pair the answer with its
// question. Without the tag an inline-answered interjection's bubble stays stranded.

// inReplyToOf returns the InReplyTo of the first assistant text PartAppended, or "".
func inReplyToOf(evs []event.Event) string {
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartText && d.Role == session.RoleAssistant {
			return d.InReplyTo
		}
	}
	return ""
}

func TestClosingTextAfterRouteIsNotTagged(t *testing.T) {
	step0 := []port.ProviderEvent{
		{Type: port.ProviderText, Text: "알겠습니다."},
		{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_route", Name: "route_interjection", Args: json.RawMessage(`{"action":"append","reason":"needs real work"}`)}},
		{Type: port.ProviderFinish},
	}
	step1 := []port.ProviderEvent{
		{Type: port.ProviderText, Text: "별도 작업으로 처리하겠습니다."},
		{Type: port.ProviderFinish},
	}
	llm := &scriptLLM{steps: [][]port.ProviderEvent{step0, step1}}
	a, s := newAsideApp(t, llm)

	// triageQueued runs the modeQueued mini-turn (route → escalate=true).
	if escalate := a.triageQueued(context.Background(), AgentSpec{Name: "default"}, s, "m_route2", "add a README too"); !escalate {
		t.Fatalf("a routed queued message must escalate")
	}
	evs, _ := a.store.Read(context.Background(), s.ID, 0)
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartText && d.Role == session.RoleAssistant && d.InReplyTo != "" {
			t.Fatalf("no reply in an escalating turn may be tagged, got InReplyTo=%q on %q", d.InReplyTo, d.Part.Text)
		}
	}
}
