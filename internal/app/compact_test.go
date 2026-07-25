package app

import (
	"encoding/json"
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
