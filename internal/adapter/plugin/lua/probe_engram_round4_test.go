package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
)

func loadEngram(t *testing.T, wd string, fa *fakeAnalyzer, log *syncLog) *Host {
	t.Helper()
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   fa,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: wd},
		Logf:       log.logf,
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load engram: %v", err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
	return h
}

// Round 4: a human-authored table with ≥7 columns in the prose section is NOT the ledger. It must
// not be fed to the sidecar as existing lessons, and a replaces correction must target the dated
// ledger row — not the human table's header. The ledger row is told apart by form: its first cell
// is an RFC3339 timestamp.
func TestProbeEngramLeavesAHumanTableAlone(t *testing.T) {
	wd := t.TempDir()
	human := "| 월 | 화 | 수 | 목 | 금 | 토 | 일 |\n" +
		"| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n" +
		"| 배포 | 회고 | 계획 | 개발 | 개발 | 휴식 | 휴식 |\n"
	seed := "사람이 손으로 둔 주간 일정표:\n\n" + human + "\n" +
		"# 작업 이력 및 교훈 기록 (팀 공유)\n\n" +
		"| 일시 | 사용자 | 분류 | 작업 | 시도한 접근 | 결과 | 교훈 |\n" +
		"| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n" +
		"| 2026-08-01T09:00:00Z | user | 설정 | 포트 해제 | pkill -f server | ✅ 성공 | pkill -f 로 서버를 내리면 된다 |\n"
	if err := os.WriteFile(filepath.Join(wd, "SESSION_SUMMARY.md"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	log := &syncLog{}
	fa := &fakeAnalyzer{reply: `{"lesson":{"task":"포트 해제","approach":"pkill -f server","outcome":"fail","lesson":"pkill -f는 자기 자신까지 죽인다 — port_owner로 pid를 집어 kill해야 한다","category":"디버깅","replaces":1},"skill":null}`}
	h := loadEngram(t, wd, fa, log)

	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "포트가 안 풀려"})
	h.FireEventWith("turn_finished", map[string]string{
		"session": "s1", "text": "pkill -f가 자기 프로세스까지 죽였습니다; port_owner로 해결",
		"outcome": "verified",
	})
	h.DrainEvents(5 * time.Second)

	b, err := os.ReadFile(filepath.Join(wd, "SESSION_SUMMARY.md"))
	if err != nil {
		t.Fatal(err)
	}
	ledger := string(b)
	active := ledger
	if i := strings.Index(ledger, "## 이전 기록"); i >= 0 {
		active = ledger[:i]
	}
	// The sidecar's existing-lessons digest counted only the dated ledger row.
	if strings.Contains(fa.text, "배포") || strings.Contains(fa.text, "| 월") {
		t.Errorf("the human table leaked into the sidecar digest:\n%s", fa.text)
	}
	// (1) resolved to the ledger row: it moved to the archive, the human table kept every line.
	if strings.Contains(active, "pkill -f 로 서버를 내리면 된다") {
		t.Errorf("replaces:1 did not archive the ledger row:\n%s", active)
	}
	for _, line := range strings.Split(human, "\n") {
		if line != "" && !strings.Contains(active, line) {
			t.Errorf("the human table lost a line: %q\n%s", line, active)
		}
	}
	if !strings.Contains(active, "port_owner로 pid를 집어") {
		t.Errorf("the correction is missing from the active table:\n%s", active)
	}
}

// Round 4: undoing a merge-save must RESTORE the previous SKILL.md, not delete the whole skill —
// the skill existed before this turn (possibly human-refined); deletion was data loss.
func TestProbeEngramUndoRestoresTheOverwrittenSkill(t *testing.T) {
	wd := t.TempDir()
	log := &syncLog{}
	fa := &fakeAnalyzer{replies: []string{
		`{"lesson":null,"skill":{"name":"port_probe","trigger":"포트 점검","technique":"V1: lsof -i :PORT","description":"포트 점유를 lsof로 확인한다"}}`,
		`{"lesson":null,"skill":{"name":"port_probe","trigger":"포트 점검","technique":"V2: port_owner PORT","description":"포트 점유를 port_owner로 확인한다"}}`,
	}}
	h := loadEngram(t, wd, fa, log)
	skillPath := filepath.Join(wd, ".claude", "skills", "port_probe", "SKILL.md")

	// Turn A creates the skill; the following ordinary message closes the undo window.
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "포트 점검 방법 알려줘"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "lsof로 확인, 됐다", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	if b, err := os.ReadFile(skillPath); err != nil || !strings.Contains(string(b), "V1:") {
		t.Fatalf("turn A did not create the skill: %v %s", err, b)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "고마워 잘 되네"})
	h.DrainEvents(5 * time.Second)

	// Turn B overwrites it (same name — the merge path), then the user denies.
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "port_owner로도 됐다", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	if b, err := os.ReadFile(skillPath); err != nil || !strings.Contains(string(b), "V2:") {
		t.Fatalf("turn B did not overwrite the skill: %v %s", err, b)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "N"})
	h.DrainEvents(5 * time.Second)

	b, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("undo DELETED the pre-existing skill instead of restoring it: %v\nlog:\n%s", err, log.String())
	}
	if !strings.Contains(string(b), "V1:") || strings.Contains(string(b), "V2:") {
		t.Errorf("undo must restore the pre-merge body:\n%s", b)
	}
}

