package app

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/model"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/session"
)

// estimateTokens is the ≈4-chars/token fallback the compaction trigger and the context meter use when
// the provider's real prompt_tokens are unavailable. It must count system + every part's text, tool
// call name+args, and tool result content.
func TestEstimateTokens(t *testing.T) {
	if got := estimateTokens("", nil); got != 0 {
		t.Errorf("empty = %d, want 0", got)
	}
	// 8 sys + (4 text + (2 name + 6 args) + 4 result) = 8 + 16 = 24 chars → 24/4 = 6.
	msgs := []session.Message{{Parts: []session.Part{
		{Kind: session.PartText, Text: "abcd"},
		{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "ab", Args: json.RawMessage("012345")}},
		{Kind: session.PartToolResult, ToolResult: &session.ToolResult{Content: json.RawMessage("wxyz")}},
	}}}
	if got := estimateTokens("SYSTEM12", msgs); got != 6 {
		t.Errorf("estimateTokens = %d, want 6", got)
	}
	// Reasoning is persisted but never sent on the wire (joinText emits only PartText), so it must
	// not be counted — counting it folded thinking-model sessions away at a fraction of real use.
	withReasoning := []session.Message{{Parts: []session.Part{
		{Kind: session.PartText, Text: "abcd"},
		{Kind: session.PartReasoning, Text: "this is a long private deliberation the model never sees again"},
	}}}
	if got := estimateTokens("SYSTEM12", withReasoning); got != estimateTokens("SYSTEM12", []session.Message{{Parts: []session.Part{{Kind: session.PartText, Text: "abcd"}}}}) {
		t.Errorf("estimateTokens counted a reasoning part that the wire drops: %d", got)
	}
}

// flattenForSummary renders tool structure as prose so the summarizer request carries no tool_use
// blocks — a strict backend rejects those when the request declares no tools, which silently broke
// every auto-fold on such a route.
func TestFlattenForSummary(t *testing.T) {
	msgs := []session.Message{
		{Role: session.RoleAssistant, Parts: []session.Part{
			{Kind: session.PartText, Text: "let me look"},
			{Kind: session.PartToolCall, ToolCall: &session.ToolCall{Name: "read", Args: json.RawMessage(`{"path":"a.go"}`)}},
		}},
		{Role: session.RoleTool, Parts: []session.Part{
			{Kind: session.PartToolResult, ToolResult: &session.ToolResult{Content: json.RawMessage(`"package main"`)}},
		}},
		{Role: session.RoleAssistant, Parts: []session.Part{{Kind: session.PartReasoning, Text: "private"}}},
	}
	out := flattenForSummary(msgs)
	for _, m := range out {
		for _, p := range m.Parts {
			if p.Kind != session.PartText {
				t.Errorf("a non-text part survived flattening: %q", p.Kind)
			}
			if p.ToolCall != nil || p.ToolResult != nil {
				t.Error("tool structure survived flattening")
			}
		}
		if m.Role != session.RoleAssistant && m.Role != session.RoleUser {
			t.Errorf("an illegal role reached the summarizer: %q", m.Role)
		}
	}
	// The tool result's message was role "tool"; with nothing to answer it must ride in as user.
	if len(out) < 2 || out[1].Role != session.RoleUser {
		t.Errorf("the tool-result message was not demoted to user: %+v", out)
	}
	// A reasoning-only message has no wire content, so it drops out entirely.
	joined := ""
	for _, m := range out {
		for _, p := range m.Parts {
			joined += p.Text + "\n"
		}
	}
	if strings.Contains(joined, "private") {
		t.Error("reasoning leaked into the summary request")
	}
	if !strings.Contains(joined, "read") || !strings.Contains(joined, "package main") {
		t.Errorf("the tool call and result did not survive as prose:\n%s", joined)
	}
}

// maybeCompact does not fold when the messages already fit the budget — the overage is then the
// system prompt, which no fold can shrink, and folding every step was measured spinning uselessly.
func TestMaybeCompactSkipsWhenMessagesAlreadyFit(t *testing.T) {
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "tiny", ContextWindow: 400, Tools: true}) // budget 400*0.8 = 320 tokens
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &usageLLM{text: "ok"}, builtin.Default(), bus.New(), nil, Config{
		Permission: "allow", Models: reg, CompactRatio: 0.8,
	})
	sid, _ := a.CreateSession(context.Background(), command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "tiny"}})
	// Ten small fact events — enough to be foldable (> keepRecentEvents+1) — reconstructed into
	// small messages that fit the budget, plus a huge system prompt that does not. The overage is
	// entirely the prompt; folding the messages cannot help, so no compaction event may appear.
	for i := 0; i < 10; i++ {
		d, _ := json.Marshal(event.PartAppendedData{MessageID: fmt.Sprintf("m%d", i), Role: session.RoleAssistant, Part: session.Part{Kind: session.PartText, Text: "short"}})
		a.appendFact(context.Background(), sid, event.TypePartAppended, event.Actor{}, d)
	}
	evs, _ := store.Read(context.Background(), sid, 0)
	msgs := reconstruct(evs)
	hugeSys := strings.Repeat("x", 4000) // ~1000 tokens, over the 320 budget on its own
	sess := a.sessionInfo(context.Background(), sid)
	if a.maybeCompact(context.Background(), sess, a.agentFor(sess), event.Actor{}, evs, msgs, hugeSys) {
		t.Error("folded when the messages already fit — the overage was the system prompt, which a fold cannot shrink")
	}
}

// truncateAt keeps exactly the events at or before the boundary seq (the compaction split point).
func TestTruncateAt(t *testing.T) {
	evs := []event.Event{{Seq: 1}, {Seq: 2}, {Seq: 3}, {Seq: 4}}
	got := truncateAt(evs, 2)
	if len(got) != 2 || got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("truncateAt(_, 2) = %+v, want seqs [1 2]", got)
	}
	// Boundary at/after the last seq keeps everything; before the first keeps nothing.
	if len(truncateAt(evs, 4)) != 4 {
		t.Error("boundary at last seq should keep all")
	}
	if len(truncateAt(evs, 0)) != 0 {
		t.Error("boundary before first seq should keep none")
	}
}

// shardPath extracts the file a tool call targeted (workdir-relative) so a compacted message lands
// on the right recall topic. It reads "path" (most file tools) or falls back to "file" (astgrep/LSP
// nav), returns "" for a call that names no file (bash/web) so it lands in "discussion", and never
// panics on malformed args (best-effort unmarshal).
func TestShardPath(t *testing.T) {
	wd := "/w"
	cases := []struct {
		name, args, want string
	}{
		{"path field, workdir-relative", `{"path":"/w/sub/f.go"}`, "sub/f.go"},
		{"relative path joined to workdir", `{"path":"sub/g.go"}`, "sub/g.go"},
		{"file fallback when no path (astgrep/LSP)", `{"file":"/w/x.go"}`, "x.go"},
		{"path wins over file", `{"path":"/w/a.go","file":"/w/b.go"}`, "a.go"},
		{"no file → empty (bash/web)", `{"command":"ls -la"}`, ""},
		{"empty object → empty", `{}`, ""},
		{"malformed JSON → empty (best-effort, no panic)", `not json`, ""},
	}
	for _, c := range cases {
		if got := shardPath(wd, json.RawMessage(c.args)); got != c.want {
			t.Errorf("%s: shardPath(%q) = %q, want %q", c.name, c.args, got, c.want)
		}
	}
}
