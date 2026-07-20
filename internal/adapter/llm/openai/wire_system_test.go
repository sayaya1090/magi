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

// The cache path makes the head system a []textBlock (cache_control), so a following
// system (the reconstruct compaction summary, always prepended as a system message) cannot
// string-merge into it. It MUST still be demoted, leaving exactly one system at index 0 —
// the exact shape a post-compaction request takes when caching is on. This is the scenario
// behind the recurring "system message must be at the beginning" 400.
func TestNormalizeSystemPlacementCachedHead(t *testing.T) {
	msgs := []wireMessage{
		{Role: "system", Content: []textBlock{{Type: "text", Text: "main", CacheControl: ephemeral()}}},
		{Role: "system", Content: "compaction summary"}, // cannot merge into a textBlock head
		{Role: "user", Content: "hi"},
	}
	out := normalizeSystemPlacement(msgs)
	sys, sysIdx := 0, -1
	for i, m := range out {
		if m.Role == "system" {
			sys++
			sysIdx = i
		}
	}
	if sys != 1 || sysIdx != 0 {
		t.Fatalf("cached-head compaction: want exactly 1 system at idx 0, got count=%d idx=%d (roles would 400)", sys, sysIdx)
	}
	// The summary survives, demoted to a user note (not silently dropped).
	found := false
	for _, m := range out {
		if s, ok := m.Content.(string); ok && strings.Contains(s, "compaction summary") {
			found = true
		}
	}
	if !found {
		t.Fatal("compaction summary lost during demotion")
	}
}

// wireRoleDiag reports the system count and indices (roles only, never content) so a 400
// in a run log shows the exact message shape sent.
func TestWireRoleDiag(t *testing.T) {
	d := wireRoleDiag([]wireMessage{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"},
		{Role: "system", Content: "late"},
	})
	for _, want := range []string{"3 msgs", "systemCount=2", "[0 2]", "system,user,system"} {
		if !strings.Contains(d, want) {
			t.Errorf("wireRoleDiag missing %q in %q", want, d)
		}
	}
}
