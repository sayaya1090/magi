package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 번들 스킬은 세션이 처음 읽는 규칙집이라 비어 있으면 안 되고, 모델이 부르는 이름
// (`skill{name}`) 은 AGENTS.md 가 시키는 이름과 같아야 한다.
func TestBundledSkillsNameWhatTheInstructionsAskFor(t *testing.T) {
	names := BundledSkillNames()
	if len(names) < 5 {
		t.Fatalf("번들 스킬이 %d개뿐이다: %v", len(names), names)
	}
	for _, must := range []string{"deck-design", "design-guide", "visual-deck", "academic-deck", "research"} {
		found := false
		for _, n := range names {
			found = found || n == must
		}
		if !found {
			t.Errorf("AGENTS.md 가 시키는 %q 가 번들에 없다: %v", must, names)
		}
	}
	body, err := bundledSkills.ReadFile("powerpoint/skills/deck-design.md")
	if err != nil || !strings.Contains(string(body), "render_slide") {
		t.Errorf("deck-design 이 장마다 렌더를 시키지 않는다: %v", err)
	}
}

// 시드는 멱등 reconcile 이다: 심고, 우리가 심은 그대로면 새 번들로 바꾸고, 사람이 고쳤으면 둔다.
func TestSeedSkillsIsIdempotentAndKeepsHumanEdits(t *testing.T) {
	dir := t.TempDir()
	first, err := SeedSkills(dir)
	if err != nil || len(first.Written) != len(BundledSkillNames()) {
		t.Fatalf("빈 워크스페이스에 전부 안 심었다: %+v %v", first, err)
	}
	again, _ := SeedSkills(dir)
	if len(again.Written)+len(again.Updated)+len(again.Kept) != 0 {
		t.Fatalf("두 번째 시드가 뭔가를 했다: %+v", again)
	}
	edited := filepath.Join(skillsDir(dir), "research.md")
	if err := os.WriteFile(edited, []byte("# 내 회사의 조사 요령\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, _ := SeedSkills(dir)
	if len(third.Kept) != 1 || third.Kept[0] != "research" {
		t.Fatalf("사람이 고친 스킬을 둔다고 적지 않았다: %+v", third)
	}
	if got, _ := os.ReadFile(edited); !strings.HasPrefix(string(got), "# 내 회사의") {
		t.Fatal("사람이 고친 스킬을 번들로 덮었다")
	}
	// 「우리가 심은 그대로」인 파일은 번들이 바뀌면 따라간다: 지문 기록을 옛 번들의 것으로
	// 바꿔 놓고 현재 파일을 그 옛 판으로 만들면 시드가 갱신해야 한다.
	stale := filepath.Join(skillsDir(dir), "visual-deck.md")
	old := []byte("옛 번들의 visual-deck\n")
	if err := os.WriteFile(stale, old, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(skillsDir(dir), bundledMarker)
	m, _ := os.ReadFile(marker)
	m = []byte(strings.Replace(string(m), `"visual-deck": "`+digestOfBundled(t, "visual-deck")+`"`,
		`"visual-deck": "`+digest(old)+`"`, 1))
	if err := os.WriteFile(marker, m, 0o644); err != nil {
		t.Fatal(err)
	}
	fourth, _ := SeedSkills(dir)
	if len(fourth.Updated) != 1 || fourth.Updated[0] != "visual-deck" {
		t.Fatalf("우리가 심은 옛 판을 새 번들로 안 바꿨다: %+v", fourth)
	}
	if got, _ := os.ReadFile(stale); string(got) == string(old) {
		t.Fatal("갱신했다면서 파일은 옛 판이다")
	}
	// 사람이 더한 스킬은 건드리지 않는다.
	mine := filepath.Join(skillsDir(dir), "my-brand.md")
	_ = os.WriteFile(mine, []byte("brand"), 0o644)
	_, _ = SeedSkills(dir)
	if got, _ := os.ReadFile(mine); string(got) != "brand" {
		t.Fatal("사람이 더한 스킬을 건드렸다")
	}
}

func digestOfBundled(t *testing.T, name string) string {
	t.Helper()
	b, err := bundledSkills.ReadFile("powerpoint/skills/" + name + ".md")
	if err != nil {
		t.Fatal(err)
	}
	return digest(b)
}
