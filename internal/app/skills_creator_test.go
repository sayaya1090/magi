package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// loadSkills reads the skill-creator layout (.claude/skills/<slug>/SKILL.md,
// frontmatter description) alongside the flat .magi/skills format, and the
// mtime-signature cache picks up a skill saved mid-session without a restart.
func TestLoadSkillsCreatorLayoutAndInvalidation(t *testing.T) {
	wd := t.TempDir()
	a := &App{}

	// Flat format skill.
	flat := filepath.Join(wd, ".magi", "skills")
	if err := os.MkdirAll(flat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flat, "deploy.md"), []byte("how to deploy\nrun make ship"), 0o644); err != nil {
		t.Fatal(err)
	}
	// skill-creator format skill (what engram writes).
	sc := filepath.Join(wd, ".claude", "skills", "port_conflict_lsof")
	if err := os.MkdirAll(sc, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: port_conflict_lsof\ndescription: \"EADDRINUSE 포트 충돌 시 lsof로 점유 프로세스를 찾아 종료한다\"\n---\n\n# 본문\nlsof -i :PORT"
	if err := os.WriteFile(filepath.Join(sc, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	skills := a.loadSkills(wd)
	if len(skills) != 2 {
		t.Fatalf("want 2 skills (flat + skill-creator), got %d: %+v", len(skills), skills)
	}
	byName := map[string]string{}
	for _, s := range skills {
		byName[s.Name] = s.Description
	}
	if byName["deploy"] != "how to deploy" {
		t.Errorf("flat skill description = %q", byName["deploy"])
	}
	if byName["port_conflict_lsof"] != "EADDRINUSE 포트 충돌 시 lsof로 점유 프로세스를 찾아 종료한다" {
		t.Errorf("skill-creator description = %q (frontmatter quotes must be stripped)", byName["port_conflict_lsof"])
	}
	if b, ok := a.skillBody(wd, "port_conflict_lsof"); !ok || b != body {
		t.Errorf("skillBody must return the full SKILL.md")
	}

	// A skill saved AFTER the first load (engram mid-session) must appear on the
	// next call — the mtime signature invalidates the per-workdir cache.
	sc2 := filepath.Join(wd, ".claude", "skills", "compose_wait_for_healthy")
	if err := os.MkdirAll(sc2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sc2, "SKILL.md"),
		[]byte("---\nname: compose_wait_for_healthy\ndescription: \"compose 기동 순서 대기\"\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := len(a.loadSkills(wd)); got != 3 {
		t.Fatalf("mid-session skill not picked up: got %d skills, want 3", got)
	}
	// And an unchanged tree serves from cache (same slice contents, no error).
	if got := len(a.loadSkills(wd)); got != 3 {
		t.Fatalf("cache path broke: got %d", got)
	}
}

// A skill-creator skill's bundled resources (scripts/, references/) surface in
// the body as a manifest with the absolute dir, so relative references in the
// instructions are resolvable; a SKILL.md-only skill gets no manifest. And an
// in-place SKILL.md edit invalidates the cache.
func TestLoadSkillsBundledResourcesAndEditInvalidation(t *testing.T) {
	wd := t.TempDir()
	a := &App{}
	sc := filepath.Join(wd, ".claude", "skills", "pdf_tools")
	if err := os.MkdirAll(filepath.Join(sc, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sc, "SKILL.md"),
		[]byte("---\nname: pdf_tools\ndescription: \"pdf 처리\"\n---\nrun scripts/extract.py"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sc, "scripts", "extract.py"), []byte("print()"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sc, "reference.md"), []byte("ref"), 0o644); err != nil {
		t.Fatal(err)
	}

	body, ok := a.skillBody(wd, "pdf_tools")
	if !ok {
		t.Fatal("skill not loaded")
	}
	for _, want := range []string{"Bundled skill resources", sc, "scripts/extract.py", "reference.md"} {
		if !strings.Contains(body, want) {
			t.Errorf("manifest missing %q in body:\n%s", want, body)
		}
	}
	if strings.Contains(body, "- SKILL.md") {
		t.Error("SKILL.md itself must not be listed as a resource")
	}

	// In-place SKILL.md edit → next load sees the new content (file-mtime in the signature).
	time.Sleep(10 * time.Millisecond) // ensure a distinct mtime on coarse filesystems
	if err := os.WriteFile(filepath.Join(sc, "SKILL.md"),
		[]byte("---\nname: pdf_tools\ndescription: \"pdf 처리 v2\"\n---\nnew body"), 0o644); err != nil {
		t.Fatal(err)
	}
	body2, _ := a.skillBody(wd, "pdf_tools")
	if !strings.Contains(body2, "new body") {
		t.Errorf("edited SKILL.md not picked up:\n%s", body2)
	}
}

// frontmatterDescription: frontmatter wins, quotes stripped; no frontmatter →
// first content line.
func TestFrontmatterDescription(t *testing.T) {
	if got := frontmatterDescription("---\nname: x\ndescription: \"do the thing\"\n---\nbody"); got != "do the thing" {
		t.Errorf("frontmatter: %q", got)
	}
	if got := frontmatterDescription("just a line\nmore"); got != "just a line" {
		t.Errorf("fallback: %q", got)
	}
	if got := frontmatterDescription("---\nname: only\n---\n\nfirst content"); got != "first content" {
		t.Errorf("no description key: %q", got)
	}
}
