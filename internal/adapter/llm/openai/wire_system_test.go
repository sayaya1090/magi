package openai

import (
	"strings"
	"testing"
)

// Strict chat templates 400 on a second or mid-conversation system message; the
// wire layer must emit at most one system message, at position 0.
func TestNormalizeSystemPlacement(t *testing.T) {
	msgs := []wireMessage{
		{Role: "system", Content: "main"},
		{Role: "system", Content: "compaction summary"}, // leading run → merged
		{Role: "user", Content: "hi"},
		{Role: "system", Content: "late note"}, // mid-conversation → demoted
		{Role: "assistant", Content: "ok"},
	}
	out := normalizeSystemPlacement(msgs)
	if out[0].Role != "system" {
		t.Fatalf("head must stay system, got %s", out[0].Role)
	}
	if s, _ := out[0].Content.(string); !strings.Contains(s, "main") || !strings.Contains(s, "compaction summary") {
		t.Fatalf("leading system messages not merged: %v", out[0].Content)
	}
	sys := 0
	for i, m := range out {
		if m.Role == "system" {
			sys++
			if i != 0 {
				t.Fatalf("system message at index %d", i)
			}
		}
	}
	if sys != 1 {
		t.Fatalf("want exactly 1 system message, got %d", sys)
	}
	// The demoted note keeps its content as a prefixed user message.
	found := false
	for _, m := range out {
		if m.Role == "user" {
			if s, ok := m.Content.(string); ok && strings.Contains(s, "[system note]") && strings.Contains(s, "late note") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("mid-conversation system message was not demoted to a prefixed user message")
	}
	// No system at all → untouched shape.
	plain := normalizeSystemPlacement([]wireMessage{{Role: "user", Content: "x"}})
	if len(plain) != 1 || plain[0].Role != "user" {
		t.Fatalf("no-system case altered: %+v", plain)
	}
}
