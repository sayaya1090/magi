package tui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// Forensic probes for the reqID/spinner/inline-group feature. Excluded from commits
// (probe_*), they pin the wiring the shipped tests (resurface_test.go) don't cover:
// reqID stamping, turn-ownership for the spinner, and inline-answer group extraction.

func userPrompt(text, msgID, resurfacedFrom string) event.Event {
	data, _ := json.Marshal(event.PromptSubmittedData{
		MessageID:      msgID,
		Parts:          []session.Part{{Kind: session.PartText, Text: text}},
		ResurfacedFrom: resurfacedFrom,
	})
	return event.Event{Type: event.TypePromptSubmitted, Actor: event.Actor{Kind: event.ActorUser, ID: "tui"}, Data: data}
}

func replyPart(text, inReplyTo string) event.PartAppendedData {
	return event.PartAppendedData{Part: session.Part{Kind: session.PartText, Text: text}, InReplyTo: inReplyTo}
}

// (a) A locally-added user bubble learns its reqID from the prompt.submitted event, and
// the fresh submit that started the turn claims the spinner (turnReqID). A queued
// interjection landing mid-turn (awaiting flag already cleared) must NOT steal it.
func TestReqID_StampAndTurnOwnership(t *testing.T) {
	m := &Model{running: true, awaitingTurnReqID: true, blocks: []block{
		{kind: blockUser, text: "build the parser"}, // added locally by sendPrompt
	}}
	m.applyEvent(userPrompt("build the parser", "m_main", ""))
	if m.blocks[0].reqID != "m_main" {
		t.Fatalf("submit bubble not stamped: %+v", m.blocks[0])
	}
	if m.turnReqID != "m_main" {
		t.Fatalf("fresh submit must own the spinner, turnReqID=%q", m.turnReqID)
	}
	if m.awaitingTurnReqID {
		t.Errorf("awaiting flag must clear once the turn's prompt lands")
	}

	// A mid-turn interjection: a new user bubble + its prompt.submitted while running.
	m.blocks = append(m.blocks, block{kind: blockUser, text: "also how's it going?"})
	m.applyEvent(userPrompt("also how's it going?", "m_interj", ""))
	if m.blocks[1].reqID != "m_interj" {
		t.Fatalf("interjection bubble not stamped: %+v", m.blocks[1])
	}
	if m.turnReqID != "m_main" {
		t.Fatalf("a queued interjection must NOT steal the spinner from the running turn, turnReqID=%q", m.turnReqID)
	}
}

// (b) An inline interjection answer (InReplyTo set) pulls its stranded question bubble
// down to just above the answer, forming a [question → answer] pair lifted out of the
// main-task flow. The question sits directly above its answer, main work stays above.
func TestInlineAnswer_GroupExtraction(t *testing.T) {
	m := &Model{blocks: []block{
		{kind: blockUser, reqID: "m_q", text: "레미제라블 출판년도?"}, // stranded up top
		{kind: blockToolCall, name: "read", args: `{"path":"README.md"}`, done: true, ok: true},
		{kind: blockAssistant, text: "…메인 작업 진행 중…"},
	}}
	m.onPartAppended(replyPart("1862년입니다.", "m_q"), time.Time{})

	// 4 blocks now: main work (tool+assistant), then [question, answer] adjacent.
	if len(m.blocks) != 4 {
		t.Fatalf("want 4 blocks, got %d", len(m.blocks))
	}
	q, ans := m.blocks[2], m.blocks[3]
	if q.kind != blockUser || q.reqID != "m_q" {
		t.Fatalf("question not directly above the answer: %+v", q)
	}
	if ans.kind != blockAssistant || ans.text != "1862년입니다." {
		t.Fatalf("answer not last: %+v", ans)
	}
	// The main-task work is undisturbed above the pair.
	if m.blocks[0].kind != blockToolCall || m.blocks[1].kind != blockAssistant {
		t.Fatalf("main-task blocks disturbed: %+v", m.blocks[:2])
	}
}

// A reply whose InReplyTo matches nothing (or an ordinary reply) must not move anything.
func TestInlineAnswer_NoMatchIsNoop(t *testing.T) {
	base := []block{{kind: blockUser, reqID: "m_a", text: "q"}, {kind: blockAssistant, text: "x"}}
	m := &Model{blocks: append([]block(nil), base...)}
	m.onPartAppended(replyPart("orphan answer", "m_missing"), time.Time{})
	if len(m.blocks) != 3 || m.blocks[0].reqID != "m_a" {
		t.Fatalf("unmatched InReplyTo must not reorder: %+v", m.blocks)
	}
}

// (c) The spinner reverts to ▌ when the turn finishes: turnReqID is cleared.
func TestTurnFinished_ClearsSpinnerOwner(t *testing.T) {
	m := &Model{running: true, turnReqID: "m_main"}
	fd, _ := json.Marshal(event.TurnFinishedData{})
	m.applyEvent(event.Event{Type: event.TypeTurnFinished, Data: fd})
	if m.running || m.turnReqID != "" {
		t.Fatalf("turn finish must clear running+turnReqID, got running=%v turnReqID=%q", m.running, m.turnReqID)
	}
}

