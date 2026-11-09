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

// TestEngramFactsOmitsSkillsByDefault verifies that by default, engram facts sidecar
// does NOT inject skill digest (neither summary list nor bodies) into the prompt.
func TestEngramFactsOmitsSkillsByDefault(t *testing.T) {
	wd := t.TempDir()
	log := &syncLog{}
	fa := &fakeAnalyzer{
		reply: `{"lesson":null,"skill":null}`,
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   fa,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: wd},
		Logf:       log.logf,
	})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })

	skillsDir := filepath.Join(wd, ".claude", "skills", "sample_skill")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDir, "SKILL.md"), []byte("---\nname: sample_skill\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	h.FireEventWith("user_message", map[string]string{
		"session": "s0",
		"text":    "run sample task",
	})
	h.FireEventWith("turn_finished", map[string]string{
		"session": "s0",
		"text":    "sample task complete",
		"outcome": "verified",
	})
	h.DrainEvents(5 * time.Second)

	fa.mu.Lock()
	analyzedText := fa.text
	fa.mu.Unlock()

	if strings.Contains(analyzedText, "[기존 스킬 목록]") || strings.Contains(analyzedText, "[기존 스킬 본문]") {
		t.Fatalf("skills must not be injected by default into facts prompt:\n%s", analyzedText)
	}
}

// TestEngramSkillsDigestSeparatesListAndSelectedBodies verifies that when include_skills is enabled:
// 1. All skills are listed in [기존 스킬 목록] as one-line summaries.
// 2. Only used skills and high-overlap skills have their full bodies included in [기존 스킬 본문].
// 3. Unrelated skills have their bodies omitted from [기존 스킬 본문].
func TestEngramSkillsDigestSeparatesListAndSelectedBodies(t *testing.T) {
	wd := t.TempDir()
	dataDir := t.TempDir()
	// Enable include_skills via plugin store
	pdata := filepath.Join(dataDir, "plugin-data")
	if err := os.MkdirAll(pdata, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdata, "engram.json"), []byte(`{"include_skills":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	log := &syncLog{}
	fa := &fakeAnalyzer{
		reply: `{"lesson":null,"skill":null}`,
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   fa,
		DataDir:    dataDir,
		Runtime:    RuntimeInfo{Workdir: wd},
		Logf:       log.logf,
	})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })

	// Create 4 skills on disk:
	// - skill_used: will be passed as used this turn
	// - skill_relevant: keywords "compiler" and "optlevel" match user message
	// - skill_unrelated_1: keywords "database" and "postgres"
	// - skill_unrelated_2: keywords "docker" and "container"
	skillsDir := filepath.Join(wd, ".claude", "skills")
	createSkill := func(name, desc, bodyText string) {
		dir := filepath.Join(skillsDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + name + "\ndescription: \"" + desc + "\"\n---\n\n# " + name + "\n\n" + bodyText
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	createSkill("skill_used", "used skill desc", "BODY_OF_USED_SKILL_UNIQUE_TOKEN")
	createSkill("skill_relevant", "compiler optimization pass", "BODY_OF_RELEVANT_COMPILER_PASS_OPTLEVEL")
	createSkill("skill_unrelated_1", "postgres database migrations", "BODY_OF_UNRELATED_DATABASE_POSTGRES")
	createSkill("skill_unrelated_2", "docker container build cache", "BODY_OF_UNRELATED_DOCKER_CONTAINER")

	h.FireEventWith("user_message", map[string]string{
		"session": "s1",
		"text":    "we need to fix the compiler optlevel issue",
	})
	h.FireEventWith("turn_finished", map[string]string{
		"session": "s1",
		"text":    "compiler pass checked and verified",
		"outcome": "verified",
		"skills":  "skill_used",
	})
	h.DrainEvents(5 * time.Second)

	fa.mu.Lock()
	analyzedText := fa.text
	fa.mu.Unlock()

	if analyzedText == "" {
		t.Fatalf("analyzer was not called; logs:\n%s", log.String())
	}

	// 1. [기존 스킬 목록] check: all 4 skills must be listed in summary
	if !strings.Contains(analyzedText, "[기존 스킬 목록]") {
		t.Fatalf("missing [기존 스킬 목록] in prompt:\n%s", analyzedText)
	}
	for _, name := range []string{"skill_used", "skill_relevant", "skill_unrelated_1", "skill_unrelated_2"} {
		if !strings.Contains(analyzedText, name) {
			t.Errorf("expected skill %q in list, but not found", name)
		}
	}

	// 2. [기존 스킬 본문] check:
	// skill_used must be present (marked [이번 턴 사용됨] and body included)
	if !strings.Contains(analyzedText, "BODY_OF_USED_SKILL_UNIQUE_TOKEN") {
		t.Errorf("skill_used body must be included in prompt")
	}
	// skill_relevant must be present (matched keywords in context)
	if !strings.Contains(analyzedText, "BODY_OF_RELEVANT_COMPILER_PASS_OPTLEVEL") {
		t.Errorf("skill_relevant body must be included in prompt")
	}
	// skill_unrelated_1 and skill_unrelated_2 bodies must NOT be present
	if strings.Contains(analyzedText, "BODY_OF_UNRELATED_DATABASE_POSTGRES") {
		t.Errorf("skill_unrelated_1 body must NOT be included in prompt")
	}
	if strings.Contains(analyzedText, "BODY_OF_UNRELATED_DOCKER_CONTAINER") {
		t.Errorf("skill_unrelated_2 body must NOT be included in prompt")
	}
}

// TestEngramSkillUpdateRejectedWithoutBody verifies that when the analyzer attempts to
// update an existing skill whose body was NOT provided in the prompt, the update is rejected
// to protect human edits from being lost.
func TestEngramSkillUpdateRejectedWithoutBody(t *testing.T) {
	wd := t.TempDir()
	dataDir := t.TempDir()
	pdata := filepath.Join(dataDir, "plugin-data")
	if err := os.MkdirAll(pdata, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pdata, "engram.json"), []byte(`{"include_skills":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	log := &syncLog{}
	// Analyzer attempts to overwrite an existing skill 'legacy_skill'
	fa := &fakeAnalyzer{
		reply: `{"lesson":{"task":"t","approach":"a","outcome":"success","lesson":"l"},"skill":{"name":"legacy_skill","description":"overwritten desc","technique":"OVERWRITTEN_TECHNIQUE"}}`,
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   fa,
		DataDir:    dataDir,
		Runtime:    RuntimeInfo{Workdir: wd},
		Logf:       log.logf,
	})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })

	// Create legacy_skill on disk (keywords completely unrelated to user message)
	skillDir := filepath.Join(wd, ".claude", "skills", "legacy_skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	origContent := "---\nname: legacy_skill\ndescription: \"original desc\"\n---\n\n# legacy_skill\n\nORIGINAL_HUMAN_EDITED_BODY"
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte(origContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Turn message has nothing to do with legacy_skill, so legacy_skill's body is NOT sent
	h.FireEventWith("user_message", map[string]string{
		"session": "s2",
		"text":    "unrelated audio processing task",
	})
	h.FireEventWith("turn_finished", map[string]string{
		"session": "s2",
		"text":    "audio processing done",
		"outcome": "verified",
	})
	h.DrainEvents(5 * time.Second)

	// Verify legacy_skill body is still ORIGINAL_HUMAN_EDITED_BODY (not overwritten)
	b, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "OVERWRITTEN_TECHNIQUE") {
		t.Fatalf("existing skill without body in prompt was overwritten! content:\n%s", string(b))
	}
	if !strings.Contains(string(b), "ORIGINAL_HUMAN_EDITED_BODY") {
		t.Fatalf("original content lost! content:\n%s", string(b))
	}

	// Verify log contains rejection message
	if !strings.Contains(log.String(), "skill update rejected (body not provided to analyzer): legacy_skill") {
		t.Fatalf("expected rejection log not found; logs:\n%s", log.String())
	}
}

