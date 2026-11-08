package git

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/port"
)

func TestStage1LegacyManagedRecordsKeepDigestAndLazyInference(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	// Legacy-shaped ExperienceChange without any Stage 1 fields.
	rec1, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID:      "legacy_obj_1",
		OperationID:   "op_legacy_1",
		ObservationID: "obs_legacy_1",
		Owner:         "engram",
		Kind:          "memory",
		State:         "active",
		HostOutcome:   "verified",
		Memory: &port.Memory{
			Text: "[디버깅] 포트 충돌 해결 — lsof 확인 → success: 잔존 프로세스 종료가 먼저다",
		},
	})
	if err != nil {
		t.Fatalf("ApplyExperience legacy: %v", err)
	}
	// Replaying the exact same legacy operation must be idempotent (digest unchanged).
	rec2, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID:      "legacy_obj_1",
		OperationID:   "op_legacy_1",
		ObservationID: "obs_legacy_1",
		Owner:         "engram",
		Kind:          "memory",
		State:         "active",
		HostOutcome:   "verified",
		Memory: &port.Memory{
			Text: "[디버깅] 포트 충돌 해결 — lsof 확인 → success: 잔존 프로세스 종료가 먼저다",
		},
	})
	if err != nil || rec2.Revision != rec1.Revision {
		t.Fatalf("expected idempotent replay revision %q, got %q (err=%v)", rec1.Revision, rec2.Revision, err)
	}

	summaries, err := store.SearchManagedExperience(ctx, "포트 충돌", "memory", 0, 10)
	if err != nil || len(summaries) != 1 || summaries[0].Memory == nil {
		t.Fatalf("SearchManagedExperience: got %+v, err=%v", summaries, err)
	}
	m := summaries[0].Memory
	if m.Kind != "lesson" || m.Tier != "reference" || m.Status != "observed" {
		t.Fatalf("unexpected lazy inference: kind=%q tier=%q status=%q", m.Kind, m.Tier, m.Status)
	}
	if len(m.Areas) == 0 || m.Areas[0] != "디버깅" {
		t.Fatalf("expected inferred area '디버깅', got %+v", m.Areas)
	}

	// Also verify reading existing repository managed JSON files if present.
	repoManaged := filepath.Join("..", "..", "..", "..", ".magi", "experience", "managed")
	repoStore := New(filepath.Dir(repoManaged))
	if list, err := repoStore.SearchManagedExperience(ctx, "", "memory", 0, 100); err != nil {
		t.Fatalf("failed reading existing .magi/experience/managed records: %v", err)
	} else {
		for _, item := range list {
			if item.ObjectID == "" || item.Memory == nil || item.Memory.Description == "" || item.Memory.Tier == "" || item.Memory.Status == "" {
				t.Fatalf("incomplete lazy defaults on existing record: %+v", item)
			}
		}
	}
}

func TestStage3ExactIDRecallExpirationAndCoreMemories(t *testing.T) {
	ctx := context.Background()
	store := New(t.TempDir())

	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	tomorrow := time.Now().UTC().AddDate(0, 0, 1).Format("2006-01-02")

	// 1. Active core observed memory.
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID:      "core_rule_01",
		OperationID:   "op1",
		ObservationID: "obs1",
		Owner:         "engram",
		Kind:          "memory",
		State:         "active",
		HostOutcome:   "verified",
		Memory: &port.Memory{
			Text:        "CGO 없이 순수 Go SQLite 드라이버만 사용한다",
			Description: "SQLite 드라이버 선택 규칙",
			Kind:        "preference",
			Areas:       []string{"구현", "설정"},
			Tier:        "core",
			Status:      "observed",
			ValidUntil:  tomorrow,
			Evidence:    []port.MemoryEvidence{{Session: "s1", Outcome: "verified"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// 2. Expired memory (valid_until < today) -> should be excluded from Retrieve and CoreMemories.
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID:      "expired_02",
		OperationID:   "op2",
		ObservationID: "obs2",
		Owner:         "engram",
		Kind:          "memory",
		State:         "active",
		HostOutcome:   "verified",
		Memory: &port.Memory{
			Text:        "임시 우회 플래그 사용",
			Description: "만료된 임시 우회",
			Tier:        "core",
			Status:      "observed",
			ValidUntil:  yesterday,
		},
	}); err != nil {
		t.Fatal(err)
	}

	// 3. Disconfirmed memory -> should be excluded from Retrieve and CoreMemories.
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID:      "disconfirmed_03",
		OperationID:   "op3",
		ObservationID: "obs3",
		Owner:         "engram",
		Kind:          "memory",
		State:         "active",
		HostOutcome:   "verified",
		Memory: &port.Memory{
			Text:        "pkill -f server 로 프로세스를 종료한다",
			Description: "반증된 종료 방법",
			Tier:        "core",
			Status:      "disconfirmed",
		},
	}); err != nil {
		t.Fatal(err)
	}

	cores, err := store.CoreMemories(ctx, 2400)
	if err != nil {
		t.Fatal(err)
	}
	if len(cores) != 1 || !strings.HasPrefix(cores[0].ID, "managed:core_rule_01") {
		t.Fatalf("expected only core_rule_01 in CoreMemories, got %+v", cores)
	}

	// Exact ID recall via "managed:core_rule_01" even when query words don't match body tokens.
	mems, _, err := store.Retrieve(ctx, "managed:core_rule_01", nil)
	if err != nil || len(mems) != 1 || !strings.HasPrefix(mems[0].ID, "managed:core_rule_01") {
		t.Fatalf("expected exact ObjectID recall for managed:core_rule_01, got %+v (err=%v)", mems, err)
	}
}
