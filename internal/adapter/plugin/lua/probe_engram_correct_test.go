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

// A lesson that turned out wrong must be CORRECTED, not contradicted beside itself. The sidecar
// names the row it overturns (replaces), and the ledger replaces it in place — the old claim
// moves to the archive section (a tombstone the recall provider never injects), the new claim
// becomes the table's current truth.
func TestProbeEngramCorrectsALessonInPlace(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	wd := t.TempDir()
	// Seed a ledger whose one lesson is WRONG (recorded ✅ for an approach that later broke).
	seed := "# 작업 이력 및 교훈 기록 (팀 공유)\n\n" +
		"| 일시 | 사용자 | 분류 | 작업 | 시도한 접근 | 결과 | 교훈 |\n" +
		"| :--- | :--- | :--- | :--- | :--- | :--- | :--- |\n" +
		"| 2026-08-01 | user | 설정 | 포트 해제 | pkill -f server | ✅ 성공 | pkill -f 로 서버를 내리면 된다 |\n"
	if err := os.WriteFile(filepath.Join(wd, "SESSION_SUMMARY.md"), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	log := &syncLog{}
	fa := &fakeAnalyzer{reply: `{"lesson":{"task":"포트 해제","approach":"pkill -f server","outcome":"fail","lesson":"pkill -f는 자기 자신까지 죽인다 — port_owner로 pid를 집어 kill해야 한다","category":"디버깅","replaces":1},"skill":null}`}
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

	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "포트가 안 풀려"})
	h.FireEventWith("turn_finished", map[string]string{
		"session": "s1", "text": "pkill -f가 자기 프로세스까지 죽였습니다; port_owner로 해결",
		"outcome": "verified",
	})

	deadline := time.Now().Add(5 * time.Second)
	var ledger string
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(wd, "SESSION_SUMMARY.md"))
		if err == nil && strings.Contains(string(b), "port_owner") {
			ledger = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ledger == "" {
		t.Fatalf("correction never written; log:\n%s", log.String())
	}

	active := ledger
	archive := ""
	if i := strings.Index(ledger, "## 이전 기록"); i >= 0 {
		active, archive = ledger[:i], ledger[i:]
	} else {
		t.Fatalf("no archive section — the overturned row must survive as a tombstone:\n%s", ledger)
	}
	if strings.Contains(active, "pkill -f 로 서버를 내리면 된다") {
		t.Errorf("the overturned claim is still in the ACTIVE table:\n%s", active)
	}
	if !strings.Contains(active, "port_owner로 pid를 집어") {
		t.Errorf("the correction is missing from the active table:\n%s", active)
	}
	if !strings.Contains(archive, "pkill -f 로 서버를 내리면 된다") || !strings.Contains(archive, "정정됨") {
		t.Errorf("the archive must hold the old row, marked corrected:\n%s", archive)
	}
}
