package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// 별칭으로 온 값은 **제자리로 옮겨진다.** 버리는 것이 아니다 — 버리면 「적었습니다」 하고 빈
// 칸이 남는다(파워포인트 판이 2026-09-04 IR 판에서 그 화면을 봤다: `set_notes{notes:...}` 가
// 거절당한 뒤 모델이 재시도하지 않아 표지 고지가 통째로 비었다). 엑셀에서 같은 자리는
// 워드 판의 자리는 `format_text{font_color:...}` — 정본은 `color` 다.
func TestAliasMovesValueIntoPlace(t *testing.T) {
	var notes tool
	for _, one := range catalogue(false) {
		if one.Name == "format_text" {
			notes = one
		}
	}
	if notes.Name == "" {
		t.Fatal("format_text 가 목록에 없다")
	}

	args, err := validateArgs(notes, json.RawMessage(`{"from":1,"font_color":"#FF0000"}`))
	if err != nil {
		t.Fatalf("별칭이 거절당했다: %v", err)
	}
	if args["color"] != "#FF0000" {
		t.Fatalf("값이 color 로 안 옮겨졌다: %#v", args)
	}
	if _, still := args["font_color"]; still {
		t.Fatalf("별칭 이름이 남아 손까지 갔다: %#v", args)
	}

	// **별칭은 광고돼야 한다.** magi 는 광고된 스키마로 모르는 인자를 가려내므로, 안 실으면
	// 그 이름으로 온 호출마다 「버렸다」는 거짓 경고가 붙는다 — 값은 들어갔는데 모델은 자기가
	// 성공시킨 호출을 의심한다(2026-09-04 실측). 정본이 무엇인지는 설명이 말한다.
	schema := string(schemaOf(notes))
	if !strings.Contains(schema, `"font_color"`) {
		t.Fatalf("별칭이 스키마에 없다 — 거짓 경고가 붙는다: %s", schema)
	}
	if !strings.Contains(schema, "prefer color") {
		t.Fatalf("어느 쪽이 정본인지 스키마가 말하지 않는다: %s", schema)
	}
	// 그래도 **필수는 정본 하나**다. 별칭을 필수로 세면 둘 다 보내라는 말이 된다.
	for _, r := range notes.Required {
		if r == "font_color" {
			t.Fatal("별칭이 필수로 실렸다")
		}
	}

	// 둘 다 오면 우리가 뜻을 못 정한다 — 지어내지 말고 거절한다.
	if _, err := validateArgs(notes, json.RawMessage(`{"from":1,"color":"가","font_color":"나"}`)); err == nil {
		t.Fatal("정본과 별칭이 같이 왔는데 통과했다")
	} else if !strings.Contains(err.Error(), "only") {
		t.Fatalf("사유가 무엇을 보내야 하는지 안 말한다: %v", err)
	}

	// 그리고 **여전히 모르는 이름은 거절한다.** 별칭을 받는 것이 아무 이름이나 받는 것이 되면,
	// 조용히 안 먹는 인자가 다시 생긴다.
	if _, err := validateArgs(notes, json.RawMessage(`{"from":1,"color":"가","memo":"나"}`)); err == nil {
		t.Fatal("모르는 이름이 통과했다")
	}
}