// TestEngramNewSkillAllowedEvenWithoutPriorBody verifies that a genuinely NEW skill
// (which did not previously exist on disk) is created without issues.
func TestEngramNewSkillAllowedEvenWithoutPriorBody(t *testing.T) {
	wd := t.TempDir()
	log := &syncLog{}
	fa := &fakeAnalyzer{
		reply: `{"lesson":{"task":"t","approach":"a","outcome":"success","lesson":"l"},"skill":{"name":"brand_new_skill","description":"new skill desc","technique":"NEW_SKILL_TECHNIQUE"}}`,
	}
	h := NewHostWithConfig(HostConfig{
		ToolSink:   builtin.NewRegistry(),
		ContextReg: &fakeContextReg{},
		Analyzer:   fa,
		DataDir:    t.TempDir(),
		Runtime:    RuntimeInfo{Workdir: wd},
		Logf:       log.logf,
	})
	if _, err := h.Load(context.Background(), filepath.Join("..", "..", "..", "..", "plugins", "engram")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.DrainEvents(5 * time.Second) })

	h.FireEventWith("user_message", map[string]string{
		"session": "s3",
		"text":    "running brand new task",
	})
	h.FireEventWith("turn_finished", map[string]string{
		"session": "s3",
		"text":    "brand new task complete",
		"outcome": "verified",
	})
	h.DrainEvents(5 * time.Second)

	skillFile := filepath.Join(wd, ".claude", "skills", "brand_new_skill", "SKILL.md")
	b, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatalf("brand_new_skill was not created: %v; logs:\n%s", err, log.String())
	}
	if !strings.Contains(string(b), "NEW_SKILL_TECHNIQUE") {
		t.Fatalf("unexpected content in brand_new_skill:\n%s", string(b))
	}
}