// (c2) A mid-turn cache reset can re-cache the in-flight bubble WITH a spinner frame; when
// the turn finishes, that poisoned entry (and the tail) must be dropped so the bubble
// re-renders as ▌ instead of a frozen spinner glyph.
func TestSpinnerCache_ClearedOnFinish(t *testing.T) {
	m := &Model{blocks: []block{
		{kind: blockAssistant, text: "main work"},
		{kind: blockUser, reqID: "m_main", text: "do it"}, // in-flight bubble at index 1
		{kind: blockAssistant, text: "trailing"},
	}}
	m.cache = []string{"cached-0", "cached-spinner-1", "cached-2"} // index 1 poisoned
	m.clearSpinnerCache("m_main")
	if len(m.cache) != 1 || m.cache[0] != "cached-0" {
		t.Fatalf("cache must truncate at the spinning bubble's index, got %v", m.cache)
	}
	// Unknown reqID is a no-op.
	m.cache = []string{"a", "b", "c"}
	m.clearSpinnerCache("m_absent")
	if len(m.cache) != 3 {
		t.Fatalf("unknown reqID must not touch the cache, got %v", m.cache)
	}
}

// (e) The queued glyph lifecycle: a mid-turn steer bubble is flagged queued (waiting),
// and every exit path clears it — answered inline (moveUserBlockBefore), resurfaced as its
// own turn (moveUserBlockToEnd), or the turn finishing and the queue draining.
func TestQueuedGlyph_Lifecycle(t *testing.T) {
	// Inline answer clears queued on the moved question bubble.
	m := &Model{blocks: []block{
		{kind: blockUser, reqID: "m_q", text: "question", queued: true},
		{kind: blockAssistant, text: "main work"},
	}}
	m.onPartAppended(replyPart("answer", "m_q"), time.Time{})
	for _, b := range m.blocks {
		if b.kind == blockUser && b.queued {
			t.Fatalf("inline-answered bubble must clear queued: %+v", b)
		}
	}

	// Resurface clears queued on the moved-to-end bubble.
	m = &Model{running: true, blocks: []block{
		{kind: blockAssistant, text: "running"},
		{kind: blockUser, reqID: "m_r", text: "steer", queued: true},
	}}
	m.applyEvent(userPrompt("steer", "m_new", "m_r"))
	if last := m.blocks[len(m.blocks)-1]; last.reqID != "m_r" || last.queued {
		t.Fatalf("resurfaced bubble must move to end and clear queued: %+v", last)
	}

	// Turn finish drains the queue: any bubble still flagged queued is cleared, and its
	// (possibly spinner-poisoned) cache prefix dropped.
	m = &Model{running: true, blocks: []block{
		{kind: blockUser, reqID: "m_a", text: "a"},
		{kind: blockUser, reqID: "m_b", text: "b", queued: true},
		{kind: blockUser, reqID: "m_c", text: "c", queued: true},
	}}
	m.cache = []string{"c0", "c1", "c2"}
	fd, _ := json.Marshal(event.TurnFinishedData{})
	m.applyEvent(event.Event{Type: event.TypeTurnFinished, Data: fd})
	for _, b := range m.blocks {
		if b.queued {
			t.Fatalf("turn finish must clear all queued flags: %+v", b)
		}
	}
	if len(m.cache) != 1 { // truncated at the earliest queued index (1)
		t.Fatalf("cache must truncate at the first cleared bubble, got %v", m.cache)
	}
}

// (d) A resurfacing interjection re-anchors the spinner to the resurfaced request and
// matches its bubble by reqID (not text), so a duplicate-text prompt pairs correctly.
func TestResurface_ReqIDMatchAndSpinnerReanchor(t *testing.T) {
	m := &Model{running: true, blocks: []block{
		{kind: blockUser, reqID: "m_first", text: "status?"},  // an earlier, same-text prompt
		{kind: blockAssistant, text: "done with the first"},   //
		{kind: blockUser, reqID: "m_second", text: "status?"}, // the one that resurfaces
	}}
	m.applyEvent(userPrompt("status?", "m_new", "m_second"))
	last := m.blocks[len(m.blocks)-1]
	if last.reqID != "m_second" {
		t.Fatalf("reqID match must move the RIGHT duplicate-text bubble, got %+v", last)
	}
	if m.turnReqID != "m_second" {
		t.Fatalf("resurface must re-anchor the spinner to the resurfaced request, turnReqID=%q", m.turnReqID)
	}
	// m_first bubble stays put (only m_second moved).
	if m.blocks[0].reqID != "m_first" {
		t.Fatalf("the other same-text bubble must be untouched: %+v", m.blocks[0])
	}
}
