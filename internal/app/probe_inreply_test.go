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

func TestHandleAsideReplyCarriesInReplyTo(t *testing.T) {
	llm := &scriptLLM{steps: [][]port.ProviderEvent{asideText("1862년입니다.")}}
	a, s := newAsideApp(t, llm)
	const origMsgID = "m_origq"
	a.enqueueInterject(context.Background(), s.ID, origMsgID, "레미제라블 출판년도?")

	acted := a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "review the repo", origMsgID, "레미제라블 출판년도?")
	if acted {
		t.Fatalf("chitchat must not act on the work")
	}
	evs, _ := a.store.Read(context.Background(), s.ID, 0)
	if got := inReplyToOf(evs); got != origMsgID {
		t.Fatalf("inline chitchat reply must tag InReplyTo=%q, got %q", origMsgID, got)
	}
}

// An ack that PRECEDES a route call must NOT be tagged: the routed message resurfaces as its
// own turn (ResurfacedFrom reorders the bubble), so tagging the ack too would move the same
// bubble twice and strand the ack with no question above it.
func TestAckBeforeRouteIsNotTagged(t *testing.T) {
	step := []port.ProviderEvent{
		{Type: port.ProviderText, Text: "알겠습니다, 처리하겠습니다."},
		{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_route", Name: "route_interjection", Args: json.RawMessage(`{"action":"append","reason":"needs real work"}`)}},
		{Type: port.ProviderFinish},
	}
	llm := &scriptLLM{steps: [][]port.ProviderEvent{step}}
	a, s := newAsideApp(t, llm)
	const origMsgID = "m_route"
	a.enqueueInterject(context.Background(), s.ID, origMsgID, "add a README too")

	a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "big delegated task", origMsgID, "add a README too")
	evs, _ := a.store.Read(context.Background(), s.ID, 0)
	if got := inReplyToOf(evs); got != "" {
		t.Fatalf("an ack before a route must NOT be tagged (double-move), got InReplyTo=%q", got)
	}
}

// A modeAside interjection that ROUTES (re-anchors the running task) must NOT tag its
// confirmation text, even when that text arrives in a later tool-call-free step. Regression
// from the live smoke test: "docs 디렉토리만 읽어도 돼" routed (append) in step 0, then the
// model confirmed in step 1 (no call) — tagging that pulled the steer's question bubble out of
// the main flow into a detached Q&A group. modeAside route sets didRoute (not escalate), so the
// guard must check the route effect, not just escalate.
func TestAsideRouteConfirmationIsNotTagged(t *testing.T) {
	step0 := []port.ProviderEvent{ // route only, no visible text
		{Type: port.ProviderToolCall, ToolCall: &session.ToolCall{CallID: "c_route", Name: "route_interjection", Args: json.RawMessage(`{"action":"append","reason":"narrow scope to docs"}`)}},
		{Type: port.ProviderFinish},
	}
	step1 := []port.ProviderEvent{ // confirmation text, no call
		{Type: port.ProviderText, Text: "알겠습니다 — docs 디렉토리만 검토하겠습니다."},
		{Type: port.ProviderFinish},
	}
	llm := &scriptLLM{steps: [][]port.ProviderEvent{step0, step1}}
	a, s := newAsideApp(t, llm)
	const origMsgID = "m_scope"
	a.enqueueInterject(context.Background(), s.ID, origMsgID, "docs 디렉토리 아래만 읽어도 돼")

	acted := a.handleAside(context.Background(), AgentSpec{Name: "default"}, s, 0, "review the docs", origMsgID, "docs 디렉토리 아래만 읽어도 돼")
	if !acted {
		t.Fatalf("a routing aside must report acted=true")
	}
	evs, _ := a.store.Read(context.Background(), s.ID, 0)
	for _, e := range evs {
		if e.Type != event.TypePartAppended {
			continue
		}
		var d event.PartAppendedData
		if json.Unmarshal(e.Data, &d) == nil && d.Part.Kind == session.PartText && d.Role == session.RoleAssistant && d.InReplyTo != "" {
			t.Fatalf("a routing aside's confirmation must not be tagged (detaches the steer), got InReplyTo=%q on %q", d.InReplyTo, d.Part.Text)
		}
	}
}

// The escalate effect is sticky across the mini-loop's steps: in a modeQueued triage, an
// ack+route in step 0 followed by a tool-call-free closing sentence in step 1 must leave
// NEITHER reply tagged. The turn escalates (drain resurfaces via ResurfacedFrom), so a tagged
// closing reply would double-move the very bubble the resurface already relocates.
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
