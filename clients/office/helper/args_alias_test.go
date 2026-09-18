package office

import (
	"encoding/json"
	"strings"
	"testing"
)

// 별칭 인자(Alias)로 유입된 값은 정규 파라미터로 매핑됩니다.
// 누락하거나 버릴 경우 모델 호출은 성공했으나 필드가 반영되지 않는 결함(2026-09-04 실측: `set_notes{notes:...}` 거절 후 재시도 없이 비정상 처리된 사례, Word `format_text{font_color:...}` -> 정규 필드 `color`)을 방지합니다.
func TestAliasMovesValueIntoPlace(t *testing.T) {
	var notes tool
	for _, one := range Word.Catalogue(false) {
		if one.Name == "format_text" {
			notes = one
		}
	}
	if notes.Name == "" {
		t.Fatal("format_text 가 목록에 없다")
	}

	args, err := validateArgs(Word, notes, json.RawMessage(`{"from":1,"font_color":"#FF0000"}`))
	if err != nil {
		t.Fatalf("별칭이 거절당했다: %v", err)
	}
	if args["color"] != "#FF0000" {
		t.Fatalf("값이 color 로 안 옮겨졌다: %#v", args)
	}
	if _, still := args["font_color"]; still {
		t.Fatalf("별칭 이름이 남아 손까지 갔다: %#v", args)
	}

	// 별칭은 스키마에 공개 정의되어야 합니다. 광고되지 않은 인자는 데몬 계층에서 미인식 필드로 처리되어 불필요한 경고가 발생하므로(2026-09-04 실측),
	// 스키마에 별칭을 명시하되 정규 필드 사용을 우선하도록 설명(`prefer color`)을 기술합니다.
	schema := string(schemaOf(Word, notes))
	if !strings.Contains(schema, `"font_color"`) {
		t.Fatalf("별칭이 스키마에 없다 — 거짓 경고가 붙는다: %s", schema)
	}
	if !strings.Contains(schema, "prefer color") {
		t.Fatalf("어느 쪽이 정본인지 스키마가 말하지 않는다: %s", schema)
	}
	// 필수 인자(`Required`) 목록에는 정규 파라미터만 포함하며 별칭을 필수로 지정하지 않습니다.
	for _, r := range notes.Required {
		if r == "font_color" {
			t.Fatal("별칭이 필수로 실렸다")
		}
	}

	// 정규 파라미터와 별칭이 동시 전달된 경우 충돌 방지를 위해 명시적으로 거절합니다.
	if _, err := validateArgs(Word, notes, json.RawMessage(`{"from":1,"color":"가","font_color":"나"}`)); err == nil {
		t.Fatal("정본과 별칭이 같이 왔는데 통과했다")
	} else if !strings.Contains(err.Error(), "only") {
		t.Fatalf("사유가 무엇을 보내야 하는지 안 말한다: %v", err)
	}

	// 정의되지 않은 미인식 인자는 허용하지 않고 즉시 검증 오류로 거절합니다.
	if _, err := validateArgs(Word, notes, json.RawMessage(`{"from":1,"color":"가","memo":"나"}`)); err == nil {
		t.Fatal("모르는 이름이 통과했다")
	}
}
