package lua

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayaya1090/magi/internal/adapter/tool/builtin"
	"github.com/sayaya1090/magi/internal/core/session"
	"github.com/sayaya1090/magi/internal/port"
)

func loadEngramAdvised(t *testing.T, fa *fakeAnalyzer) (*Host, *fakeAdvisorReg) {
	t.Helper()
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	reg := &fakeAdvisorReg{}
	log := &syncLog{}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: reg, Analyzer: fa,
		DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: t.TempDir()}, Logf: log.logf})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load engram: %v", err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
	if len(reg.advisors) != 1 {
		t.Fatalf("engram registered %d compaction advisors, want 1", len(reg.advisors))
	}
	return h, reg
}

func advice(t *testing.T, reg *fakeAdvisorReg, sid string) string {
	t.Helper()
	got, err := reg.advisors[0].AdviseCompaction(context.Background(), port.CompactionQuery{SessionID: session.SessionID(sid)})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// The real lesson path: engram analyses a finished turn, records a lesson, and the compaction
// advice for that session then names it.
func TestEngramAdvisesAboutALessonItJustRecorded(t *testing.T) {
	fa := &fakeAnalyzer{reply: `{"lesson":{"task":"포트 해제","approach":"port_owner 사용","outcome":"success","lesson":"포트는 port_owner로 푼다","category":"설정"},"skill":null}`}
	h, reg := loadEngramAdvised(t, fa)
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "포트 좀 풀어줘"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "port_owner로 풀었다", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	if got := advice(t, reg, "s1"); !strings.Contains(got, "포트는 port_owner로 푼다") {
		t.Errorf("advice does not name the recorded lesson (analyzer calls %d):\n%s", fa.calls, got)
	}
}

// What the agent saved itself with the remember tool reaches engram as a plain tool_finished
// observation; engram keeps the successful, durable ones. A turn checkpoint or a failed call is not kept.
func TestEngramAdvisesAboutWhatTheAgentRemembered(t *testing.T) {
	h, reg := loadEngramAdvised(t, &fakeAnalyzer{reply: `{"lesson":null,"skill":null}`})
	h.FireEventWith("tool_finished", map[string]string{"session": "s1", "tool": "remember", "ok": "true",
		"args": `{"text":"go tests here need TMPDIR=/tmp","scope":"project"}`})
	h.FireEventWith("tool_finished", map[string]string{"session": "s1", "tool": "remember", "ok": "true",
		"args": `{"text":"check the empty list before finishing","scope":"turn"}`})
	h.FireEventWith("tool_finished", map[string]string{"session": "s1", "tool": "remember", "ok": "false",
		"args": `{"text":"refused entry"}`})
	h.FireEventWith("tool_finished", map[string]string{"session": "s1", "tool": "bash", "ok": "true",
		"args": `{"command":"echo remember me"}`})
	h.DrainEvents(5 * time.Second)
	got := advice(t, reg, "s1")
	if !strings.Contains(got, "go tests here need TMPDIR=/tmp") {
		t.Errorf("advice lacks the remembered entry:\n%s", got)
	}
	for _, no := range []string{"check the empty list", "refused entry", "echo remember me"} {
		if strings.Contains(got, no) {
			t.Errorf("advice keeps %q:\n%s", no, got)
		}
	}
}

// A skill the user cancels with N is no longer kept, so the brief must not be told to leave it out:
// both the delete path (a new skill) and the restore path (an overwritten one) take it off the list.
func TestEngramForgetsASkillTheUserCancelled(t *testing.T) {
	fa := &fakeAnalyzer{replies: []string{
		`{"lesson":null,"skill":{"name":"port_probe","trigger":"포트 점검","technique":"V1: lsof -i :PORT","description":"포트 점유를 lsof로 확인한다"}}`,
		`{"lesson":null,"skill":{"name":"port_probe","trigger":"포트 점검","technique":"V2: port_owner PORT","description":"포트 점유를 port_owner로 확인한다"}}`,
		`{"lesson":null,"skill":{"name":"disk_probe","trigger":"디스크 점검","technique":"df -h","description":"디스크 여유를 df로 확인한다"}}`,
	}}
	h, reg := loadEngramAdvised(t, fa)
	turn := func(user, answer string) {
		h.FireEventWith("user_message", map[string]string{"session": "s1", "text": user})
		h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": answer, "outcome": "verified"})
		h.DrainEvents(5 * time.Second)
	}
	// Restore path: port_probe is created, kept, then overwritten and the overwrite cancelled.
	turn("포트 점검 방법 알려줘", "lsof로 확인, 됐다")
	turn("고마워 잘 되네", "port_owner로도 됐다")
	if got := advice(t, reg, "s1"); !strings.Contains(got, "skill: port_probe") {
		t.Fatalf("the saved skill is not on the list:\n%s", got)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "N"})
	h.DrainEvents(5 * time.Second)
	if got := advice(t, reg, "s1"); strings.Contains(got, "skill: port_probe") {
		t.Errorf("a cancelled (restored) skill is still on the list:\n%s", got)
	}
	// Delete path: disk_probe is new, so N deletes it.
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "df로 확인했다", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	if got := advice(t, reg, "s1"); !strings.Contains(got, "skill: disk_probe") {
		t.Fatalf("the new skill is not on the list (analyzer calls %d):\n%s", fa.calls, got)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "N"})
	h.DrainEvents(5 * time.Second)
	if got := advice(t, reg, "s1"); strings.Contains(got, "skill: disk_probe") {
		t.Errorf("a cancelled (deleted) skill is still on the list:\n%s", got)
	}
}
