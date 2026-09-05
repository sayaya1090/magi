package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// 사람이 관리하는 **가이드 문서** — 이름이 붙은 여러 벌의 규칙.
//
// # 「늘 지킬 것」과 무엇이 다른가
//
// `instructions.go` 의 `AGENTS.md` 는 **매 턴 프롬프트에 통째로** 실린다. 그래서 짧아야 하고,
// 한 벌뿐이고, 끄고 켤 수가 없다 — 비우는 것이 곧 지우는 것이다.
//
// 가이드는 **모델이 필요할 때 불러 읽는다**(magi 의 skill 도구). 그래서 길어도 되고, 여러 벌을
// 둘 수 있고, **안 쓸 때 꺼 둘 수 있다.** 디자인 가이드처럼 「장을 만들 때만」 필요한 것이
// 여기 산다.
//
// 대가도 갈린다: 매 턴 실리는 것은 **반드시** 지켜지고, 불러 읽는 것은 **모델이 안 부르면 안
// 지켜진다.** 화면이 그 차이를 적어야 한다 — 안 적으면 사람은 적어 둔 것이 늘 도는 줄 안다.
//
// # 왜 파일이 그 자리인가
//
// magi 는 워크스페이스의 `.magi/skills/*.md` 를 스킬로 읽는다(`internal/app/skills.go`). 우리는
// 그 자리에 쓸 뿐이고 새 개념을 만들지 않는다 — 사람이 편집기로 직접 고쳐도 같은 것이 보인다.

// guidesDir 는 켜져 있는 가이드가 사는 자리. offDir 는 꺼 둔 것.
func guidesDir(configDir string) string {
	return filepath.Join(DeckSpace(configDir), ".magi", "skills")
}
func guidesOffDir(configDir string) string { return filepath.Join(guidesDir(configDir), "off") }

// maxGuide 는 한 벌의 천장.
//
// `AGENTS.md` 보다 넉넉한 이유가 있다 — 이건 매 턴이 아니라 **불렀을 때만** 실린다. 그래도
// 천장을 두는 것은 한 번에 실리는 값이 여전히 크기 때문이고, `instructions.go` 와 같은 이유로
// **자르지 않고 거절한다.**
const maxGuide = 40000

// Guide 는 화면이 그리는 한 줄.
type Guide struct {
	Name string `json:"name"`
	// Description 은 **magi 가 뽑는 것과 같은 규칙**으로 뽑는다(아래 guideDescription).
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Chars       int    `json:"chars"`
}

// guideDescription 은 설명 한 줄을 뽑는다 — **프런트매터가 있으면 그것, 없으면 첫 줄.**
//
// ⚠ **이 규칙은 magi 의 `internal/app/skills.go` 에 있는 것과 같아야 한다.** 두 벌이 갈리면
// 화면에 보이는 설명과 **모델이 받는 설명이 달라진다** — 사람은 자기가 읽은 것을 모델도 읽는
// 줄 안다. 그쪽이 이 규칙을 갖게 된 경위도 같은 종류였다: 첫 줄만 읽던 시절, 프런트매터로 적힌
// 스킬이 자기 설명을 `---` 로 광고하고 있었고 그 글자가 다섯 대의 컴패니언 전부에 실려 나갔다.
func guideDescription(text string) string {
	body := strings.TrimSpace(text)
	if strings.HasPrefix(body, "---") {
		if end := strings.Index(body[3:], "\n---"); end >= 0 {
			for _, line := range strings.Split(body[3:3+end], "\n") {
				if v, ok := strings.CutPrefix(strings.TrimSpace(line), "description:"); ok {
					return strings.TrimSpace(strings.Trim(strings.TrimSpace(v), `"'`))
				}
			}
		}
	}
	if i := strings.IndexByte(body, '\n'); i >= 0 {
		return strings.TrimSpace(body[:i])
	}
	return body
}

// guideName 은 이름을 검사한다. **이름이 곧 파일 이름**이라 여기서 막지 않으면 경로가 된다.
func guideName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fmt.Errorf("가이드 이름이 비었습니다")
	}
	if len(name) > 60 {
		return "", fmt.Errorf("가이드 이름이 너무 깁니다(%d자, 최대 60자)", len([]rune(name)))
	}
	if strings.ContainsAny(name, `/\:`) || strings.Contains(name, "..") || strings.HasPrefix(name, ".") {
		return "", fmt.Errorf("가이드 이름에 %q 는 쓸 수 없습니다 — 이름이 곧 파일 이름이라 "+
			"경로가 되면 안 됩니다. 글자·숫자·`-`·`_` 로 지어 주세요", name)
	}
	if name == "off" {
		return "", fmt.Errorf("`off` 는 꺼 둔 가이드가 사는 폴더 이름이라 쓸 수 없습니다")
	}
	return name, nil
}

