package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/port"
)

// Probe: load the bundled engram plugin and
// drive one capture + recall round trip against a scripted analyzer.
func TestProbeEngramPluginEndToEnd(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram") // bundled plugin, repo-relative
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	wd := t.TempDir()
	log := &syncLog{}
	fa := &fakeAnalyzer{reply: `{"lesson":{"task":"포트 충돌 해결","approach":"lsof로 점유 프로세스 확인 후 종료","outcome":"success","lesson":"EADDRINUSE는 잔존 프로세스 확인이 먼저다","category":"디버깅"},"skill":{"name":"port_conflict_lsof","trigger":"EADDRINUSE 포트 충돌","technique":"lsof -i :PORT로 점유 프로세스를 찾아 종료","description":"EADDRINUSE 포트 충돌 시 lsof로 점유 프로세스를 찾아 종료한다"}}`}
	reg := &fakeContextReg{}
	var notesMu sync.Mutex
	var notes []string
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: reg,
		Analyzer:   fa,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: wd},
		Notify: func(sid, text string) {
			notesMu.Lock()
			notes = append(notes, sid+": "+text)
			notesMu.Unlock()
		},
		Logf: log.logf,
	})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load engram: %v", err)
	}
	// The event pipeline is asynchronous, and t.TempDir's cleanup is not: a handler still writing
	// when the test returns races the RemoveAll, which fails the test with "directory not empty"
	// from the cleanup rather than from anything the test asserted. Reproduced under -race.
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })

	// Turn 1: plain finish (outcome=done) → no analysis.
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "서버가 안 떠"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "원인을 조사 중입니다", "outcome": "done"})
	// Turn 2: council-verified finish → structural gate fires → sidecar → ledger+skill.
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "고쳐줘"})
	h.FireEventWith("turn_finished", map[string]string{
		"session": "s1", "text": "lsof로 8080 점유 프로세스를 종료해 해결했습니다",
		"outcome": "verified",
	})

	deadline := time.Now().Add(5 * time.Second)
	var ledger string
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(filepath.Join(wd, "SESSION_SUMMARY.md"))
		if err == nil && strings.Contains(string(b), "포트 충돌 해결") {
			ledger = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ledger == "" {
		t.Fatalf("lesson never written; log:\n%s", log.String())
	}
	if !strings.Contains(ledger, "✅ 성공") || !strings.Contains(ledger, "| 일시 | 사용자 |") {
		t.Errorf("ledger malformed:\n%s", ledger)
	}

	// Waited for, like the ledger above. The two files are written by the same asynchronous
	// handler but not in the same instant, and this assertion used to read once — so under load
	// (the whole test tree building and running at once) it read the directory before the skill
	// landed and failed with "no such file". Observed exactly that way on 2026-08-07; the test
	// passes alone every time, which is the shape of a wait that was inherited rather than taken.
	var skill []byte
	var err error
	for deadline = time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		if skill, err = os.ReadFile(filepath.Join(wd, ".claude/skills/port_conflict_lsof/SKILL.md")); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("skill not saved: %v; log:\n%s", err, log.String())
	}
	if !strings.Contains(string(skill), "name: port_conflict_lsof") || !strings.Contains(string(skill), "lsof") {
		t.Errorf("skill malformed:\n%s", skill)
	}

	// Save notification fired, and a short denial on the NEXT user message undoes
	// the auto-saved skill (file gone, undo note sent, one-shot window).
	//
	// Waited for, not read once. The notification travels the same asynchronous pipeline as the
	// files above, and the skill landing on disk does not mean the note has been delivered — CI
	// failed here with "save notification missing; notes=[]" on a run where both files were
	// written. Everything else in this test polls; this was the one assertion that did not.
	h.DrainEvents(5 * time.Second)
	var sawSave bool
	for deadline = time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		notesMu.Lock()
		sawSave = len(notes) > 0 && strings.Contains(notes[0], "자동 저장")
		notesMu.Unlock()
		if sawSave {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !sawSave {
		t.Fatalf("save notification missing; notes=%v", notes)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "N"})
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(wd, ".claude/skills/port_conflict_lsof")); os.IsNotExist(err) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(wd, ".claude/skills/port_conflict_lsof")); !os.IsNotExist(err) {
		t.Fatalf("denial did not remove the auto-saved skill; log:\n%s", log.String())
	}
	notesMu.Lock()
	undoNoted := false
	for _, n := range notes {
		if strings.Contains(n, "취소") {
			undoNoted = true
		}
	}
	notesMu.Unlock()
	if !undoNoted {
		t.Fatalf("undo note missing; notes=%v", notes)
	}

	// Deterministic near-duplicate backstop: a later verified turn whose sidecar
	// emits the SAME lesson reworded (different task/approach → the disk
	// signature misses it) must be skipped by the token-overlap guard.
	fa.mu.Lock()
	fa.replies = []string{`{"lesson":{"task":"포트가 이미 점유된 문제 처리","approach":"lsof 명령으로 점유 프로세스 확인 후 종료함","outcome":"success","lesson":"EADDRINUSE는 잔존 프로세스 확인이 먼저다 — lsof로 점유 프로세스를 찾아 종료한다"},"skill":null}`}
	fa.calls = 0
	fa.mu.Unlock()
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "또 포트 문제"})
	h.FireEventWith("turn_finished", map[string]string{
		"session": "s1", "text": "이전과 같은 방식으로 해결했습니다", "outcome": "verified",
	})
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(log.String(), "유사 교훈 이미 기록됨") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(log.String(), "유사 교훈 이미 기록됨") {
		t.Fatalf("near-duplicate lesson was not skipped; log:\n%s", log.String())
	}
	b2, _ := os.ReadFile(filepath.Join(wd, "SESSION_SUMMARY.md"))
	if got := strings.Count(string(b2), "\n| 2"); got != 1 {
		t.Fatalf("ledger should still have exactly 1 data row, got %d:\n%s", got, b2)
	}

	// Recall: the registered context provider serves the ledger rows.
	if len(reg.providers) == 0 {
		t.Fatal("context provider not registered")
	}
	chunks, err := reg.providers[0].Provide(context.Background(), port.ContextQuery{SessionID: "s2", Workdir: wd, Prompt: "포트가 또 충돌"})
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) == 0 || !strings.Contains(chunks[0].Text, "포트 충돌 해결") {
		t.Fatalf("recall block missing the lesson: %+v", chunks)
	}
}
