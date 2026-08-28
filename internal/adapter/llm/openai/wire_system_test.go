package openai

import (
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
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

// A prompt magi wrote reaches the model marked as one.
//
// The whole path, because the attribution is assembled by two pieces that could each be right
// alone and wrong together: internal/app gives magi's own prompts the system role, and this
// package demotes any mid-conversation system message to a user one prefixed "[system note]" — so
// what the model actually receives is a user turn that says it is not the person. Asserting only
// the role in app would leave the sentence the model reads untested.
func TestAPromptMagiWroteArrivesMarkedAsNotThePerson(t *testing.T) {
	req := buildRequest(port.ChatRequest{
		System: "you are magi",
		Messages: []session.Message{
			{Role: session.RoleUser, Parts: []session.Part{{Kind: session.PartText, Text: "rename a.txt"}}},
			{Role: session.RoleAssistant, Parts: []session.Part{{Kind: session.PartText, Text: "done"}}},
			{Role: session.RoleSystem, Parts: []session.Part{{Kind: session.PartText,
				Text: "You stopped without saying you are finished."}}},
		},
	}, false, false, "", 0, Sampling{}, nil)

	last := req.Messages[len(req.Messages)-1]
	// A user turn, so it works on every backend — including the ones that reject a system message
	// anywhere but the head, which is why it is demoted rather than sent as one.
	if last.Role != "user" {
		t.Fatalf("magi's own prompt goes out as a %q message", last.Role)
	}
	body, _ := last.Content.(string)
	if !strings.HasPrefix(body, "[system note]") {
		t.Errorf("what the model reads does not say who wrote it: %q", body)
	}
	if !strings.Contains(body, "You stopped without saying") {
		t.Errorf("the marker replaced the message instead of introducing it: %q", body)
	}
	// The person's own turn is untouched: marking everything would be the same failure the other
	// way round.
	first := req.Messages[1]
	if first.Role != "user" || strings.Contains(first.Content.(string), "[system note]") {
		t.Errorf("the person's own prompt was marked as magi's: %+v", first)
	}
}