// ListGuides 는 켜진 것과 꺼진 것을 **한 목록으로** 준다.
//
// 꺼진 것을 빼면 사람은 자기가 꺼 둔 것을 다시 켤 길이 없다 — 화면에서 사라진 것과 지워진 것이
// 같아 보인다.
func ListGuides(configDir string) ([]Guide, error) {
	var out []Guide
	for _, spec := range []struct {
		dir     string
		enabled bool
	}{{guidesDir(configDir), true}, {guidesOffDir(configDir), false}} {
		entries, err := os.ReadDir(spec.dir)
		if os.IsNotExist(err) {
			continue // 아직 아무것도 안 적은 것이다 — 실패가 아니다
		}
		if err != nil {
			return nil, fmt.Errorf("가이드를 못 읽었습니다(%s): %w", spec.dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".md")
			data, err := os.ReadFile(filepath.Join(spec.dir, e.Name()))
			if err != nil {
				return nil, fmt.Errorf("가이드 %q 를 못 읽었습니다: %w", name, err)
			}
			body := strings.TrimSpace(string(data))
			out = append(out, Guide{
				Name: name, Description: guideDescription(body),
				Enabled: spec.enabled, Chars: len([]rune(body)),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReadGuide 는 한 벌의 본문. 켜졌든 꺼졌든 **같은 글**이다.
func ReadGuide(configDir, raw string) (string, bool, error) {
	name, err := guideName(raw)
	if err != nil {
		return "", false, err
	}
	for _, spec := range []struct {
		dir     string
		enabled bool
	}{{guidesDir(configDir), true}, {guidesOffDir(configDir), false}} {
		data, err := os.ReadFile(filepath.Join(spec.dir, name+".md"))
		if err == nil {
			return strings.TrimSpace(string(data)), spec.enabled, nil
		}
		if !os.IsNotExist(err) {
			return "", false, fmt.Errorf("가이드 %q 를 못 읽었습니다: %w", name, err)
		}
	}
	return "", false, fmt.Errorf("가이드 %q 가 없습니다", name)
}

// WriteGuide 는 한 벌을 적어 둔다. **있던 것이 꺼져 있었으면 꺼진 채로 고친다** — 고치는 것과
// 켜는 것은 다른 일이고, 저장이 조용히 켜 버리면 사람이 안 시킨 일이 일어난다.
func WriteGuide(configDir, raw, text string) (Guide, error) {
	name, err := guideName(raw)
	if err != nil {
		return Guide{}, err
	}
	body := strings.TrimSpace(text)
	if body == "" {
		return Guide{}, fmt.Errorf("가이드 %q 의 내용이 비었습니다 — 지우려면 삭제를 쓰세요. "+
			"빈 파일을 남기면 「안 적힌 것」과 「없는 것」이 두 상태가 되는데 사람에게는 같은 뜻입니다", name)
	}
	if len([]rune(body)) > maxGuide {
		return Guide{}, fmt.Errorf("가이드가 너무 깁니다(%d자, 최대 %d자) — 자르지 않고 거절합니다. "+
			"조용히 자르면 뒷부분이 어느 날부터 안 지켜지는데 화면에는 저장됐다고 적힙니다",
			len([]rune(body)), maxGuide)
	}
	dir := guidesDir(configDir)
	if _, enabled, err := ReadGuide(configDir, name); err == nil && !enabled {
		dir = guidesOffDir(configDir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Guide{}, fmt.Errorf("%s 를 못 만들었습니다: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body+"\n"), 0o600); err != nil {
		return Guide{}, fmt.Errorf("가이드 %q 를 못 적었습니다: %w", name, err)
	}
	return Guide{
		Name: name, Description: guideDescription(body),
		Enabled: dir == guidesDir(configDir), Chars: len([]rune(body)),
	}, nil
}

// EnableGuide 는 켜고 끈다 — **파일을 옮기는 것**이지 지우는 것이 아니다.
//
// magi 의 스킬 로더는 디렉토리를 건너뛰므로(`skills.go`), `off/` 안에 있는 것은 그대로 잠든다.
// 내용을 안 건드리므로 다시 켜면 적어 둔 그대로 돌아온다.
func EnableGuide(configDir, raw string, on bool) (Guide, error) {
	name, err := guideName(raw)
	if err != nil {
		return Guide{}, err
	}
	body, was, err := ReadGuide(configDir, name)
	if err != nil {
		return Guide{}, err
	}
	if was == on {
		// **이미 그 상태인 것은 실패가 아니다.** 두 번 누른 것뿐이다.
		return Guide{Name: name, Description: guideDescription(body), Enabled: on, Chars: len([]rune(body))}, nil
	}
	from, to := guidesOffDir(configDir), guidesDir(configDir)
	if !on {
		from, to = to, from
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return Guide{}, fmt.Errorf("%s 를 못 만들었습니다: %w", to, err)
	}
	if err := os.Rename(filepath.Join(from, name+".md"), filepath.Join(to, name+".md")); err != nil {
		return Guide{}, fmt.Errorf("가이드 %q 를 못 옮겼습니다: %w", name, err)
	}
	return Guide{Name: name, Description: guideDescription(body), Enabled: on, Chars: len([]rune(body))}, nil
}

// DeleteGuide 는 지운다. 켜져 있든 꺼져 있든 **둘 다** 본다.
func DeleteGuide(configDir, raw string) error {
	name, err := guideName(raw)
	if err != nil {
		return err
	}
	gone := false
	for _, dir := range []string{guidesDir(configDir), guidesOffDir(configDir)} {
		err := os.Remove(filepath.Join(dir, name+".md"))
		if err == nil {
			gone = true
			continue
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("가이드 %q 를 못 지웠습니다: %w", name, err)
		}
	}
	if !gone {
		return fmt.Errorf("가이드 %q 가 없습니다", name)
	}
	return nil
}
