package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sayaya1090/magi/internal/adapter/store/jsonl"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/bus"
	"github.com/sayaya1090/magi/internal/core/command"
	"github.com/sayaya1090/magi/internal/core/event"
	"github.com/sayaya1090/magi/internal/core/model"
	"github.com/sayaya1090/magi/internal/core/session"
)

func textPart(s string) session.Part { return session.Part{Kind: session.PartText, Text: s} }
func callPart(callID, name, path string) session.Part {
	return session.Part{Kind: session.PartToolCall, ToolCall: &session.ToolCall{
		CallID: callID, Name: name, Args: json.RawMessage(`{"path":"` + path + `"}`),
	}}
}
func resultPart(callID, content string) session.Part {
	c, _ := json.Marshal(content)
	return session.Part{Kind: session.PartToolResult, ToolResult: &session.ToolResult{CallID: callID, Content: c}}
}

// shardByPath buckets messages by the file each touched, attributes a tool result to its
// call's file, drops prior summaries, and puts file-less messages in "discussion".
func TestShardByPath(t *testing.T) {
	msgs := []session.Message{
		{ID: "u1", Role: session.RoleUser, Parts: []session.Part{textPart("let's work")}},
		{ID: "a1", Role: session.RoleAssistant, Parts: []session.Part{callPart("c1", "read", "foo.go")}},
		{ID: "t1", Role: session.RoleTool, Parts: []session.Part{resultPart("c1", "foo body")}},
		{ID: "a2", Role: session.RoleAssistant, Parts: []session.Part{callPart("c2", "edit", "bar.go")}},
		{ID: "t2", Role: session.RoleTool, Parts: []session.Part{resultPart("c2", "edited")}},
		{ID: "compaction-9", Role: session.RoleSystem, Parts: []session.Part{textPart("old summary")}},
	}
	shards := shardByPath(msgs, "/work")
	want := map[string][]string{
		"foo.go":     {"a1", "t1"},
		"bar.go":     {"a2", "t2"},
		"discussion": {"u1"},
	}
	if len(shards) != len(want) {
		t.Fatalf("got %d shards, want %d: %+v", len(shards), len(want), shards)
	}
	briefs := map[string]string{"foo.go": "read", "bar.go": "edit"}
	for _, sh := range shards {
		w, ok := want[sh.Topic]
		if !ok {
			t.Errorf("unexpected shard topic %q", sh.Topic)
			continue
		}
		if strings.Join(sh.MessageIDs, ",") != strings.Join(w, ",") {
			t.Errorf("shard %q ids = %v, want %v", sh.Topic, sh.MessageIDs, w)
		}
		if exp, ok := briefs[sh.Topic]; ok && sh.Brief != exp {
			t.Errorf("shard %q brief = %q, want %q (action trail)", sh.Topic, sh.Brief, exp)
		}
	}
	// The prior summary (compaction-9) must not be sharded.
	for _, sh := range shards {
		for _, id := range sh.MessageIDs {
			if id == "compaction-9" {
				t.Error("compaction- message must be excluded from shards")
			}
		}
	}
}

func TestMatchShard(t *testing.T) {
	shards := []event.ContextShard{
		{Topic: "internal/app/loop.go"},
		{Topic: "internal/app/compact.go"},
		{Topic: "discussion", Brief: "messages not tied to a specific file"},
	}
	cases := []struct {
		q       string
		wantIdx int
		ambig   bool
	}{
		{"internal/app/loop.go", 0, false}, // exact
		{"loop.go", 0, false},              // basename
		{"compact", 1, false},              // substring
		{"discussion", 2, false},           // exact
		{"nonexistent-xyz", -1, false},     // no match
	}
	for _, c := range cases {
		idx, amb := matchShard(c.q, shards)
		if idx != c.wantIdx || amb != c.ambig {
			t.Errorf("matchShard(%q) = (%d,%v), want (%d,%v)", c.q, idx, amb, c.wantIdx, c.ambig)
		}
	}
}

// rebuildMessages recovers messages by ID from the raw log, ignoring compaction.
func TestRebuildMessages(t *testing.T) {
	mk := func(seq int64, typ event.Type, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Seq: seq, Type: typ, Data: b}
	}
	evs := []event.Event{
		mk(1, event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "u1", Parts: []session.Part{textPart("hi")}}),
		mk(2, event.TypePartAppended, event.PartAppendedData{MessageID: "a1", Role: session.RoleAssistant, Part: callPart("c1", "read", "foo.go")}),
		mk(3, event.TypePartAppended, event.PartAppendedData{MessageID: "a1", Role: session.RoleAssistant, Part: textPart("done")}),
		mk(4, event.TypeCompaction, event.CompactionData{Summary: "S", ReplacesUpToSeq: 3}),
	}
	got := rebuildMessages(evs, []string{"a1", "u1"})
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2: %+v", len(got), got)
	}
	// Order preserved (u1 before a1); a1's two parts merged.
	if got[0].ID != "u1" || got[1].ID != "a1" {
		t.Errorf("order/ids wrong: %+v", got)
	}
	if len(got[1].Parts) != 2 {
		t.Errorf("a1 should have 2 merged parts, got %d", len(got[1].Parts))
	}
}

