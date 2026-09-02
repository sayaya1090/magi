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
