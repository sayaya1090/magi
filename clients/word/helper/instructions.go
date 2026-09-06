package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 사람이 **한 번 적어 두면 매번 지켜지는 말** — 지속 지시.
//
// # 왜 필요한가
//
// 「불릿은 한 줄로」, 「강조는 우리 회사 파랑으로」, 「표는 항상 머리글 굵게」. 이런 것은 부탁이
// 아니라 **취향이고 규칙**이라, 대화마다 다시 말하게 하면 쓰는 사람이 지친다. 그리고 지치면
// 안 말하게 되고, 안 말하면 결과가 매번 조금씩 다르다 — 발표 자료에서 그건 눈에 띈다.
//
// # 왜 파일 하나로 되는가
//
// magi 는 워크스페이스의 `AGENTS.md` 를 **매 시스템 프롬프트에 넣고 압축에도 안 날린다**
// (`internal/app/memory.go`). 파워포인트 컴패니언은 자기 워크스페이스를 갖고 있으므로
// (`own.go`), 거기 그 파일 하나를 쓰면 「파워포인트에서만 적용되는 지시」가 그대로 된다 —
// 새 개념도, 새 배선도 없다.
//
// # 우리가 안 하는 것
//
// **해석하지 않는다.** 사람이 적은 글을 그대로 싣고 그대로 돌려준다. 「이건 규칙 문법에 안 맞아요」
// 같은 것을 우리가 판단하기 시작하면, 사람은 자기가 뭘 적을 수 있는지를 매번 겪어 봐야 알게 된다.

// instructionsFile 은 그 파일의 자리.
func instructionsFile(configDir string) string {
	return filepath.Join(DeckSpace(configDir), "AGENTS.md")
}

// maxInstructions 는 받아 줄 길이의 천장.
//
// 넉넉하다 — 이 파일은 **매 턴 프롬프트에 들어가므로** 길면 그 값을 사람이 매번 치른다. 다만
// 자르지 않고 **거절한다**: 조용히 자르면 사람이 적어 둔 규칙의 뒷부분이 어느 날부터 안 지켜지는데,
// 화면에는 저장됐다고 적혀 있다.
const maxInstructions = 8000

// ReadInstructions 는 지금 적혀 있는 것.
//
// 파일이 없는 것은 **실패가 아니다** — 아직 아무것도 안 적은 것이고, 그게 기본 상태다.
func ReadInstructions(configDir string) (string, error) {
	data, err := os.ReadFile(instructionsFile(configDir))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("지시를 못 읽었습니다(%s): %w", instructionsFile(configDir), err)
	}
	// **쓴 것과 같은 모양으로 돌려준다.** 우리는 저장할 때 앞뒤를 다듬으므로, 읽을 때 안 다듬으면
	// 화면이 보여 주는 글과 저장될 글이 서로 다르다 — 사람이 고치지도 않았는데 「바뀜」이 뜬다.
	// 사람이 편집기로 직접 고쳐 둔 파일도 같은 규칙으로 읽는다.
	return strings.TrimSpace(string(data)), nil
}

// WriteInstructions 는 적어 둔다. 빈 글이면 파일을 치운다.
//
// **비우는 것이 지우는 것**이다. 빈 파일을 남겨 두면 「아무것도 안 적혀 있음」과 「파일이 없음」이
// 두 상태가 되는데, 사람에게는 같은 뜻이고 우리에게만 다르다.
func WriteInstructions(configDir, text string) (string, error) {
	body := strings.TrimSpace(text)
	if len(body) > maxInstructions {
		// **자르지 않고 거절한다.** 조용히 자르면 규칙의 뒷부분이 어느 날부터 안 지켜지는데
		// 화면에는 저장됐다고 적혀 있다 — 이 저장소가 최악이라고 적은 그 모양이다.
		return "", fmt.Errorf("지시가 너무 깁니다(%d자, 최대 %d자) — 이 글은 매번 모델에게 "+
			"통째로 실려 가므로 길면 그 값을 매 턴 치릅니다. 줄여서 다시 저장해 주세요",
			len([]rune(body)), maxInstructions)
	}
	path := instructionsFile(configDir)
	if body == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("지시를 못 지웠습니다(%s): %w", path, err)
		}
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("덱 작업 폴더 %s 를 못 만들었습니다: %w", filepath.Dir(path), err)
	}
	// 끝에 줄바꿈 하나 — 사람이 편집기로 열어 볼 파일이다.
	if err := os.WriteFile(path, []byte(body+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("지시를 못 적었습니다(%s): %w", path, err)
	}
	return body, nil
}

