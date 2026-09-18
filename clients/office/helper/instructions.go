package office

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 패키지 내 워크스페이스 지속 지시(Custom Instructions) 관리 모듈.
//
// # 배경 및 구조
// 문서 서식 규칙(예: "글머리 기호 1줄 유지", "기업 브랜드 색상 적용", "표 머리글 굵게" 등)은
// 대화 턴마다 반복 전달하지 않고 영속적으로 유지되어야 합니다.
// magi 코어는 워크스페이스 루트의 `AGENTS.md` 내용을 매 시스템 프롬프트에 포함시키며,
// 컨텍스트 압축(Compaction) 시에도 이를 보존합니다(`internal/app/memory.go`).
// Office 컴패니언은 전용 워크스페이스를 보유하므로(`own.go`), 해당 경로에 `AGENTS.md`를
// 배치함으로써 추가적인 프로토콜 확장 없이 오피스 전용 지속 지시를 주입합니다.
//
// # 처리 원칙
// 사용자가 입력한 지시 텍스트를 파싱하거나 변경하지 않고 원문 그대로 보존하여 파일에 기록합니다.

// instructionsFile 은 해당 워크스페이스의 AGENTS.md 절대 경로를 반환합니다.
func instructionsFile(app *App, configDir string) string {
	return filepath.Join(app.DeckSpace(configDir), "AGENTS.md")
}

// maxInstructions 는 지속 지시 텍스트의 최대 허용 길이(8,000자)입니다.
// 본 지시 내용은 매 턴마다 시스템 프롬프트에 포함되어 토큰을 소모하므로 상한을 설정합니다.
// 부분 절삭(Truncate)할 경우 후반부 규칙이 조용히 누락되는 현상이 발생하므로, 초과 시 오류로 명시적 거절합니다.
const maxInstructions = 8000

// ReadInstructions 는 워크스페이스의 지속 지시(AGENTS.md) 내용을 반환합니다.
// 파일이 존재하지 않는 경우는 오류가 아니며, 빈 문자열을 반환합니다.
func ReadInstructions(app *App, configDir string) (string, error) {
	data, err := os.ReadFile(instructionsFile(app, configDir))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("지시를 못 읽었습니다(%s): %w", instructionsFile(app, configDir), err)
	}
	// 저장 시점과 일관되게 앞뒤 공백을 정규화하여 불필요한 변경 플래그 오탐을 방지합니다.
	return strings.TrimSpace(string(data)), nil
}

// WriteInstructions 는 전달받은 지시 내용을 AGENTS.md에 저장합니다. 본문이 비어 있으면 기존 파일을 삭제합니다.
func WriteInstructions(app *App, configDir, text string) (string, error) {
	body := strings.TrimSpace(text)
	if len(body) > maxInstructions {
		// 조용한 절삭으로 인한 규칙 누락을 방지하기 위해 상한 초과 시 명시적 오류를 반환합니다.
		return "", fmt.Errorf("지시가 너무 깁니다(%d자, 최대 %d자) — 이 글은 매번 모델에게 "+
			"통째로 실려 가므로 길면 그 값을 매 턴 치릅니다. 줄여서 다시 저장해 주세요",
			len([]rune(body)), maxInstructions)
	}
	path := instructionsFile(app, configDir)
	if body == "" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("지시를 못 지웠습니다(%s): %w", path, err)
		}
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("덱 작업 폴더 %s 를 못 만들었습니다: %w", filepath.Dir(path), err)
	}
	// 외부 편집기 조회 및 POSIX 표준을 고려하여 개행을 추가합니다.
	if err := os.WriteFile(path, []byte(body+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("지시를 못 적었습니다(%s): %w", path, err)
	}
	return body, nil
}

// DefaultInstructions 는 워크스페이스 초기 생성 시 AGENTS.md에 주입되는 기본 운영 지침 메타데이터를 의미합니다.
// 도구 사용 제약(예: 불필요한 인자 제외, 글꼴 적용 순서, 슬라이드당 렌더링 빈도 등)을 사전에 정의하여
// 사용자가 매 대화마다 운영 규칙을 직접 작성해야 하는 번거로움을 방지합니다(2026-09-05 사용자 피드백 반영).
// 사용자가 직접 내용을 수정한 이후에는 덮어쓰지 않고 사용자 설정을 최우선으로 보존합니다.

// SeedInstructions 는 지속 지시 파일이 존재하지 않거나 비어 있는 초기 상태일 때만 기본 지시를 주입합니다.
// 사용자 입력 내용이 이미 존재하는 경우 수정을 가하지 않습니다. 주입 성공 시 true를 반환합니다.
func SeedInstructions(app *App, configDir string) (bool, error) {
	have, err := ReadInstructions(app, configDir)
	if err != nil {
		return false, err
	}
	if have != "" {
		return false, nil
	}
	if _, err := WriteInstructions(app, configDir, app.Instructions); err != nil {
		return false, err
	}
	return true, nil
}