// reconstruct advertises recall topics, aggregated across multiple compactions.
func TestReconstructRecallHintAggregates(t *testing.T) {
	mk := func(seq int64, typ event.Type, data any) event.Event {
		b, _ := json.Marshal(data)
		return event.Event{Seq: seq, Type: typ, Data: b}
	}
	evs := []event.Event{
		mk(1, event.TypePartAppended, event.PartAppendedData{MessageID: "a1", Role: session.RoleAssistant, Part: textPart("x")}),
		mk(2, event.TypeCompaction, event.CompactionData{Summary: "S1", ReplacesUpToSeq: 1,
			Shards: []event.ContextShard{{Topic: "foo.go", MessageIDs: []string{"a1"}}}}),
		mk(3, event.TypePartAppended, event.PartAppendedData{MessageID: "a2", Role: session.RoleAssistant, Part: textPart("y")}),
		mk(4, event.TypeCompaction, event.CompactionData{Summary: "S2", ReplacesUpToSeq: 3,
			Shards: []event.ContextShard{{Topic: "bar.go", MessageIDs: []string{"a2"}}}}),
	}
	msgs := reconstruct(evs)
	if len(msgs) == 0 || msgs[0].Role != session.RoleSystem {
		t.Fatalf("expected a leading system summary, got %+v", msgs)
	}
	text := msgs[0].Parts[0].Text
	if !strings.Contains(text, "S2") {
		t.Errorf("latest summary text missing: %q", text)
	}
	// Both the latest topic AND the earlier compaction's topic must be advertised.
	if !strings.Contains(text, "foo.go") || !strings.Contains(text, "bar.go") {
		t.Errorf("recall hint should aggregate topics across compactions, got: %q", text)
	}
	if !strings.Contains(text, "recall_context") {
		t.Errorf("hint should name the recall_context tool, got: %q", text)
	}
}

func TestRecallBudget(t *testing.T) {
	g := newRunGuard()
	if ok, _ := g.allowRecall("foo.go"); !ok {
		t.Fatal("first recall of a topic should be allowed")
	}
	if ok, _ := g.allowRecall("foo.go"); ok {
		t.Error("same topic should not be recallable twice in a turn")
	}
	// Distinct topics until the budget is hit.
	allowed := 1 // already spent one above
	for i := 0; i < recallBudget+2; i++ {
		if ok, _ := g.allowRecall(string(rune('a' + i))); ok {
			allowed++
		}
	}
	if allowed != recallBudget {
		t.Errorf("allowed %d recalls, want budget %d", allowed, recallBudget)
	}
}

// actionTrail renders a path's tool activity deterministically, with ×N for repeats.
func TestActionTrail(t *testing.T) {
	if got := actionTrail([]string{"read", "edit", "edit", "bash"}); got != "read · edit×2 · bash" {
		t.Errorf("actionTrail = %q, want %q", got, "read · edit×2 · bash")
	}
	if got := actionTrail(nil); got != "" {
		t.Errorf("empty trail = %q, want empty", got)
	}
}

