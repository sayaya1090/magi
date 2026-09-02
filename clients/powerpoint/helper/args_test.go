package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func toolNamed(t *testing.T, name string) tool {
	t.Helper()
	for _, tl := range catalogue() {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("%s 라는 도구가 없다", name)
	return tool{}
}

// 모르는 키는 **거절한다**(DESIGN.md §4.3). 조용히 무시하면 「고쳤습니다」 하고 안 바뀌는
// 실패가 인자 한 칸에서 난다.
func TestAnUndeclaredArgumentIsRefused(t *testing.T) {
	_, err := validateArgs(toolNamed(t, "set_text"),
		[]byte(`{"shape_id":"sh1","text":"Q3","keep_formatting":true}`))
	if err == nil {
		t.Fatal("모르는 키가 그냥 지나갔다")
	}
	// 거절이 **무엇을 쓸 수 있는지** 같이 말해야 한다 — 안 적으면 모델이 다음 시도에서 또 지어낸다.
	for _, want := range []string{"keep_formatting", "shape_id", "text", "document"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("거절 문구에 %q 가 없다: %s", want, err)
		}
	}
	// 그리고 **아무것도 안 바뀌었다**고 말해야 한다. 모델이 「반쯤 됐나」를 물을 자리가 없다.
	if !strings.Contains(err.Error(), "did not run") {
		t.Errorf("거절 문구가 안 돌았다는 말을 안 한다: %s", err)
	}
}

func TestRequiredArgumentsAreRequired(t *testing.T) {
	if _, err := validateArgs(toolNamed(t, "set_text"), []byte(`{"shape_id":"sh1"}`)); err == nil {
		t.Fatal("text 없이 통과했다")
	}
	if _, err := validateArgs(toolNamed(t, "set_text"), []byte(`{"shape_id":"sh1","text":""}`)); err != nil {
		t.Fatalf("빈 문자열은 값이다 — 거절하면 안 된다: %v", err)
	}
}

// 타입이 틀린 것과 모르는 키는 **다른 실패**다. 한 문장으로 뭉치면 모델이 이름을 고치려 든다.
func TestATypeMistakeSaysWhatWasExpected(t *testing.T) {
	_, err := validateArgs(toolNamed(t, "move_shape"), []byte(`{"shape_id":"sh1","left":"12pt"}`))
	if err == nil {
		t.Fatal("문자열이 숫자 칸을 통과했다")
	}
	if !strings.Contains(err.Error(), "left") || !strings.Contains(err.Error(), "number") {
		t.Errorf("무엇이 왜 틀렸는지 안 적혔다: %s", err)
	}
}

// 번호는 1 부터다(CAPABILITIES.md §10.4 — 사람도 모델도 "3번 슬라이드"라고 말한다).
// 0 을 받아 조용히 첫 장으로 읽으면, 모델이 0-based 라고 믿은 채 이후 전부 한 장씩 어긋난다.
func TestSlideIsOneBasedAndSaysSo(t *testing.T) {
	if _, err := validateArgs(toolNamed(t, "read_slide"), []byte(`{"slide":0}`)); err == nil {
		t.Fatal("slide=0 이 통과했다")
	}
	if _, err := validateArgs(toolNamed(t, "read_slide"), []byte(`{"slide":1}`)); err != nil {
		t.Fatalf("slide=1 이 거절당했다: %v", err)
	}
	// 소수도 위치가 아니다.
	if _, err := validateArgs(toolNamed(t, "read_slide"), []byte(`{"slide":1.5}`)); err == nil {
		t.Fatal("slide=1.5 가 통과했다")
	}
}

// 인자가 아예 없는 호출은 정상이다 — 슬라이드도 문서도 생략이 답인 도구가 있다.
func TestNoArgumentsIsFine(t *testing.T) {
	for _, name := range []string{"list_slides", "clear_advice", "read_slide"} {
		if _, err := validateArgs(toolNamed(t, name), nil); err != nil {
			t.Errorf("%s: 빈 인자가 거절당했다: %v", name, err)
		}
	}
}

func TestDocumentIsReadOffTheArguments(t *testing.T) {
	args, err := validateArgs(toolNamed(t, "read_slide"), []byte(`{"document":" doc-7 ","slide":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := documentOf(args); got != "doc-7" {
		t.Errorf("document 가 %q 다", got)
	}
	if got := documentOf(map[string]any{}); got != "" {
		t.Errorf("생략은 빈 문자열이어야 한다(활성 문서), got %q", got)
	}
}

// **안 준 것과 `null` 은 같은 뜻이다.**
//
// JSON 을 짓는 모델은 선택 인자를 생략하는 대신 null 로 채우는 일이 잦다 — 스키마의 칸을 하나씩
// 훑으며 값을 넣기 때문이다. 실물에서 그 화면을 봤다(2026-09-02): 사람이 「4분기 계획이라는
// 제목으로 새 장 하나 만들어 줘」라고 했고 모델은 전부 옳게 했는데(늘 지킬 것까지 지켜
// title 을 "[4분기 계획]" 으로 지었다) 호출이 「"document" must be a string (got null)」로
// 튕겼다. 장은 안 생겼고 사람은 아무 일도 안 일어난 화면을 봤다.
func TestANullOptionalMeansItWasNotGiven(t *testing.T) {
	tl := tool{
		Name: "add_slide",
		Props: []property{
			{Name: "layout", Type: "string"},
			{Name: "title", Type: "string"},
			{Name: "at", Type: "integer"},
			{Name: "match_style", Type: "boolean"},
		},
	}
	args, err := validateArgs(tl, json.RawMessage(
		`{"document":null,"layout":null,"at":null,"match_style":true,"title":"[4분기 계획]"}`))
	if err != nil {
		t.Fatalf("null 을 型 오류로 되받았다 — 모델은 다 옳게 했는데 아무 일도 안 일어난다: %v", err)
	}
	for _, gone := range []string{"document", "layout", "at"} {
		if _, ok := args[gone]; ok {
			t.Fatalf("null 인 %q 를 값으로 넘겼다 — 손이 그것을 진짜 값으로 읽는다: %v", gone, args)
		}
	}
	if args["title"] != "[4분기 계획]" || args["match_style"] != true {
		t.Fatalf("진짜로 준 값까지 지웠다: %v", args)
	}
}

// **필수 칸의 null 은 그대로 거절한다.** 그쪽은 진짜로 빠뜨린 것이고, 지어내 주면 안 된다.
func TestANullRequiredIsStillMissing(t *testing.T) {
	tl := tool{
		Name:     "set_text",
		Props:    []property{{Name: "shape_id", Type: "string"}, {Name: "text", Type: "string"}},
		Required: []string{"shape_id", "text"},
	}
	_, err := validateArgs(tl, json.RawMessage(`{"shape_id":"2","text":null}`))
	if err == nil {
		t.Fatal("필수 칸이 null 인데 받아 줬다")
	}
	if !strings.Contains(err.Error(), "text") {
		t.Fatalf("어느 칸이 빠졌는지 안 알려 준다: %v", err)
	}
}

// 값이 있는 칸은 여전히 型을 본다 — null 을 봐준다고 아무 값이나 받는 것은 아니다.
func TestARealTypeMismatchIsStillRefused(t *testing.T) {
	tl := tool{Name: "add_slide", Props: []property{{Name: "at", Type: "integer"}}}
	_, err := validateArgs(tl, json.RawMessage(`{"at":"세 번째"}`))
	if err == nil {
		t.Fatal("정수 자리에 글을 받아 줬다")
	}
}
