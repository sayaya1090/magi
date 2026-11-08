package app

import (
	"context"
	"strings"
	"testing"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/port"
)

func TestExperienceExcerptsListsOnlyMemoriesWithAPassage(t *testing.T) {
	if got := experienceExcerpts([]port.Memory{{ID: "a", Text: "t"}}); got != "" {
		t.Fatalf("no excerpt should render nothing, got %q", got)
	}
	got := experienceExcerpts([]port.Memory{
		{ID: "[project] a", Text: "long", Excerpt: "line one\n  line two"},
		{ID: "[project] b", Text: "short"},
	})
	want := "Matching passages (recall_memory returns the full text):\n- [project] a: line one line two"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestCoreBlockFreezesPerSessionAndExcerptSentDeduplicates(t *testing.T) {
	ctx := context.Background()
	store := expgit.New(t.TempDir())
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: "core1", OperationID: "op1", ObservationID: "obs1", Owner: "engram",
		Kind: "memory", State: "active", HostOutcome: "verified",
		Memory: &port.Memory{
			Text:        "CGO 없이 순수 Go SQLite 드라이버만 사용한다",
			Description: "SQLite 드라이버 규칙",
			Kind:        "preference",
			Tier:        "core",
			Status:      "observed",
		},
	}); err != nil {
		t.Fatal(err)
	}

	a := &App{cfg: Config{Experience: store}}
	firstCore := a.coreBlockFor("sess-freeze")
	if !strings.Contains(firstCore, "# Core memory (pinned for this session)") || !strings.Contains(firstCore, "SQLite 드라이버 규칙") {
		t.Fatalf("expected Core memory block, got %q", firstCore)
	}

	// Mutating the store mid-session must NOT alter the frozen coreBlockFor("sess-freeze").
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: "core2", OperationID: "op2", ObservationID: "obs2", Owner: "engram",
		Kind: "memory", State: "active", HostOutcome: "verified",
		Memory: &port.Memory{
			Text:        "세션 중간에 추가된 코어 규칙",
			Description: "중간 추가 규칙",
			Kind:        "lesson",
			Tier:        "core",
			Status:      "observed",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if secondCore := a.coreBlockFor("sess-freeze"); secondCore != firstCore {
		t.Fatalf("expected session-frozen coreBlock to remain identical, got:\n%s\nvs:\n%s", secondCore, firstCore)
	}
	// Session compaction (resetMemoryDelivery) is the sanctioned mid-session boundary
	// to refresh the pinned Core memory block after recent context is summarized away.
	a.resetMemoryDelivery("sess-freeze")
	if postCompactCore := a.coreBlockFor("sess-freeze"); !strings.Contains(postCompactCore, "중간 추가 규칙") {
		t.Fatalf("expected coreBlockFor to refresh after session compaction, got %q", postCompactCore)
	}
	// A new session sees the newly added core rule, and revision hashes (@...) are stripped
	// so non-text metadata updates (like evidence) never alter the system prompt prefix.
	newSessCore := a.coreBlockFor("sess-new")
	if !strings.Contains(newSessCore, "중간 추가 규칙") {
		t.Fatalf("expected new session to see updated core rule, got %q", newSessCore)
	}
	if strings.Contains(newSessCore, "@") {
		t.Fatalf("expected coreBlockFor to strip @revision suffix for KV cache stability, got %q", newSessCore)
	}
}
