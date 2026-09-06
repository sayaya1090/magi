package office

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func guideSpace(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// TestGuidesLiveWhereMagiAlreadyLooks 는 우리가 쓰는 자리가 **magi 가 이미 읽는 자리**인지 잰다.
// 새 자리를 만들면 사람이 편집기로 연 파일과 모델이 읽는 파일이 갈린다.
func TestGuidesLiveWhereMagiAlreadyLooks(t *testing.T) {
	dir := guideSpace(t)
	if got := guidesDir(Word, dir); !strings.HasSuffix(got, filepath.Join(".magi", "skills")) {
		t.Errorf("가이드 자리가 magi 의 스킬 자리가 아니다: %s", got)
	}
	if got := guidesOffDir(Word, dir); !strings.HasPrefix(got, guidesDir(Word, dir)) {
		t.Errorf("꺼 둔 것이 스킬 폴더 밖에 있다: %s", got)
	}
}

// TestTheDescriptionIsTheOneTheModelSees 는 화면이 뽑는 설명이 magi 가 뽑는 것과 같은 규칙인지
// 잰다. 갈리면 **사람이 읽은 설명과 모델이 받는 설명이 다르다.**
func TestTheDescriptionIsTheOneTheModelSees(t *testing.T) {
	front := "---\ndescription: 발표자료를 보기 좋게 만든다\n---\n\n# 본문\n"
	if got := guideDescription(front); got != "발표자료를 보기 좋게 만든다" {
		t.Errorf("프런트매터 설명을 못 읽었다: %q", got)
	}
	if got := guideDescription("첫 줄이 설명이다\n\n본문"); got != "첫 줄이 설명이다" {
		t.Errorf("첫 줄 설명을 못 읽었다: %q", got)
	}
	// 프런트매터가 있는데 첫 줄만 읽으면 스스로를 `---` 로 광고한다 — 실제로 그렇게 나간 적이 있다.
	if guideDescription(front) == "---" {
		t.Error("설명이 `---` 다")
	}
}

// TestDisablingIsNotDeleting 은 넷을 다 잰다: 추가 · 비활성화 · 활성화 · 삭제.
func TestDisablingIsNotDeleting(t *testing.T) {
	dir := guideSpace(t)
	body := "---\ndescription: 사내 표준\n---\n\n제목은 32pt."
	g, err := WriteGuide(Word, dir, "design-guide", body)
	if err != nil {
		t.Fatalf("추가 실패: %v", err)
	}
	if !g.Enabled || g.Description != "사내 표준" {
		t.Errorf("새 가이드는 켜진 채로, 설명과 함께 서야 한다: %+v", g)
	}

	if g, err = EnableGuide(Word, dir, "design-guide", false); err != nil || g.Enabled {
		t.Fatalf("비활성화 실패: %+v %v", g, err)
	}
	// **꺼도 목록에는 남는다** — 사라지면 다시 켤 길이 없다.
	list, err := ListGuides(Word, dir)
	if err != nil || len(list) != 1 || list[0].Enabled {
		t.Fatalf("꺼 둔 것이 목록에서 사라졌다: %+v %v", list, err)
	}
	// **꺼도 글은 그대로다.**
	if got, enabled, err := ReadGuide(Word, dir, "design-guide"); err != nil || enabled || !strings.Contains(got, "32pt") {
		t.Errorf("꺼 두었더니 글이 달라졌다: %q %v %v", got, enabled, err)
	}
	// 로더는 디렉토리를 건너뛰므로 `off/` 안은 잠든다.
	if _, err := os.Stat(filepath.Join(guidesOffDir(Word, dir), "design-guide.md")); err != nil {
		t.Errorf("꺼 둔 파일이 off/ 에 없다: %v", err)
	}

	// **꺼진 것을 고쳐도 켜지지 않는다** — 고치는 것과 켜는 것은 다른 일이다.
	if g, err = WriteGuide(Word, dir, "design-guide", body+"\n본문 20pt."); err != nil || g.Enabled {
		t.Errorf("저장이 조용히 켰다: %+v %v", g, err)
	}

	if g, err = EnableGuide(Word, dir, "design-guide", true); err != nil || !g.Enabled {
		t.Fatalf("활성화 실패: %+v %v", g, err)
	}
	// 두 번 켜는 것은 실패가 아니다.
	if _, err = EnableGuide(Word, dir, "design-guide", true); err != nil {
		t.Errorf("이미 켜진 것을 켠 것이 실패로 났다: %v", err)
	}

	if err = DeleteGuide(Word, dir, "design-guide"); err != nil {
		t.Fatalf("삭제 실패: %v", err)
	}
	if list, _ = ListGuides(Word, dir); len(list) != 0 {
		t.Errorf("지웠는데 남아 있다: %+v", list)
	}
	if err = DeleteGuide(Word, dir, "design-guide"); err == nil {
		t.Error("없는 것을 지웠다는데 아무 말도 안 했다")
	}
}

// TestAGuideNameCannotBecomeAPath 는 이름이 곧 파일 이름이라는 데서 오는 것을 막는지 잰다.
func TestAGuideNameCannotBecomeAPath(t *testing.T) {
	dir := guideSpace(t)
	for _, bad := range []string{"", "../escape", "a/b", `a\b`, ".hidden", "off", strings.Repeat("가", 61)} {
		if _, err := WriteGuide(Word, dir, bad, "본문"); err == nil {
			t.Errorf("%q 를 이름으로 받았다", bad)
		}
	}
}

// TestAnOverlongGuideIsRefusedNotCut 는 자르지 않고 거절하는지 잰다.
func TestAnOverlongGuideIsRefusedNotCut(t *testing.T) {
	dir := guideSpace(t)
	if _, err := WriteGuide(Word, dir, "long", strings.Repeat("가", maxGuide+1)); err == nil {
		t.Error("너무 긴 가이드를 받았다 — 조용히 자르면 뒷부분이 어느 날부터 안 지켜진다")
	}
	if _, err := WriteGuide(Word, dir, "empty", "   "); err == nil {
		t.Error("빈 가이드를 받았다 — 지우는 것은 삭제가 할 일이다")
	}
}
