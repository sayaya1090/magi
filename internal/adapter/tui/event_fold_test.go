package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// These exercise the pure state-folding methods extracted out of applyEvent, so
// each can be verified in isolation without pumping a whole event stream.

func part(kind session.PartKind, text string) event.PartAppendedData {
	return event.PartAppendedData{Part: session.Part{Kind: kind, Text: text}}
}

func toolCallPart(name, args string) event.PartAppendedData {
	return event.PartAppendedData{Part: session.Part{
		Kind:     session.PartToolCall,
		ToolCall: &session.ToolCall{Name: name, Args: json.RawMessage(args)},
	}}
}

func TestOnPartAppended_TextAndReasoningAppendBlocks(t *testing.T) {
	m := &Model{liveText: "streaming…", liveThink: "thinking…"}
	m.onPartAppended(part(session.PartReasoning, "because X"))
	m.onPartAppended(part(session.PartText, "final answer"))

	if m.liveText != "" || m.liveThink != "" {
		t.Errorf("live buffers not cleared: text=%q think=%q", m.liveText, m.liveThink)
	}
	if len(m.blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(m.blocks))
	}
	if m.blocks[0].kind != blockReasoning || m.blocks[0].text != "because X" {
		t.Errorf("reasoning block wrong: %+v", m.blocks[0])
	}
	if m.blocks[1].kind != blockAssistant || m.blocks[1].text != "final answer" {
		t.Errorf("assistant block wrong: %+v", m.blocks[1])
	}
}

func TestOnPartAppended_CollapsesNearDuplicateResend(t *testing.T) {
	// A long answer the council rejected and the model re-sent nearly verbatim must
	// collapse to a stub instead of re-flooding the transcript.
	long := strings.Repeat("This is a substantial paragraph of the answer. ", 20)
	m := &Model{}
	m.onPartAppended(part(session.PartText, long))
	// Re-sent verbatim except for whitespace (sameAnswer ignores whitespace runs).
	m.onPartAppended(part(session.PartText, "  "+strings.ReplaceAll(long, " ", "\n")))

	if len(m.blocks) != 2 {
		t.Fatalf("want 2 blocks (full + stub), got %d", len(m.blocks))
	}
	if m.blocks[0].kind != blockAssistant {
		t.Errorf("first block should be the full assistant answer: %+v", m.blocks[0])
	}
	if m.blocks[1].kind != blockInfo || !strings.Contains(m.blocks[1].text, "재응답") {
		t.Errorf("duplicate resend should collapse to an info stub: %+v", m.blocks[1])
	}
}

func TestOnPartAppended_DistinctAnswersNotCollapsed(t *testing.T) {
	m := &Model{}
	m.onPartAppended(part(session.PartText, strings.Repeat("alpha ", 100)))
	m.onPartAppended(part(session.PartText, strings.Repeat("beta ", 100)))
	if len(m.blocks) != 2 || m.blocks[1].kind != blockAssistant {
		t.Fatalf("distinct answers must both render as assistant blocks: %+v", m.blocks)
	}
}

func TestOnPartAppended_ToolCallTracksStepsAndFiles(t *testing.T) {
	m := &Model{}
	m.onPartAppended(toolCallPart("read", `{"path":"a.go"}`))
	m.onPartAppended(toolCallPart("write", `{"path":"b.go"}`))
	m.onPartAppended(toolCallPart("edit", `{"path":"b.go"}`)) // same file, counts once

	if m.turnSteps != 3 {
		t.Errorf("turnSteps: got %d, want 3 (every tool call counts)", m.turnSteps)
	}
	// Only write/edit/multiedit register a touched file; read does not.
	if len(m.turnFiles) != 1 || !m.turnFiles["b.go"] {
		t.Errorf("turnFiles should be {b.go}: %v", m.turnFiles)
	}
	tc := 0
	for _, b := range m.blocks {
		if b.kind == blockToolCall {
			tc++
		}
	}
	if tc != 3 {
		t.Errorf("want 3 toolCall blocks, got %d", tc)
	}
}

func TestOnTurnFinished_TearsDownAndFreezesUsage(t *testing.T) {
	m := &Model{
		running:       true,
		liveText:      "x",
		liveThink:     "y",
		activeAgents:  []string{"researcher"},
		councilRound:  2,
		councilMember: "Melchior",
		turnStart:     time.Now().Add(-2 * time.Second),
		panes:         []*agentPane{{done: false}},
	}
	data, _ := json.Marshal(event.TurnFinishedData{Usage: event.Usage{In: 1234, Out: 567}})
	m.onTurnFinished(event.Event{Type: event.TypeTurnFinished, Data: data})

	if m.running {
		t.Error("running should be false after turn finish")
	}
	if m.liveText != "" || m.liveThink != "" || m.activeAgents != nil {
		t.Errorf("live state not cleared: %+v", m)
	}
	if m.councilRound != 0 || m.councilMember != "" {
		t.Errorf("council chip not cleared: round=%d member=%q", m.councilRound, m.councilMember)
	}
	if !m.panes[0].done || m.panes[0].doneAt.IsZero() {
		t.Error("lingering pane should be marked done with a fade clock")
	}
	if m.turnIn != 1234 || m.turnOut != 567 {
		t.Errorf("usage not frozen from event: in=%d out=%d", m.turnIn, m.turnOut)
	}
	if m.turnDur <= 0 {
		t.Errorf("turn meter should be frozen to a positive duration, got %v", m.turnDur)
	}
}

func TestOnTurnFinished_ZeroUsageKeepsPriorTotals(t *testing.T) {
	// A finish event with no usage (In/Out == 0) must not zero out the running
	// totals already accumulated from context-usage events during the turn.
	m := &Model{turnIn: 999, turnOut: 42, turnStart: time.Now()}
	data, _ := json.Marshal(event.TurnFinishedData{})
	m.onTurnFinished(event.Event{Type: event.TypeTurnFinished, Data: data})
	if m.turnIn != 999 || m.turnOut != 42 {
		t.Errorf("zero-usage finish clobbered totals: in=%d out=%d", m.turnIn, m.turnOut)
	}
}
