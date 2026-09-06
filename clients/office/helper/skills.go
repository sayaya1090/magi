package office

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// bundledSkills 는 이 헬퍼가 데몬에 같이 실어 보내는 피피티 스킬셋이다. 모델은 세션을 열며
// `skill` 로 이것을 읽고 그 규칙(장마다 한 번 렌더, 제목 한 줄, 색 롤)에 묶인다 — 카운슬도
// 같은 본문을 증거로 본다(council: Guidance). 파일이 사람 손의 워크스페이스에만 있으면
// 배포된 에이전트는 빈손이라, 저장소에 두고 바이너리에 굳혀 시드한다(사용자 요구
// 2026-09-05: 「그 에이전트에는 피피티 스킬셋이 번들로 들어가야 한다」).
//
//go:embed skills/powerpoint/*.md skills/excel/*.md skills/word/*.md
var bundledSkills embed.FS

// bundledMarker 는 마지막으로 심은 번들의 지문. 이것이 있어야 「사람이 고친 것」과 「우리가
// 심은 그대로인 것」을 가를 수 있다 — 지문 없이 내용만 비교하면 새 번들이 나올 때마다 사람의
// 편집을 덮거나, 반대로 영영 갱신을 못 한다.
const bundledMarker = ".bundled.json"

func skillsDir(app *App, configDir string) string {
	return filepath.Join(app.DeckSpace(configDir), ".magi", "skills")
}

// SkillSeed 는 시드 한 번의 결과: 새로 심은 것, 번들 갱신으로 바꾼 것, 사람이 고쳐서 둔 것.
type SkillSeed struct {
	Written []string
	Updated []string
	Kept    []string
}

// BundledSkillNames 는 번들에 든 스킬 이름(파일명에서 .md 를 뗀 것), 정렬됨.
func (a *App) BundledSkillNames() []string {
	ents, _ := fs.ReadDir(bundledSkills, a.Skills)
	var names []string
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	sort.Strings(names)
	return names
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// SeedSkills 는 번들 스킬을 워크스페이스의 .magi/skills 에 맞춘다. 멱등이다:
//   - 없는 파일은 심는다.
//   - 있는데 **우리가 마지막에 심은 그대로**면(지문 일치) 새 번들로 바꾼다.
//   - 있는데 사람이 고쳤으면(지문 불일치) 둔다 — 사람의 글이 번들을 이긴다.
//
// 번들에 없는 파일은 건드리지 않는다(사람이 더한 스킬).
func SeedSkills(app *App, configDir string) (SkillSeed, error) {
	var out SkillSeed
	dir := skillsDir(app, configDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return out, err
	}
	last := map[string]string{}
	if b, err := os.ReadFile(filepath.Join(dir, bundledMarker)); err == nil {
		_ = json.Unmarshal(b, &last)
	}
	next := map[string]string{}
	for _, name := range app.BundledSkillNames() {
		body, err := bundledSkills.ReadFile(app.Skills + "/" + name + ".md")
		if err != nil {
			return out, err
		}
		next[name] = digest(body)
		path := filepath.Join(dir, name+".md")
		have, err := os.ReadFile(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.WriteFile(path, body, 0o644); err != nil {
				return out, err
			}
			out.Written = append(out.Written, name)
		case err != nil:
			return out, err
		case digest(have) == next[name]:
			// 이미 이 번들 그대로.
		case digest(have) == last[name]:
			if err := os.WriteFile(path, body, 0o644); err != nil {
				return out, err
			}
			out.Updated = append(out.Updated, name)
		default:
			out.Kept = append(out.Kept, name)
			next[name] = last[name] // 사람의 판을 둔 채로는 「심었다」고 적지 않는다
		}
	}
	b, _ := json.MarshalIndent(next, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, bundledMarker), b, 0o644); err != nil {
		return out, err
	}
	return out, nil
}
