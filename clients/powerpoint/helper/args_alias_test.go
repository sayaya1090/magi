package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 별칭으로 온 값은 **제자리로 옮겨진다.** 버리는 것이 아니다 — 버리면 「적었습니다」 하고 빈
// 노트가 남는다(2026-09-04 IR 판에서 그 화면을 봤다: `set_notes{notes:...}` 가 거절당한 뒤
// 모델이 재시도하지 않아 표지 고지가 통째로 비었다).
func TestAliasMovesValueIntoPlace(t *testing.T) {
	var notes tool
	for _, one := range catalogue(false) {
		if one.Name == "set_notes" {
			notes = one
		}
	}
	if notes.Name == "" {
		t.Fatal("set_notes 가 목록에 없다")
	}

	args, err := validateArgs(notes, json.RawMessage(`{"slide":3,"notes":"※ 예시입니다"}`))
	if err != nil {
		t.Fatalf("별칭이 거절당했다: %v", err)
	}
	if args["text"] != "※ 예시입니다" {
		t.Fatalf("값이 text 로 안 옮겨졌다: %#v", args)
	}
	if _, still := args["notes"]; still {
		t.Fatalf("별칭 이름이 남아 손까지 갔다: %#v", args)
	}

	// **광고는 하나뿐이다.** 별칭을 스키마에 실으면 모델이 어느 쪽이 정본인지 묻게 된다.
	for _, p := range notes.Props {
		if p.Name == "notes" {
			t.Fatal("별칭이 스키마에 실렸다")
		}
	}
	if schema := string(schemaOf(notes)); strings.Contains(schema, `"notes"`) {
		t.Fatalf("별칭이 스키마 JSON 에 보인다: %s", schema)
	}

	// 둘 다 오면 우리가 뜻을 못 정한다 — 지어내지 말고 거절한다.
	if _, err := validateArgs(notes, json.RawMessage(`{"slide":3,"text":"가","notes":"나"}`)); err == nil {
		t.Fatal("정본과 별칭이 같이 왔는데 통과했다")
	} else if !strings.Contains(err.Error(), "only") {
		t.Fatalf("사유가 무엇을 보내야 하는지 안 말한다: %v", err)
	}

	// 그리고 **여전히 모르는 이름은 거절한다.** 별칭을 받는 것이 아무 이름이나 받는 것이 되면,
	// 조용히 안 먹는 인자가 다시 생긴다.
	if _, err := validateArgs(notes, json.RawMessage(`{"slide":3,"text":"가","memo":"나"}`)); err == nil {
		t.Fatal("모르는 이름이 통과했다")
	}
}
