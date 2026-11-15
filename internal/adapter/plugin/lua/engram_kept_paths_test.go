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