// Round 4: when the content anchor demotes a mis-pointed replaces to a plain append, the duplicate
// guard the correction path was exempt from must re-arm — otherwise the entry lands beside an
// identical row with no dedup at all.
func TestProbeEngramDemotedCorrectionDedups(t *testing.T) {
	wd := t.TempDir()
	seed := "# 작업 이력 및 교훈 기록 (팀 공유)\n\n" +
		"| 일시 | 사용자 | 분류 | 작업 | 시도한 접근 | 결과 | 교훈 |\n" +
		"| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n" +
		"| 2026-08-01T09:00:00Z | user | 기타 | 알파 | 베타 | ✅ 성공 | 감마 |\n" +
		"| 2026-08-02T09:00:00Z | user | 설정 | 포트 해제 | port_owner 사용 | ✅ 성공 | 포트는 port_owner로 푼다 |\n"
	if err := os.WriteFile(filepath.Join(wd, "SESSION_SUMMARY.md"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	log := &syncLog{}
	// replaces:1 points at the 알파 row, which shares no tokens with this lesson → demoted; the
	// entry's task/approach/outcome signature already sits in row 2 → the append must be refused.
	fa := &fakeAnalyzer{reply: `{"lesson":{"task":"포트 해제","approach":"port_owner 사용","outcome":"success","lesson":"포트는 port_owner로 푼다","category":"설정","replaces":1},"skill":null}`}
	h := loadEngram(t, wd, fa, log)

	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "포트 좀 풀어줘"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "port_owner로 풀었다", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)

	b, err := os.ReadFile(filepath.Join(wd, "SESSION_SUMMARY.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != seed {
		t.Errorf("a demoted duplicate must leave the ledger untouched; diff:\n--- seed ---\n%s\n--- got ---\n%s", seed, got)
	}
}

// Round 4: the undo window is per-session. One global slot meant another session's save
// overwrote this session's window — the user who was just told "N to undo" said N into a void.
func TestProbeEngramUndoWindowsAreIndependent(t *testing.T) {
	wd := t.TempDir()
	log := &syncLog{}
	// Descriptions share no tokens — the similar-skill backstop must not conflate the fixtures.
	fa := &fakeAnalyzer{replies: []string{
		`{"lesson":null,"skill":{"name":"skill_a","trigger":"a","technique":"A의 기법","description":"포트 충돌을 lsof로 진단한다"}}`,
		`{"lesson":null,"skill":{"name":"skill_b","trigger":"b","technique":"B의 기법","description":"빌드 캐시를 정리해 컴파일 오류를 해소"}}`,
	}}
	h := loadEngram(t, wd, fa, log)
	pathA := filepath.Join(wd, ".claude", "skills", "skill_a", "SKILL.md")
	pathB := filepath.Join(wd, ".claude", "skills", "skill_b", "SKILL.md")

	h.FireEventWith("user_message", map[string]string{"session": "sA", "text": "a 해줘"})
	h.FireEventWith("turn_finished", map[string]string{"session": "sA", "text": "a 됐다", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	h.FireEventWith("user_message", map[string]string{"session": "sB", "text": "b 해줘"})
	h.FireEventWith("turn_finished", map[string]string{"session": "sB", "text": "b 됐다", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	for _, p := range []string{pathA, pathB} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	// A's denial lands after B's save. It must still undo A's OWN skill — and leave B's alone.
	h.FireEventWith("user_message", map[string]string{"session": "sA", "text": "N"})
	h.DrainEvents(5 * time.Second)
	if _, err := os.Stat(pathA); err == nil {
		t.Errorf("session A's denial must undo its own save; skill_a survived\nlog:\n%s", log.String())
	}
	if _, err := os.Stat(pathB); err != nil {
		t.Errorf("session A's denial must not touch session B's skill: %v", err)
	}
}
