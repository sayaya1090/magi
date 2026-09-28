package office

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 번들 스킬은 세션이 처음 읽는 규칙집이라 비어 있으면 안 되고, 모델이 부르는 이름
// (`skill{name}`) 은 AGENTS.md 가 시키는 이름과 같아야 한다.
func TestBundledSkillsNameWhatTheInstructionsAskFor(t *testing.T) {
	names := Word.BundledSkillNames()
	if len(names) < 3 {
		t.Fatalf("번들 스킬이 %d개뿐이다: %v", len(names), names)
	}
	for _, must := range []string{"document-structure", "editing", "tables-and-review"} {
		found := false
		for _, n := range names {
			found = found || n == must
		}
		if !found {
			t.Errorf("AGENTS.md 가 시키는 %q 가 번들에 없다: %v", must, names)
		}
	}
	body, err := bundledSkills.ReadFile("skills/word/document-structure.md")
	if err != nil || !strings.Contains(string(body), "read_html") {
		t.Errorf("document-structure 가 read_html 로 보라고 안 한다: %v", err)
	}
}

// 시드는 멱등 reconcile 이다: 심고, 우리가 심은 그대로면 새 번들로 바꾸고, 사람이 고쳤으면 둔다.
func TestSeedSkillsIsIdempotentAndKeepsHumanEdits(t *testing.T) {
	dir := t.TempDir()
	first, err := SeedSkills(Word, dir)
	if err != nil || len(first.Written) != len(Word.BundledSkillNames()) {
		t.Fatalf("빈 워크스페이스에 전부 안 심었다: %+v %v", first, err)
	}
	again, _ := SeedSkills(Word, dir)
	if len(again.Written)+len(again.Updated)+len(again.Kept) != 0 {
		t.Fatalf("두 번째 시드가 뭔가를 했다: %+v", again)
	}
	edited := filepath.Join(skillsDir(Word, dir), "editing.md")
	if err := os.WriteFile(edited, []byte("# 내 회사의 조사 요령\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, _ := SeedSkills(Word, dir)
	if len(third.Kept) != 1 || third.Kept[0] != "editing" {
		t.Fatalf("사람이 고친 스킬을 둔다고 적지 않았다: %+v", third)
	}
	if got, _ := os.ReadFile(edited); !strings.HasPrefix(string(got), "# 내 회사의") {
		t.Fatal("사람이 고친 스킬을 번들로 덮었다")
	}
	// 「우리가 심은 그대로」인 파일은 번들이 바뀌면 따라간다: 지문 기록을 옛 번들의 것으로
	// 바꿔 놓고 현재 파일을 그 옛 판으로 만들면 시드가 갱신해야 한다.
	stale := filepath.Join(skillsDir(Word, dir), "tables-and-review.md")
	old := []byte("옛 번들의 tables-and-review\n")
	if err := os.WriteFile(stale, old, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(skillsDir(Word, dir), bundledMarker)
	m, _ := os.ReadFile(marker)
	m = []byte(strings.Replace(string(m), `"tables-and-review": "`+digestOfBundled(t, "tables-and-review")+`"`,
		`"tables-and-review": "`+digest(old)+`"`, 1))
	if err := os.WriteFile(marker, m, 0o644); err != nil {
		t.Fatal(err)
	}
	fourth, _ := SeedSkills(Word, dir)
	if len(fourth.Updated) != 1 || fourth.Updated[0] != "tables-and-review" {
		t.Fatalf("우리가 심은 옛 판을 새 번들로 안 바꿨다: %+v", fourth)
	}
	if got, _ := os.ReadFile(stale); string(got) == string(old) {
		t.Fatal("갱신했다면서 파일은 옛 판이다")
	}
	// 사람이 더한 스킬은 건드리지 않는다.
	mine := filepath.Join(skillsDir(Word, dir), "my-brand.md")
	_ = os.WriteFile(mine, []byte("brand"), 0o644)
	_, _ = SeedSkills(Word, dir)
	if got, _ := os.ReadFile(mine); string(got) != "brand" {
		t.Fatal("사람이 더한 스킬을 건드렸다")
	}
}

func digestOfBundled(t *testing.T, name string) string {
	t.Helper()
	b, err := bundledSkills.ReadFile("skills/word/" + name + ".md")
	if err != nil {
		t.Fatal(err)
	}
	return digest(b)
}

// 스킬의 예제는 모델이 그대로 따라 부른다 — 예제 블록에 적힌 도구 이름이 그 앱의 카탈로그에 없으면, 모델은 없는 도구를
// 부르고 「모른다」를 받는다. 예제 코드 블록의 줄머리 `이름 {` 를 모아 카탈로그와 견준다.
func TestSkillExamplesCallRealTools(t *testing.T) {
	head := regexp.MustCompile(`^([a-z_]+) {`)
	for _, app := range Apps {
		known := map[string]bool{"skill": true, "council": true, "land": true}
		for _, tl := range app.Catalogue(true) {
			known[tl.Name] = true
		}
		seen := 0
		for _, name := range app.BundledSkillNames() {
			body, err := bundledSkills.ReadFile(app.Skills + "/" + name + ".md")
			if err != nil {
				t.Fatal(err)
			}
			in := false
			for _, line := range strings.Split(string(body), "\n") {
				if strings.HasPrefix(line, "```") {
					in = !in
					continue
				}
				if m := head.FindStringSubmatch(line); in && m != nil {
					seen++
					if !known[m[1]] {
						t.Errorf("%s/%s: 예제가 없는 도구 %q 를 부른다", app.Key, name, m[1])
					}
				}
			}
		}
		if seen == 0 {
			t.Errorf("%s: 스킬 예제에서 도구 호출을 하나도 못 찾았다 — 이 시험은 아무것도 안 쟀다", app.Key)
		}
	}
}

// Static instructions cannot know whether the companion exposes a completion tool.
// Naming one here previously caused calls to an unavailable council tool.
func TestStaticOfficeGuidanceDoesNotPrescribeCompletionTools(t *testing.T) {
	call := regexp.MustCompile("(?i)\\b(council|land)\\s*\\{")
	check := func(name, body string) {
		t.Helper()
		if call.MatchString(body) || strings.Contains(body, "`land`") {
			t.Errorf("%s prescribes a completion tool without runtime availability", name)
		}
	}
	for name, body := range map[string]string{"ppt": pptInstructions, "word": wordInstructions, "xl": xlInstructions} {
		check(name, body)
	}
	for _, app := range []string{"powerpoint", "word", "excel"} {
		files, err := bundledSkills.ReadDir("skills/" + app)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			path := "skills/" + app + "/" + f.Name()
			body, err := bundledSkills.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			check(path, string(body))
		}
	}
}
