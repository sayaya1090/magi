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

// engram used to write any verify text over 200 bytes as scripts/verify.sh and tell the skill
// to "run" it. Live 2026-09-05 (PowerPoint companion): the sidecar's verify was a Korean
// sentence, the next run tried `bash scripts/verify.sh` (exit 127), the council demanded the
// run's evidence, and the memory hardened it into a lesson — three runs in a row. A script is
// written only for text that looks like shell; prose stays inline however long it is.
func TestEngramProseVerifyIsNotAScript(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	prose := strings.Repeat("read_notes 로 1번 표지 슬라이드의 가상 기업 및 예시 수치 고지문을 확인하고, read_slide 로 네이티브 차트와 머리행 서식 표를 실측 검증한다. ", 3)
	shell := "#!/bin/sh\nset -e\n" + strings.Repeat("test -f build/out.txt && grep -q OK build/out.txt\n", 6)
	if len(prose) <= 200 || len(shell) <= 200 {
		t.Fatalf("fixture must exceed the 200-byte split: prose=%d shell=%d", len(prose), len(shell))
	}
	run := func(t *testing.T, name, verify string) (skill string, script []byte, hasScript bool) {
		wd := t.TempDir()
		fa := &fakeAnalyzer{reply: `{"lesson":{"task":"덱 제작","approach":"x","outcome":"success","lesson":"y","category":"구현"},` +
			`"skill":{"name":"` + name + `","trigger":"t","technique":"add_slides 로 뼈대","description":"d","verify":` + jsonString(verify) + `}}`}
		h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, Analyzer: fa,
			DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: wd}, Notify: func(string, string) {}, Logf: func(string) {}})
		if _, err := h.Load(context.Background(), src); err != nil {
			t.Fatalf("load engram: %v", err)
		}
		t.Cleanup(func() { h.DrainEvents(5 * time.Second) })
		h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "덱 만들어"})
		h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "만들었습니다", "outcome": "verified"})
		path := filepath.Join(wd, ".claude/skills", name, "SKILL.md")
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), "## 검증") {
				skill = string(b)
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if skill == "" {
			t.Fatalf("skill %s never written", name)
		}
		script, err := os.ReadFile(filepath.Join(wd, ".claude/skills", name, "scripts", "verify.sh"))
		return skill, script, err == nil
	}
	t.Run("prose", func(t *testing.T) {
		skill, _, has := run(t, "deck_prose", prose)
		if has {
			t.Error("a Korean sentence must not become scripts/verify.sh")
		}
		if strings.Contains(skill, "실행해 확인") || !strings.Contains(skill, "read_notes 로 1번 표지") {
			t.Errorf("prose verify must stay inline under ## 검증, got:\n%s", skill)
		}
	})
	t.Run("shell", func(t *testing.T) {
		skill, script, has := run(t, "deck_shell", shell)
		if !has || !strings.HasPrefix(string(script), "#!/bin/sh") {
			t.Error("shell-looking verify still becomes scripts/verify.sh")
		}
		if !strings.Contains(skill, "scripts/verify.sh 를 실행해 확인한다") {
			t.Errorf("the skill must point at the script, got:\n%s", skill)
		}
	})
}

func jsonString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

// [plugins.engram] author_skills=false keeps the lesson and writes no skill.
func TestEngramAuthorSkillsOff(t *testing.T) {
	src := filepath.Join("..", "..", "..", "..", "plugins", "engram")
	if _, err := os.Stat(filepath.Join(src, "init.lua")); err != nil {
		t.Skip("bundled engram plugin not present")
	}
	wd := t.TempDir()
	fa := &fakeAnalyzer{reply: `{"lesson":{"task":"덱 제작","approach":"x","outcome":"success","lesson":"y","category":"구현"},` +
		`"skill":{"name":"deck_off","trigger":"t","technique":"add_slides 로 뼈대","description":"d"}}`}
	h := NewHostWithConfig(HostConfig{ToolSink: builtin.NewRegistry(), ContextReg: &fakeContextReg{}, Analyzer: fa,
		DataDir: t.TempDir(), Runtime: RuntimeInfo{Workdir: wd}, Notify: func(string, string) {}, Logf: func(string) {},
		PluginConfigs: map[string]map[string]any{"engram": {"author_skills": false}}})
	if _, err := h.Load(context.Background(), src); err != nil {
		t.Fatalf("load engram: %v", err)
	}
	h.FireEventWith("user_message", map[string]string{"session": "s1", "text": "덱 만들어"})
	h.FireEventWith("turn_finished", map[string]string{"session": "s1", "text": "만들었습니다", "outcome": "verified"})
	h.DrainEvents(5 * time.Second)
	if b, err := os.ReadFile(filepath.Join(wd, "SESSION_SUMMARY.md")); err != nil || !strings.Contains(string(b), "덱 제작") {
		t.Fatalf("the lesson must still be recorded: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wd, ".claude/skills/deck_off/SKILL.md")); err == nil {
		t.Error("with author_skills=false no skill may be written")
	}
}
