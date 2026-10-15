package app

import (
	"encoding/json"
	"github.com/sayaya1090/magi/internal/core/event"
	"strings"
	"testing"
)

func TestKoreanCompactedContextMatchingAndHints(t *testing.T) {
	shards := []event.ContextShard{
		{Topic: "settings.kt", Brief: "설정창 너비와 드롭다운 폭 수정"},
		{Topic: "notes.go", Brief: "슬라이드 발표자 노트 읽기"},
	}
	idx, ambiguous := matchShard("설정창의 너비", shards)
	if idx != 0 || ambiguous {
		t.Fatalf("Korean shard not matched: %d %v", idx, ambiguous)
	}
	data, err := json.Marshal(event.CompactionData{Shards: shards})
	if err != nil {
		t.Fatal(err)
	}
	hints := shardHints([]event.Event{{Type: event.TypeCompaction, Data: data}}, "설정창의 너비")
	if !strings.Contains(hints, "settings.kt") || strings.Contains(hints, "notes.go") {
		t.Fatalf("Korean hints: %q", hints)
	}
	if got := shardHints([]event.Event{{Type: event.TypeCompaction, Data: data}}, "양자컴퓨터"); got != "" {
		t.Fatalf("unrelated hints: %q", got)
	}
}
