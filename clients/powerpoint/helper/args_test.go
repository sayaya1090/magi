package main

import (
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