// End-to-end: recall pulls the original tool detail back verbatim from a compacted session.
func TestRecallContextIntegration(t *testing.T) {
	reg := model.NewRegistry()
	reg.Register(model.Info{ID: "m", ContextWindow: 100000, Tools: true})
	store, _ := jsonl.New(t.TempDir())
	a := New(store, &usageLLM{}, builtin.Default(), bus.New(), nil, Config{Permission: "allow", Models: reg})
	ctx := context.Background()
	sid, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "m"}})

	seed := func(typ event.Type, data any) {
		b, _ := json.Marshal(data)
		if err := a.appendFact(ctx, sid, typ, event.Actor{}, b); err != nil {
			t.Fatal(err)
		}
	}
	seed(event.TypePromptSubmitted, event.PromptSubmittedData{MessageID: "u1", Parts: []session.Part{textPart("let's discuss")}})
	seed(event.TypePartAppended, event.PartAppendedData{MessageID: "a1", Role: session.RoleAssistant, Part: callPart("c1", "read", "foo.go")})
	seed(event.TypePartAppended, event.PartAppendedData{MessageID: "t1", Role: session.RoleTool, Part: resultPart("c1", "FOO FILE BODY")})
	seed(event.TypeCompaction, event.CompactionData{Summary: "S", ReplacesUpToSeq: 0,
		Shards: []event.ContextShard{
			{Topic: "foo.go", MessageIDs: []string{"a1", "t1"}},
			{Topic: "discussion", MessageIDs: []string{"u1"}},
		}})

	// Hit: the original tool detail comes back verbatim.
	out, err := a.recallContext(ctx, sid, "foo.go", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "FOO FILE BODY") || !strings.Contains(out, "read") {
		t.Errorf("recall did not return the original detail: %q", out)
	}

	// Miss: lists the available topics instead of guessing.
	miss, _ := a.recallContext(ctx, sid, "totally-unrelated", nil)
	if !strings.Contains(miss, "foo.go") || !strings.Contains(miss, "discussion") {
		t.Errorf("miss should list available topics, got: %q", miss)
	}

	// Fresh session with no compaction → nothing to recall.
	sid2, _ := a.CreateSession(ctx, command.CreateSession{Workdir: t.TempDir(), Model: session.ModelRef{Provider: "openai", Model: "m"}})
	none, _ := a.recallContext(ctx, sid2, "anything", nil)
	if !strings.Contains(none, "Nothing has been compacted") {
		t.Errorf("empty session recall message wrong: %q", none)
	}
}

// shardHints: relevant compacted topics surface as pointers; unrelated queries
// stay silent; the hint count is capped.
func TestShardHints(t *testing.T) {
	comp := func(shs ...event.ContextShard) event.Event {
		d, _ := json.Marshal(event.CompactionData{Shards: shs})
		return event.Event{Type: event.TypeCompaction, Data: d}
	}
	evs := []event.Event{comp(
		event.ContextShard{Topic: "internal/parser/lexer.go", Brief: "edited twice; EOF handling fixed"},
		event.ContextShard{Topic: "discussion", Brief: "user chose the redis cache approach"},
		event.ContextShard{Topic: "docs/README.md", Brief: "read only"},
	)}

	// Two query tokens hit the lexer shard → hint with the recall pointer.
	out := shardHints(evs, "fix the parser lexer EOF bug")
	if !strings.Contains(out, "internal/parser/lexer.go") || !strings.Contains(out, "recall_context") {
		t.Fatalf("relevant shard should surface: %q", out)
	}
	if strings.Contains(out, "README") {
		t.Fatalf("unrelated shard leaked: %q", out)
	}

	// Nothing relates → no section at all (volatile context stays lean).
	if out := shardHints(evs, "rotate the kubernetes certificates"); out != "" {
		t.Fatalf("unrelated query should add nothing, got %q", out)
	}

	// No compactions → nothing, regardless of query.
	if out := shardHints(nil, "fix the lexer"); out != "" {
		t.Fatalf("no compaction should mean no hints, got %q", out)
	}

	// Cap: more matches than shardHintMax → top-scored three only.
	var many []event.ContextShard
	for i := 0; i < 6; i++ {
		many = append(many, event.ContextShard{Topic: fmt.Sprintf("pkg/lexer/file%d.go", i), Brief: "lexer parser work"})
	}
	out = shardHints([]event.Event{comp(many...)}, "lexer parser")
	if n := strings.Count(out, "\n- "); n > shardHintMax {
		t.Fatalf("hints should cap at %d, got %d:\n%s", shardHintMax, n, out)
	}
}

// BM25-lite ranking: a rare token that pins one shard must outrank a generic token
// shared by many shards, even when the generic token also matches the query.
func TestShardHintsIDFRanking(t *testing.T) {
	comp := func(shs ...event.ContextShard) event.Event {
		d, _ := json.Marshal(event.CompactionData{Shards: shs})
		return event.Event{Type: event.TypeCompaction, Data: d}
	}
	// "handler" is generic (in every shard); "dehydration" is rare (one shard).
	// A query mentioning both must rank the dehydration shard first.
	shards := []event.ContextShard{
		{Topic: "a.go", Brief: "handler wiring code"},
		{Topic: "b.go", Brief: "handler routing code"},
		{Topic: "c.go", Brief: "handler and context dehydration logic"},
		{Topic: "d.go", Brief: "handler middleware"},
	}
	out := shardHints([]event.Event{comp(shards...)}, "the handler dehydration path")
	// c.go (rare-token match) must appear, and appear before any generic-only shard.
	ci := strings.Index(out, "c.go")
	if ci < 0 {
		t.Fatalf("rare-token shard c.go should surface: %q", out)
	}
	for _, generic := range []string{"a.go", "b.go", "d.go"} {
		if gi := strings.Index(out, generic); gi >= 0 && gi < ci {
			t.Fatalf("generic-only shard %s ranked above rare-token c.go:\n%s", generic, out)
		}
	}
}