// DefaultInstructions 는 **워크스페이스가 처음 생길 때 한 번** AGENTS.md 에 심는 운영 지침이다.
//
// 왜 여기 있나: 사람은 내용만 말한다. 「document 인자를 쓰지 마라」「ea_font 는 맨 마지막」「장마다 한 번
// 렌더」 같은 것은 부탁이 아니라 이 도구를 쓰는 **방법**이고, 그것이 브리프에 들어 있으면 사람이
// 매번 적어야 한다(2026-09-05 까지 실제로 그랬다 — 사용자가 짚었다). 스킬은 모델이 읽어야 들어오지만
// AGENTS.md 는 매 시스템 프롬프트에 들어간다(`internal/app/memory.go`). 사람이 고치면 그 뒤로는 사람
// 것이다 — 비어 있거나 없을 때만 심는다.
const DefaultInstructions = `# Word 컴패니언 — 늘 지킬 것

이 대화는 **문서 하나**에 묶여 있습니다. 도구의 ` + "`document`" + ` 인자는 쓰지 않습니다.

1. **먼저 읽는다** — ` + "`list_paragraphs`" + ` 한 번이 목차다. 긴 문서는 from/to 로 넘긴다. 고칠 구간은 ` + "`read_paragraphs`" + ` 로 되읽는다. 첫 도구를 부르기 전에 ` + "`skill`" + ` 로 ` + "`document-structure`" + ` 를 읽고, 남의 글을 고치면 ` + "`editing`" + `, 표·그림·머리글이 있으면 ` + "`tables-and-review`" + ` 를 읽는다.
2. **이 문서의 옷을 입는다** — ` + "`describe_style`" + ` 이 말하는 스타일 이름과 글꼴을 쓴다. 제목은 스타일(Heading)로, 굵은 글자로 만들지 않는다.
3. **자리는 문단 번호다** — after/before 로 넣고, 답의 now 와 새 번호를 다음 호출에 쓴다. 끼워 넣으면 아래 번호가 민다.
4. **남의 문서는 변경 추적을 켠다**(` + "`set_track_changes`" + `, WordApi 1.4+). 안 되면 ` + "`snapshot_paragraphs`" + ` 를 먼저 찍는다.
5. **찾아 바꾸기는 ` + "`find`" + ` 로 세어 본 뒤 ` + "`replace_all`" + `.** 흔한 낱말이면 범위를 좁힌다.
6. **눈은 ` + "`read_html`" + ` 이다** — Word 는 그림을 못 준다. 고친 구간을 read_html 로 한 번 보고 어긋난 것을 고친다.
7. **끝낼 때 바꾼 것을 손잡이(문단 번호·표 번호·이름)와 함께 신고한다**(카운슬이 없으면 ` + "`land`" + `). 읽은 것은 한 일이 아니다. 안 한 것은 안 했다고 적는다.
`

// SeedInstructions 는 파일이 없거나 비어 있을 때만 DefaultInstructions 를 적는다. 사람이 적은 글은
// 한 글자도 안 건드린다. 심었으면 true.
func SeedInstructions(configDir string) (bool, error) {
	have, err := ReadInstructions(configDir)
	if err != nil {
		return false, err
	}
	if have != "" {
		return false, nil
	}
	if _, err := WriteInstructions(configDir, DefaultInstructions); err != nil {
		return false, err
	}
	return true, nil
}
