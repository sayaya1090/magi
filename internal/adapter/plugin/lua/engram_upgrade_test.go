package lua

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	expgit "github.com/sayaya1090/magi/internal/adapter/experience/git"
	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

type countingAnalyzer struct {
	calls   atomic.Int32
	replies []string
}

func (a *countingAnalyzer) Analyze(_ context.Context, _, _ string, _ string) (string, error) {
	idx := int(a.calls.Add(1)) - 1
	if idx < len(a.replies) {
		return a.replies[idx], nil
	}
	if len(a.replies) > 0 {
		return a.replies[len(a.replies)-1], nil
	}
	return `{"lesson":null,"skill":null}`, nil
}

func TestStage3EngramLessonsSuppressedWhenEmbeddingConnected(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	wd := t.TempDir()
	seed := "# 작업 이력 및 교훈 기록 (팀 공유)\n\n" +
		"| 일시 | 사용자 | 분류 | 작업 | 시도한 접근 | 결과 | 교훈 |\n" +
		"| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n" +
		"| 2026-08-01T09:00:00Z | user | 디버깅 | 포트 충돌 | lsof 확인 | ✅ 성공 | 포트 충돌은 lsof로 확인한다 |\n"
	if err := os.WriteFile(filepath.Join(wd, "SESSION_SUMMARY.md"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"bge-m3:latest"}]}`))
	}))
	defer srv.Close()

	reg := &fakeContextReg{}
	h := NewHostWithConfig(HostConfig{
		ToolSink:      builtin.NewRegistry(),
		ContextReg:    reg,
		EmbedReg:      &stubEmbedReg{},
		DataDir:       t.TempDir(),
		Runtime:       RuntimeInfo{Workdir: wd},
		PluginConfigs: map[string]map[string]any{"engram": {"embed_url": srv.URL + "/v1"}},
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	if len(reg.providers) != 1 {
		t.Fatalf("expected 1 context provider, got %d", len(reg.providers))
	}

	// Before startup connects embedding, lexical fallback provides the table.
	items, err := reg.providers[0].Provide(context.Background(), port.ContextQuery{Prompt: "포트 충돌 확인 방법"})
	if err != nil || len(items) != 1 {
		t.Fatalf("expected lexical fallback before embedding connect, got %+v (err=%v)", items, err)
	}

	// Once startup connects the embedding server, engram-lessons suppresses duplicate table injection.
	h.FireEvent("startup")
	items, err = reg.providers[0].Provide(context.Background(), port.ContextQuery{Prompt: "포트 충돌 확인 방법"})
	if err != nil || len(items) != 0 {
		t.Fatalf("expected engram-lessons table suppressed when embedding is connected, got %+v (err=%v)", items, err)
	}
}

func TestStage4FactsBatchingAndSearchExperienceEvidencePromotion(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	wd := t.TempDir()
	store := expgit.New(t.TempDir())
	ca := &countingAnalyzer{
		replies: []string{
			`{"lesson":{"task":"포트 충돌 점검","approach":"lsof 조회","outcome":"fail","lesson":"포트 충돌은 lsof 조회로 먼저 확인한다","category":"디버깅","description":"포트 충돌 lsof 점검","kind":"pitfall","tier":"reference","status":"hypothesis"},"skill":null}`,
			`{"lesson":{"task":"포트 충돌 재점검","approach":"lsof 재조회","outcome":"success","lesson":"포트 충돌은 lsof 조회로 먼저 확인한다","category":"디버깅"},"skill":null}`,
		},
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   ca,
		Experience: store,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: wd},
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })

	// 3 unverified turns: queued in inbox, 0 LLM calls so far.
	for i := 1; i <= 3; i++ {
		h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "포트 충돌 점검 시도"})
		h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "점검 진행 중입니다", "outcome": "unverified"})
		h.DrainEvents(5 * time.Second)
	}
	if got := ca.calls.Load(); got != 0 {
		t.Fatalf("expected 0 analyzer calls after 3 unverified turns, got %d", got)
	}

	// 4th unverified turn flushes the 4-turn batch in a single analyzer call.
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "네 번째 포트 충돌 점검"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "lsof 조회 필요", "outcome": "unverified"})
	h.DrainEvents(5 * time.Second)
	if got := ca.calls.Load(); got != 1 {
		t.Fatalf("expected 1 analyzer call on 4th unverified turn, got %d", got)
	}

	recs, err := store.SearchManagedExperience(context.Background(), "포트 충돌", "memory", 0, 10)
	if err != nil || len(recs) != 1 || recs[0].Memory == nil {
		t.Fatalf("expected 1 managed memory after batch flush, got %+v (err=%v)", recs, err)
	}
	if recs[0].Memory.Status != "hypothesis" {
		t.Fatalf("expected initial status 'hypothesis', got %q", recs[0].Memory.Status)
	}

	// Second session observes the same lesson on a verified turn -> appends evidence & promotes to 'observed' without creating a duplicate object.
	h.FireEventWith("user_message", map[string]string{"session": "s2", "text": "포트 충돌 해결 확인"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s2", "text": "lsof 조회로 해결 완료", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)

	recs, err = store.SearchManagedExperience(context.Background(), "포트 충돌", "memory", 0, 10)
	if err != nil || len(recs) != 1 || recs[0].Memory == nil {
		t.Fatalf("expected deduplicated single managed memory, got %d (%+v, err=%v)", len(recs), recs, err)
	}
	if len(recs[0].Memory.Evidence) == 0 {
		t.Fatalf("expected evidence appended on existing managed memory, got %+v", recs[0].Memory)
	}
}

func TestStage5ContextCompactedTriggersReflectionAndCircuitBreaker(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	wd := t.TempDir()
	store := expgit.New(t.TempDir())
	rec, err := store.ApplyExperience(context.Background(), port.ExperienceChange{
		ObjectID:      "mem_reflect_1",
		OperationID:   "op_init_1",
		ObservationID: "obs_init_1",
		Owner:         "plugin:engram",
		Kind:          "memory",
		State:         "active",
		HostOutcome:   "verified",
		Memory: &port.Memory{
			Text:        "SQLite 드라이버는 순수 Go 구현을 쓴다",
			Description: "SQLite 드라이버 규칙",
			Kind:        "preference",
			Tier:        "reference",
			Status:      "hypothesis",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ca := &countingAnalyzer{
		replies: []string{
			// 1st call: turn_finished extraction
			`{"lesson":{"task":"SQLite 설정","approach":"modernc sqlite 적용","outcome":"success","lesson":"CGO 없이 modernc sqlite를 사용한다","category":"설정"},"skill":null}`,
			// 2nd call: context_compacted reflection upgrades mem_reflect_1 to core + observed
			`{"actions":[{"op":"update","object_id":"mem_reflect_1","description":"SQLite 순수 Go 드라이버 원칙","text":"CGO 없이 순수 Go SQLite 드라이버(modernc)만 사용한다","kind":"preference","areas":["설정","구현"],"tier":"core","status":"observed"}]}`,
		},
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   ca,
		Experience: store,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: wd},
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })

	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "SQLite 설정 점검"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "modernc 적용 완료", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)

	h.FireEventWith("context_compacted", map[string]string{"session": "s1", "summary": "SQLite 드라이버 검증 완료"})
	h.DrainEvents(5 * time.Second)

	cores, err := store.CoreMemories(context.Background(), 2400)
	if err != nil || len(cores) != 1 {
		t.Fatalf("expected mem_reflect_1 promoted to core via reflection (initial rev=%s), got %+v (err=%v)", rec.Revision, cores, err)
	}
	if !strings.Contains(cores[0].Text, "modernc") || cores[0].Tier != "core" || cores[0].Status != "observed" {
		t.Fatalf("unexpected reflected core memory: %+v", cores[0])
	}
}

func TestStage6DreamDeterministicZeroTokenAndClusterMerge(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	wd := t.TempDir()
	store := expgit.New(t.TempDir())
	ctx := context.Background()
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")

	// 1. Expired memory -> should be archived deterministically.
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: "exp_mem", OperationID: "op1", ObservationID: "obs1", Owner: "plugin:engram",
		Kind: "memory", State: "active", HostOutcome: "verified",
		Memory: &port.Memory{
			Text: "임시 플래그 활성화", Description: "만료된 플래그", Tier: "reference", Status: "observed", ValidUntil: yesterday,
		},
	}); err != nil {
		t.Fatal(err)
	}

	// 2. Hypothesis memory with 2 sessions in evidence -> should be promoted to 'observed' with 0 LLM calls.
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: "hyp_mem", OperationID: "op2", ObservationID: "obs2", Owner: "plugin:engram",
		Kind: "memory", State: "active", HostOutcome: "verified",
		Memory: &port.Memory{
			Text:        " Go 테스트 실행 시 -race 플래그로 데이터 레이스를 점검한다",
			Description: "Go race 점검 규칙",
			Tier:        "reference",
			Status:      "hypothesis",
			Evidence: []port.MemoryEvidence{
				{Session: "sess_a", Outcome: "verified"},
				{Session: "sess_b", Outcome: "verified"},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	ca := &countingAnalyzer{}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   ca,
		Experience: store,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: wd},
	})
	if _, err := h.Load(ctx, src); err != nil {
		t.Fatal(err)
	}

	// No duplicate clusters exist -> /engram-dream must complete with 0 LLM calls!
	handled, out := h.DispatchCommand("/engram-dream", nil)
	if !handled || out == nil || !strings.Contains(out.Error(), "만료=1, 승격=1") {
		t.Fatalf("/engram-dream unexpected result: handled=%v out=%v", handled, out)
	}
	if got := ca.calls.Load(); got != 0 {
		t.Fatalf("expected 0 LLM calls when no duplicate clusters exist, got %d", got)
	}

	recs, err := store.SearchManagedExperience(ctx, "", "memory", 0, 20)
	if err != nil || len(recs) != 1 || recs[0].ObjectID != "hyp_mem" {
		t.Fatalf("expected only hyp_mem active after expiring exp_mem, got %+v (err=%v)", recs, err)
	}
	if recs[0].Memory.Status != "observed" {
		t.Fatalf("expected hyp_mem promoted to 'observed', got %q", recs[0].Memory.Status)
	}

	// Now seed a near-duplicate cluster sharing >=3 tokens and verify 1-call merge in Phase 2.
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: "dup_a", OperationID: "op3", ObservationID: "obs3", Owner: "plugin:engram",
		Kind: "memory", State: "active", HostOutcome: "verified",
		Memory: &port.Memory{
			Text:        "포트 충돌 발생 시 lsof 명령으로 점유 프로세스를 먼저 확인한다",
			Description: "포트 충돌 lsof 점유 프로세스 확인",
			Tier:        "reference",
			Status:      "observed",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: "dup_b", OperationID: "op4", ObservationID: "obs4", Owner: "plugin:engram",
		Kind: "memory", State: "active", HostOutcome: "verified",
		Memory: &port.Memory{
			Text:        "포트 충돌 디버깅에서는 lsof 명령으로 점유 프로세스를 찾아 종료한다",
			Description: "포트 충돌 lsof 점유 프로세스 종료",
			Tier:        "reference",
			Status:      "observed",
		},
	}); err != nil {
		t.Fatal(err)
	}
	ca.replies = []string{
		`{"merges":[{"keep_id":"dup_a","archive_ids":["dup_b"],"description":"포트 충돌 lsof 점검 및 종료","text":"포트 충돌 시 lsof 명령으로 점유 프로세스를 확인해 종료한다","kind":"lesson","areas":["디버깅"],"tier":"core","status":"observed"}]}`,
	}
	handled, out = h.DispatchCommand("/engram-dream", nil)
	if !handled || out == nil || !strings.Contains(out.Error(), "클러스터병합=1, LLM호출=1") {
		t.Fatalf("expected 1 cluster merge and 1 LLM call, got: handled=%v out=%v", handled, out)
	}
	if got := ca.calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 LLM call for cluster merge, got %d", got)
	}
	dups, err := store.SearchManagedExperience(ctx, "포트 충돌", "memory", 0, 10)
	if err != nil || len(dups) != 1 || dups[0].ObjectID != "dup_a" {
		t.Fatalf("expected dup_b archived and dup_a retained, got %+v (err=%v)", dups, err)
	}
}

func TestEngramPressureDreamNurseryProtectionAndContract(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	wd := t.TempDir()
	storeDir := t.TempDir()
	store := expgit.New(storeDir)
	ca := &countingAnalyzer{}

	ctx := context.Background()

	// 1. Seed an old core memory with 1500 chars
	oldDesc := "오래된_규칙: " + strings.Repeat("오래된 디버깅 절차와 규칙 설명입니다. ", 30)
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: "old_rule", OperationID: "op_old", ObservationID: "obs_old", Owner: "plugin:engram",
		Kind: "memory", State: "active", HostOutcome: "verified",
		Memory: &port.Memory{
			Text:        "오래된 본문",
			Description: oldDesc,
			Tier:        "core",
			Status:      "observed",
			Evidence:    []port.MemoryEvidence{{Session: "sess_old", Turn: 1, Outcome: "verified"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	// Artificially age the old rule file modtime to 5 days ago (so it is outside Nursery Grace Period)
	oldPath := filepath.Join(storeDir, "managed", "old_rule.json")
	pastTime := time.Now().Add(-5 * 24 * time.Hour)
	if err := os.Chtimes(oldPath, pastTime, pastTime); err != nil {
		t.Fatal(err)
	}

	// 2. Seed a fresh nursery core memory with 1000 chars (created just now, within 2-hour Grace Period)
	freshDesc := "신규_규칙: " + strings.Repeat("방금 디버깅으로 확인된 핵심 교훈 설명입니다. ", 20)
	if _, err := store.ApplyExperience(ctx, port.ExperienceChange{
		ObjectID: "fresh_rule", OperationID: "op_fresh", ObservationID: "obs_fresh", Owner: "plugin:engram",
		Kind: "memory", State: "active", HostOutcome: "verified",
		Memory: &port.Memory{
			Text:        "신규 본문",
			Description: freshDesc,
			Tier:        "core",
			Status:      "observed",
			Evidence:    []port.MemoryEvidence{{Session: "sess_now", Turn: 1, Outcome: "verified"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   ca,
		Experience: store,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: wd},
	})
	if _, err := h.Load(ctx, src); err != nil {
		t.Fatal(err)
	}

	// Total core characters exceed CORE_CHAR_BUDGET (2,400 chars).
	// Calling /engram-dream should trigger pressure/fast demote with nursery protection.
	handled, out := h.DispatchCommand("/engram-dream", nil)
	if !handled || out == nil || !strings.Contains(out.Error(), "코어강등=1") {
		t.Fatalf("expected 1 core demotion under pressure, got handled=%v out=%v", handled, out)
	}

	// Verify that old_rule was demoted to reference, while fresh_rule remained core!
	mems, err := store.SearchManagedExperience(ctx, "", "memory", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	var oldTier, freshTier string
	for _, m := range mems {
		if m.ObjectID == "old_rule" && m.Memory != nil {
			oldTier = m.Memory.Tier
		}
		if m.ObjectID == "fresh_rule" && m.Memory != nil {
			freshTier = m.Memory.Tier
		}
	}
	if oldTier != "reference" {
		t.Fatalf("expected old_rule demoted to reference, got %q", oldTier)
	}
	if freshTier != "core" {
		t.Fatalf("expected fresh_rule protected in core due to Nursery Grace Period, got %q", freshTier)
	}
}

